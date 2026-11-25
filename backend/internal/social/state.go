package social

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const StateLifetime = 10 * time.Minute

type StateIntent string

const (
	SignInIntent StateIntent = "signin"
	LinkIntent   StateIntent = "link"
)

var (
	ErrInvalidStateBinding = errors.New("social: invalid state binding")
	ErrInvalidState        = errors.New("social: invalid or expired state")
	ErrStateUnavailable    = errors.New("social: state storage unavailable")
)

// StateBinding must come from the server's pending request and its browser
// cookie, never callback query parameters. RedirectURI is the configured callback.
type StateBinding struct {
	OrgID        string
	Provider     Provider
	Intent       StateIntent
	PendingID    string
	RedirectURI  string
	UserID       string
	SessionID    string
	BrowserToken string `json:"-"`
}

// StateTransaction contains credentials for one provider authorization attempt.
// Do not log it or send the verifier to a browser. State, Nonce and
// PKCEChallenge are provider authorization parameters.
type StateTransaction struct {
	State         string
	Nonce         string
	PKCEVerifier  string `json:"-"`
	PKCEChallenge string
	Binding       StateBinding
	ExpiresAt     time.Time
}

type stateRedis interface {
	SetNX(context.Context, string, interface{}, time.Duration) *redis.BoolCmd
	GetDel(context.Context, string) *redis.StringCmd
}

type StateStore struct {
	client stateRedis
	now    func() time.Time
}

func NewStateStore(client redis.UniversalClient) *StateStore {
	return &StateStore{client: client, now: time.Now}
}

type storedState struct {
	BindingDigest string    `json:"binding_digest"`
	Nonce         string    `json:"nonce"`
	Verifier      string    `json:"verifier"`
	ExpiresAt     time.Time `json:"expires_at"`
}

func (s *StateStore) Issue(ctx context.Context, binding StateBinding) (StateTransaction, error) {
	if !validStateBinding(binding) {
		return StateTransaction{}, ErrInvalidStateBinding
	}
	if s == nil || s.client == nil {
		return StateTransaction{}, ErrStateUnavailable
	}
	state, err := randomStateToken()
	if err != nil {
		return StateTransaction{}, ErrStateUnavailable
	}
	verifier, err := randomStateToken()
	if err != nil {
		return StateTransaction{}, ErrStateUnavailable
	}
	nonce := ""
	if binding.Provider != GitHub {
		nonce, err = randomStateToken()
		if err != nil {
			return StateTransaction{}, ErrStateUnavailable
		}
	}
	payload := storedState{
		BindingDigest: stateBindingDigest(binding), Nonce: nonce,
		Verifier: verifier, ExpiresAt: s.now().UTC().Add(StateLifetime),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return StateTransaction{}, ErrStateUnavailable
	}
	created, err := s.client.SetNX(ctx, stateKey(state), encoded, StateLifetime).Result()
	if err != nil || !created {
		return StateTransaction{}, ErrStateUnavailable
	}
	return stateTransaction(state, binding, payload), nil
}

// Consume spends the state before validation, so even simultaneous callbacks
// cannot exchange the same authorization code twice. A mismatch also spends it.
func (s *StateStore) Consume(ctx context.Context, state string, binding StateBinding) (StateTransaction, error) {
	if !validStateToken(state) || !validStateBinding(binding) {
		return StateTransaction{}, ErrInvalidState
	}
	if s == nil || s.client == nil {
		return StateTransaction{}, ErrStateUnavailable
	}
	encoded, err := s.client.GetDel(ctx, stateKey(state)).Result()
	if errors.Is(err, redis.Nil) {
		return StateTransaction{}, ErrInvalidState
	}
	if err != nil {
		return StateTransaction{}, ErrStateUnavailable
	}
	if len(encoded) > 2048 {
		return StateTransaction{}, ErrInvalidState
	}
	var payload storedState
	if json.Unmarshal([]byte(encoded), &payload) != nil ||
		!validStateToken(payload.Verifier) || !payload.ExpiresAt.After(s.now()) ||
		payload.ExpiresAt.After(s.now().Add(StateLifetime)) ||
		(binding.Provider == GitHub && payload.Nonce != "") ||
		(binding.Provider != GitHub && !validStateToken(payload.Nonce)) ||
		subtle.ConstantTimeCompare([]byte(payload.BindingDigest), []byte(stateBindingDigest(binding))) != 1 {
		return StateTransaction{}, ErrInvalidState
	}
	return stateTransaction(state, binding, payload), nil
}

func stateTransaction(state string, binding StateBinding, payload storedState) StateTransaction {
	challenge := sha256.Sum256([]byte(payload.Verifier))
	binding.BrowserToken = ""
	return StateTransaction{
		State: state, Nonce: payload.Nonce, PKCEVerifier: payload.Verifier,
		PKCEChallenge: base64.RawURLEncoding.EncodeToString(challenge[:]),
		Binding:       binding, ExpiresAt: payload.ExpiresAt,
	}
}

func validStateBinding(binding StateBinding) bool {
	org, err := uuid.Parse(binding.OrgID)
	if err != nil || org == uuid.Nil || !binding.Provider.Valid() ||
		!boundedStateString(binding.BrowserToken, 32, 256) ||
		!boundedStateString(binding.RedirectURI, 1, 2048) {
		return false
	}
	redirect, err := url.Parse(binding.RedirectURI)
	if err != nil || redirect.Scheme != "https" || redirect.Hostname() == "" ||
		redirect.User != nil || strings.Contains(binding.RedirectURI, "#") || redirect.Opaque != "" ||
		redirect.RawQuery != "" || redirect.ForceQuery || redirect.RawPath != "" ||
		redirect.Path != "/login/social/"+string(binding.Provider)+"/callback" {
		return false
	}
	switch binding.Intent {
	case SignInIntent:
		return boundedStateString(binding.PendingID, 1, 256) && binding.UserID == "" && binding.SessionID == ""
	case LinkIntent:
		return boundedStateString(binding.PendingID, 0, 256) &&
			boundedStateString(binding.UserID, 1, 256) && boundedStateString(binding.SessionID, 1, 256)
	default:
		return false
	}
}

func boundedStateString(value string, min, max int) bool {
	return len(value) >= min && len(value) <= max && utf8.ValidString(value) &&
		!strings.ContainsAny(value, "\x00\r\n")
}

func randomStateToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func validStateToken(token string) bool {
	if len(token) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == token
}

func stateKey(state string) string {
	digest := sha256.Sum256([]byte(state))
	return "social:state:" + hex.EncodeToString(digest[:])
}

func stateBindingDigest(binding StateBinding) string {
	// JSON length-prefixes string values, so adjacent fields cannot be confused.
	encoded, _ := json.Marshal(struct {
		StateBinding
		BrowserDigest string
	}{binding, stateKey(binding.BrowserToken)})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
