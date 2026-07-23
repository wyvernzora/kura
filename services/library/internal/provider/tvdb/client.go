package tvdb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/wyvernzora/kura/services/library/internal/provider"
)

type client struct {
	apiKey             string
	baseURL            string
	httpClient         *http.Client
	tokenRefreshBefore time.Duration

	mu          sync.Mutex
	bearerToken string
	tokenExpiry time.Time
}

func newClient(apiKey string, opts Options) *client {
	baseURL := strings.TrimRight(opts.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultURL
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}

	refreshBefore := opts.TokenRefreshBefore
	if refreshBefore == 0 {
		refreshBefore = defaultRefreshAhead
	}

	return &client{
		apiKey:             apiKey,
		baseURL:            baseURL,
		httpClient:         httpClient,
		tokenRefreshBefore: refreshBefore,
	}
}

func (c *client) get(ctx context.Context, path string, values url.Values, out any) error {
	return c.doJSON(ctx, http.MethodGet, path, values, nil, out, true)
}

// doJSON sends one TVDB request, handling auth-token refresh on a 401
// via a second attempt. The initial attempt and the post-refresh retry
// share the same encoded body so a fresh request is built per attempt.
func (c *client) doJSON(ctx context.Context, method, path string, values url.Values, body any, out any, withAuth bool) error {
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("tvdb: encode request: %w", err)
		}
	}

	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("tvdb: build url: %w", err)
	}
	reqURL.RawQuery = values.Encode()

	// Two attempts: initial + one retry after a 401 clears the token.
	for attempt := range 2 {
		var payload io.Reader
		if encoded != nil {
			payload = bytes.NewReader(encoded)
		}
		req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), payload)
		if err != nil {
			return fmt.Errorf("tvdb: build request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		if encoded != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if withAuth {
			token, err := c.token(ctx)
			if err != nil {
				return err
			}
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("%w: %v", provider.ErrUnavailable, err)
		}

		if resp.StatusCode == http.StatusUnauthorized && withAuth && attempt == 0 {
			// TVDB bearer tokens are long lived but can expire or be
			// invalidated. Refresh once before surfacing.
			resp.Body.Close()
			c.clearToken()
			continue
		}

		return finishDoJSON(resp, out)
	}
	// Unreachable: loop body always returns or continues to the
	// retry, which is also limited to 2 iterations.
	return nil
}

func finishDoJSON(resp *http.Response, out any) error {
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return tvdbHTTPError(resp)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("tvdb: decode response: %w", err)
	}
	return nil
}

func tvdbHTTPError(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%w: status %d", provider.ErrUnauthorized, resp.StatusCode)
	case http.StatusNotFound:
		return fmt.Errorf("%w: status %d", provider.ErrNotFound, resp.StatusCode)
	default:
		return fmt.Errorf("%w: status %d", provider.ErrUnavailable, resp.StatusCode)
	}
}
