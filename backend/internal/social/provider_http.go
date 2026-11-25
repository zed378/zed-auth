package social

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxProviderResponse = 1 << 20

func (a *Adapter) request(ctx context.Context, method, endpoint, bearer string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return ErrProviderResponse
	}
	if req.URL.Scheme != "https" || req.URL.User != nil {
		return ErrProviderResponse
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "zed-auth-social")
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return ErrProviderResponse
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ErrProviderResponse
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderResponse+1))
	if err != nil || len(raw) > maxProviderResponse {
		return ErrProviderResponse
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return ErrProviderResponse
	}
	return nil
}
