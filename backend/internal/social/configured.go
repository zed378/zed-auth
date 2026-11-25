package social

import "github.com/zed378/zed-auth/backend/internal/config"

// ConfiguredAdapters constructs the configured providers without network calls.
// Callback URLs are derived from the service issuer, never a browser parameter.
func ConfiguredAdapters(c *config.Config, secrets config.SecretResolver) (map[Provider]*Adapter, error) {
	if c == nil {
		return nil, ErrProviderConfig
	}
	adapters := make(map[Provider]*Adapter)
	for _, item := range []struct {
		provider Provider
		config   config.SocialProviderConfig
	}{
		{Google, c.Social.Google},
		{Microsoft, c.Social.Microsoft},
		{GitHub, c.Social.GitHub},
	} {
		if item.config == (config.SocialProviderConfig{}) {
			continue
		}
		adapter, err := NewAdapter(ProviderConfig{
			Provider: item.provider, ClientID: item.config.ClientID,
			ClientSecretRef:   item.config.ClientSecretRef,
			RedirectURI:       c.Issuer + "/login/social/" + string(item.provider) + "/callback",
			MicrosoftTenantID: item.config.TenantID,
		}, secrets)
		if err != nil {
			return nil, ErrProviderConfig
		}
		adapters[item.provider] = adapter
	}
	return adapters, nil
}
