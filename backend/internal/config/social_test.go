package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSocialConfigurationDefaultsDisabled(t *testing.T) {
	cfg, err := LoadFrom(env(valid()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Social.Google.Configured() || cfg.Social.Microsoft.Configured() || cfg.Social.GitHub.Configured() {
		t.Fatal("social providers must be opt in")
	}
}

func TestSocialConfigurationLoadsReferences(t *testing.T) {
	m := valid()
	for _, provider := range []string{"GOOGLE", "MICROSOFT", "GITHUB"} {
		m["AUTH_SOCIAL_"+provider+"_CLIENT_ID"] = "client-" + provider
		m["AUTH_SOCIAL_"+provider+"_CLIENT_SECRET_REF"] = "file:/run/secrets/" + provider
	}
	m["AUTH_SOCIAL_MICROSOFT_TENANT_ID"] = "11111111-1111-4111-8111-111111111111"
	cfg, err := LoadFrom(env(m))
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range []SocialProviderConfig{cfg.Social.Google, cfg.Social.Microsoft, cfg.Social.GitHub} {
		if !got.Configured() || !strings.HasPrefix(string(got.ClientSecretRef), "file:/run/secrets/") {
			t.Fatal("configured provider lost its secret reference")
		}
	}
	if cfg.Social.Microsoft.TenantID != m["AUTH_SOCIAL_MICROSOFT_TENANT_ID"] {
		t.Fatal("Microsoft issuer binding lost")
	}
	encoded, err := json.Marshal(cfg.Social)
	if err != nil || strings.Contains(string(encoded), "/run/secrets/") {
		t.Fatal("provider configuration serialized a secret reference")
	}
}

func TestSocialConfigurationRequiresHTTPSOrigin(t *testing.T) {
	for _, issuer := range []string{
		"http://localhost:8080", "https://auth.example.com/path",
		"https://auth.example.com?", "https://auth.example.com?q=x",
		"https://user:password@auth.example.com", "https://auth.example.com#fragment",
		"https://auth.example.com#",
	} {
		t.Run(issuer, func(t *testing.T) {
			m := valid()
			m["AUTH_ISSUER"] = issuer
			m["AUTH_SOCIAL_GOOGLE_CLIENT_ID"] = "client"
			m["AUTH_SOCIAL_GOOGLE_CLIENT_SECRET_REF"] = "env:GOOGLE_SECRET"
			if cfg, err := LoadFrom(env(m)); err == nil || cfg != nil {
				t.Fatal("configured provider accepted unsafe callback origin")
			}
		})
	}
}

func TestSocialConfigurationRejectsPartialOrUnsafeCredentials(t *testing.T) {
	const pastedSecret = "do-not-echo-this-client-secret"
	for name, settings := range map[string]map[string]string{
		"missing reference":     {"AUTH_SOCIAL_GOOGLE_CLIENT_ID": "client"},
		"missing client":        {"AUTH_SOCIAL_GITHUB_CLIENT_SECRET_REF": "env:GITHUB_SECRET"},
		"plaintext secret":      {"AUTH_SOCIAL_GOOGLE_CLIENT_ID": "client", "AUTH_SOCIAL_GOOGLE_CLIENT_SECRET_REF": pastedSecret},
		"empty reference":       {"AUTH_SOCIAL_GOOGLE_CLIENT_ID": "client", "AUTH_SOCIAL_GOOGLE_CLIENT_SECRET_REF": "file:"},
		"unsupported reference": {"AUTH_SOCIAL_GOOGLE_CLIENT_ID": "client", "AUTH_SOCIAL_GOOGLE_CLIENT_SECRET_REF": "https://secret.example/"},
		"wildcard tenant":       {"AUTH_SOCIAL_MICROSOFT_CLIENT_ID": "client", "AUTH_SOCIAL_MICROSOFT_CLIENT_SECRET_REF": "env:MS_SECRET", "AUTH_SOCIAL_MICROSOFT_TENANT_ID": "common"},
		"missing tenant":        {"AUTH_SOCIAL_MICROSOFT_CLIENT_ID": "client", "AUTH_SOCIAL_MICROSOFT_CLIENT_SECRET_REF": "env:MS_SECRET"},
		"zero tenant":           {"AUTH_SOCIAL_MICROSOFT_CLIENT_ID": "client", "AUTH_SOCIAL_MICROSOFT_CLIENT_SECRET_REF": "env:MS_SECRET", "AUTH_SOCIAL_MICROSOFT_TENANT_ID": "00000000-0000-0000-0000-000000000000"},
		"tenant alone":          {"AUTH_SOCIAL_MICROSOFT_TENANT_ID": "11111111-1111-4111-8111-111111111111"},
	} {
		t.Run(name, func(t *testing.T) {
			m := valid()
			for key, value := range settings {
				m[key] = value
			}
			cfg, err := LoadFrom(env(m))
			if err == nil || cfg != nil {
				t.Fatal("unsafe configuration accepted")
			}
			if strings.Contains(err.Error(), pastedSecret) {
				t.Fatal("configuration error disclosed a client secret")
			}
		})
	}
}
