package social

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/zed378/zed-auth/backend/internal/config"
)

var (
	ErrProviderConfig   = errors.New("social: invalid provider configuration")
	ErrProviderResponse = errors.New("social: provider authentication failed")
)

// ProviderConfig contains references only; credentials are resolved per exchange.
type ProviderConfig struct {
	Provider        Provider         `json:"provider"`
	ClientID        string           `json:"client_id"`
	ClientSecretRef config.SecretRef `json:"-"`
	RedirectURI     string           `json:"redirect_uri"`
	// A fixed tenant makes Microsoft's issuer unambiguous for the stored subject.
	MicrosoftTenantID string `json:"microsoft_tenant_id,omitempty"`
}

func (c ProviderConfig) Validate() error {
	u, err := url.Parse(c.RedirectURI)
	if !c.Provider.Valid() || len(c.ClientID) == 0 || len(c.ClientID) > 512 || strings.TrimSpace(c.ClientID) != c.ClientID || strings.ContainsAny(c.ClientID, "\r\n\x00") || err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(c.RedirectURI, "#") || u.Opaque != "" || u.Path != "/login/social/"+string(c.Provider)+"/callback" || u.RawPath != "" {
		return ErrProviderConfig
	}
	ref := string(c.ClientSecretRef)
	scheme, value, ok := strings.Cut(ref, ":")
	if !ok || (scheme != "file" && scheme != "env") || value == "" || strings.TrimSpace(ref) != ref || strings.ContainsAny(ref, "\r\n\x00") {
		return ErrProviderConfig
	}
	if c.Provider == Microsoft {
		tenant, err := uuid.Parse(c.MicrosoftTenantID)
		if err != nil || tenant == uuid.Nil || tenant.String() != c.MicrosoftTenantID {
			return ErrProviderConfig
		}
	} else if c.MicrosoftTenantID != "" {
		return ErrProviderConfig
	}
	return nil
}

// ProviderIdentity is deliberately independent of mutable email/profile fields.
type ProviderIdentity struct {
	Provider Provider
	Subject  string
}

// Adapter performs provider requests only; it never reads or creates local users.
type Adapter struct {
	cfg                            ProviderConfig
	secrets                        config.SecretResolver
	http                           *http.Client
	issuer, authorize, token, jwks string
	mu                             sync.Mutex
	keys                           []jose.JSONWebKey
	keysExpire, lastRefresh        time.Time
	now                            func() time.Time
}

func NewAdapter(c ProviderConfig, secrets config.SecretResolver) (*Adapter, error) {
	if err := c.Validate(); err != nil || secrets == nil {
		return nil, ErrProviderConfig
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	transport.ResponseHeaderTimeout = 5 * time.Second
	transport.MaxConnsPerHost = 8
	a := &Adapter{cfg: c, secrets: secrets, now: time.Now, http: &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	switch c.Provider {
	case Google:
		a.issuer = "https://accounts.google.com"
		a.authorize = "https://accounts.google.com/o/oauth2/v2/auth"
		a.token = "https://oauth2.googleapis.com/token"
		a.jwks = "https://www.googleapis.com/oauth2/v3/certs"
	case Microsoft:
		base := "https://login.microsoftonline.com/" + c.MicrosoftTenantID
		a.issuer = base + "/v2.0"
		a.authorize = base + "/oauth2/v2.0/authorize"
		a.token = base + "/oauth2/v2.0/token"
		a.jwks = base + "/discovery/v2.0/keys"
	case GitHub:
		a.authorize = "https://github.com/login/oauth/authorize"
		a.token = "https://github.com/login/oauth/access_token"
	}
	return a, nil
}

func (a *Adapter) AuthorizeURL(state, nonce, challenge string) (string, error) {
	if !urlToken(state, 43, 128) || !urlToken(challenge, 43, 43) || (a.cfg.Provider != GitHub && !urlToken(nonce, 43, 128)) {
		return "", ErrProviderResponse
	}
	q := url.Values{"client_id": {a.cfg.ClientID}, "redirect_uri": {a.cfg.RedirectURI}, "response_type": {"code"}, "state": {state}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	if a.cfg.Provider == GitHub {
		q.Set("scope", "read:user user:email")
	} else {
		q.Set("scope", "openid email profile")
		q.Set("nonce", nonce)
	}
	return a.authorize + "?" + q.Encode(), nil
}

func urlToken(s string, min, max int) bool {
	if len(s) < min || len(s) > max {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-._~", r)) {
			return false
		}
	}
	return true
}

func (a *Adapter) Exchange(ctx context.Context, code, verifier, nonce string) (ProviderIdentity, error) {
	if len(code) == 0 || len(code) > 4096 || strings.ContainsAny(code, "\r\n\x00") || !urlToken(verifier, 43, 128) || (a.cfg.Provider != GitHub && !urlToken(nonce, 43, 128)) {
		return ProviderIdentity{}, ErrProviderResponse
	}
	secret, err := a.secrets.Resolve(a.cfg.ClientSecretRef)
	if err != nil || len(secret) == 0 || len(secret) > 8192 {
		return ProviderIdentity{}, ErrProviderResponse
	}
	defer clear(secret)
	q := url.Values{"grant_type": {"authorization_code"}, "client_id": {a.cfg.ClientID}, "client_secret": {string(secret)}, "redirect_uri": {a.cfg.RedirectURI}, "code": {code}, "code_verifier": {verifier}}
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		IDToken     string `json:"id_token"`
		Error       string `json:"error"`
	}
	if err = a.request(ctx, http.MethodPost, a.token, "", q, &token); err != nil || token.Error != "" {
		return ProviderIdentity{}, ErrProviderResponse
	}
	if a.cfg.Provider == GitHub {
		return a.githubIdentity(ctx, token.AccessToken, token.TokenType)
	}
	return a.verifyIDToken(ctx, token.IDToken, nonce)
}
