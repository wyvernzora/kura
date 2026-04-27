package tvdb

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type loginRequest struct {
	APIKey string `json:"apikey"`
}

type loginResponse struct {
	Data struct {
		Token string `json:"token"`
	} `json:"data"`
	Status string `json:"status"`
}

func (c *client) token(ctx context.Context) (string, error) {
	now := time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.bearerToken != "" && now.Add(c.tokenRefreshBefore).Before(c.tokenExpiry) {
		return c.bearerToken, nil
	}

	// TVDB tokens are documented as roughly 30-day credentials. Kura keeps them
	// in process only; no provider data or token state is persisted to disk.
	var out loginResponse
	err := c.doJSON(ctx, http.MethodPost, "/login", nil, loginRequest{APIKey: c.apiKey}, &out, false, false)
	if err != nil {
		return "", err
	}
	if out.Data.Token == "" {
		return "", fmt.Errorf("%w: empty token", ErrUnauthorized)
	}

	c.bearerToken = out.Data.Token
	c.tokenExpiry = now.Add(30 * 24 * time.Hour)
	return c.bearerToken, nil
}

func (c *client) clearToken() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.bearerToken = ""
	c.tokenExpiry = time.Time{}
}
