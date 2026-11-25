package config

import (
	"net/url"
	"strings"

	"github.com/google/uuid"
)

// SocialConfig contains deployment credentials; organization policy separately
// decides which configured providers a user may use.
type SocialConfig struct {
	Google    SocialProviderConfig
	Microsoft SocialProviderConfig
	GitHub    SocialProviderConfig
}

type SocialProviderConfig struct {
	ClientID        string
	ClientSecretRef SecretRef `json:"-"`
	// TenantID pins Microsoft's issuer. The identity key is provider + subject,
	// so accepting arbitrary Microsoft issuers would make that key ambiguous.
	TenantID string
}

func (c SocialProviderConfig) Configured() bool {
	return c.ClientID != "" && c.ClientSecretRef != ""
}

func (l *loader) social() SocialConfig {
	return SocialConfig{
		Google:    l.socialProvider("GOOGLE", false),
		Microsoft: l.socialProvider("MICROSOFT", true),
		GitHub:    l.socialProvider("GITHUB", false),
	}
}

func (l *loader) socialProvider(provider string, microsoft bool) SocialProviderConfig {
	prefix := "AUTH_SOCIAL_" + provider
	c := SocialProviderConfig{
		ClientID:        l.optional(prefix+"_CLIENT_ID", ""),
		ClientSecretRef: SecretRef(l.optional(prefix+"_CLIENT_SECRET_REF", "")),
	}
	if microsoft {
		c.TenantID = l.optional(prefix+"_TENANT_ID", "")
	}
	if c.ClientID == "" && c.ClientSecretRef == "" && c.TenantID == "" {
		return c
	}
	if c.ClientID == "" {
		l.problem("%s_CLIENT_ID is required when this provider is configured", prefix)
	} else if len(c.ClientID) > 512 || strings.ContainsAny(c.ClientID, "\r\n\x00") {
		l.problem("%s_CLIENT_ID must be at most 512 bytes without control characters", prefix)
	}
	if c.ClientSecretRef == "" {
		l.problem("%s_CLIENT_SECRET_REF is required when this provider is configured", prefix)
	} else {
		scheme, name, ok := strings.Cut(string(c.ClientSecretRef), ":")
		if !ok || (scheme != "file" && scheme != "env") || strings.TrimSpace(name) == "" || strings.ContainsAny(name, "\r\n\x00") {
			// Never include the supplied value: it may be a pasted secret.
			l.problem("%s_CLIENT_SECRET_REF must use a nonempty file: or env: reference", prefix)
		}
	}
	if microsoft {
		id, err := uuid.Parse(c.TenantID)
		if err != nil || id == uuid.Nil || id.String() != c.TenantID {
			l.problem("%s_TENANT_ID must be a canonical nonzero tenant UUID", prefix)
		}
	}
	return c
}

func (l *loader) validateSocial(c *Config) {
	if !c.Social.Google.Configured() && !c.Social.Microsoft.Configured() && !c.Social.GitHub.Configured() {
		return
	}
	u, err := url.Parse(c.Issuer)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		u.Opaque != "" || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(c.Issuer, "#") {
		l.problem("AUTH_ISSUER must be an HTTPS origin when social providers are configured")
	}
}
