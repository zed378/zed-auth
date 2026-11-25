package social

import (
	"errors"
	"testing"

	"github.com/zed378/zed-auth/backend/internal/config"
)

type unusedSecrets struct{ calls int }

func (s *unusedSecrets) Resolve(config.SecretRef) ([]byte, error) {
	s.calls++
	return nil, errors.New("must not resolve during construction")
}

func TestConfiguredAdaptersDeriveCallbacksAndDelaySecretResolution(t *testing.T) {
	c := &config.Config{Issuer: "https://auth.example.com", Social: config.SocialConfig{
		Google:    config.SocialProviderConfig{ClientID: "google-client", ClientSecretRef: "env:GOOGLE_SECRET"},
		Microsoft: config.SocialProviderConfig{ClientID: "ms-client", ClientSecretRef: "env:MS_SECRET", TenantID: "11111111-1111-4111-8111-111111111111"},
		GitHub:    config.SocialProviderConfig{ClientID: "github-client", ClientSecretRef: "file:/run/secrets/github"},
	}}
	secrets := &unusedSecrets{}
	adapters, err := ConfiguredAdapters(c, secrets)
	if err != nil || len(adapters) != 3 {
		t.Fatalf("adapters = %d, err = %v", len(adapters), err)
	}
	for provider, adapter := range adapters {
		if adapter.cfg.Provider != provider || adapter.cfg.RedirectURI != c.Issuer+"/login/social/"+string(provider)+"/callback" {
			t.Fatal("provider callback did not bind to service origin")
		}
	}
	if secrets.calls != 0 {
		t.Fatal("provider construction resolved a secret")
	}
}

func TestConfiguredAdaptersRejectPartialConfiguration(t *testing.T) {
	for _, c := range []*config.Config{
		nil,
		{Issuer: "https://auth.example.com", Social: config.SocialConfig{Google: config.SocialProviderConfig{ClientID: "client"}}},
		{Issuer: "http://auth.example.com", Social: config.SocialConfig{Google: config.SocialProviderConfig{ClientID: "client", ClientSecretRef: "env:GOOGLE_SECRET"}}},
	} {
		if _, err := ConfiguredAdapters(c, &unusedSecrets{}); !errors.Is(err, ErrProviderConfig) {
			t.Fatalf("partial or unsafe config: %v", err)
		}
	}
}

func TestConfiguredAdaptersAllowDisabledDeployment(t *testing.T) {
	got, err := ConfiguredAdapters(&config.Config{}, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("disabled deployment = %v, %v", got, err)
	}
}
