package social

import (
	"context"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
)

func (a *Adapter) githubIdentity(ctx context.Context, token, tokenType string) (ProviderIdentity, error) {
	if token == "" || len(token) > 8192 || strings.ContainsAny(token, "\r\n\x00") || !strings.EqualFold(tokenType, "bearer") {
		return ProviderIdentity{}, ErrProviderResponse
	}
	var profile struct {
		ID int64 `json:"id"`
	}
	if err := a.request(ctx, http.MethodGet, "https://api.github.com/user", token, nil, &profile); err != nil || profile.ID <= 0 {
		return ProviderIdentity{}, ErrProviderResponse
	}
	var emails []githubEmail
	if err := a.request(ctx, http.MethodGet, "https://api.github.com/user/emails", token, nil, &emails); err != nil || !hasVerifiedEmail(emails) {
		return ProviderIdentity{}, ErrProviderResponse
	}
	return ProviderIdentity{Provider: GitHub, Subject: strconv.FormatInt(profile.ID, 10)}, nil
}

type githubEmail struct {
	Email    string `json:"email"`
	Verified bool   `json:"verified"`
}

func hasVerifiedEmail(emails []githubEmail) bool {
	for _, e := range emails {
		parsed, err := mail.ParseAddress(e.Email)
		if e.Verified && err == nil && parsed.Address == e.Email && len(e.Email) <= 320 {
			return true
		}
	}
	return false
}
