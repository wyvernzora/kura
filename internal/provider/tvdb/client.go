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

	"github.com/wyvernzora/kura/internal/provider"
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
	return c.doJSON(ctx, http.MethodGet, path, values, nil, out, true, true)
}

func (c *client) doJSON(ctx context.Context, method, path string, values url.Values, body any, out any, withAuth bool, retryAuth bool) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("tvdb: encode request: %w", err)
		}
		payload = bytes.NewReader(encoded)
	}

	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("tvdb: build url: %w", err)
	}
	reqURL.RawQuery = values.Encode()

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), payload)
	if err != nil {
		return fmt.Errorf("tvdb: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
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
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized && withAuth {
		// TVDB bearer tokens are long lived but can expire or be invalidated.
		// Refresh once before surfacing the authorization failure.
		c.clearToken()
		if retryAuth {
			return c.doJSON(ctx, method, path, values, body, out, true, false)
		}
	}

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
