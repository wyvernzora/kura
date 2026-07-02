// Package client is the HTTP client the CLI uses to talk to a running
// `kura serve --rest`. CLI verbs dispatch through this package instead
// of importing internal/workflow directly, keeping the server as the
// normal filesystem writer.
//
// Discovery: `KURA_SERVER_URL` (default `http://127.0.0.1:8080`).
// Operator-only verbs (trash empty/restore, remove --purge,
// reconcile recover) set the X-Kura-Operator: 1 header. Destructive
// ops (trash empty, remove --purge) also set X-Confirm: 1.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the kura-serve REST endpoint when no env var is set.
	DefaultBaseURL = "http://127.0.0.1:8080"

	// EnvBaseURL is the env var name CLI consumers read for discovery.
	EnvBaseURL = "KURA_SERVER_URL"

	// EnvToken is the env var holding the bearer token sent on
	// every request. Same convention as the server's KURA_TOKEN
	// resolver — operators export it once and both ends pick it up.
	EnvToken = "KURA_TOKEN"

	// EnvDisableToken suppresses the bearer header on the client
	// when set truthy. Use for proxy-fronted deploys where the
	// proxy injects its own auth.
	EnvDisableToken = "KURA_DISABLE_TOKEN"

	headerOperator = "X-Kura-Operator"
	headerConfirm  = "X-Confirm"
	defaultTimeout = 5 * time.Minute
)

// Client wraps net/http with kura-aware request building and error
// decoding. One Client per CLI invocation; safe for sequential use.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client

	// Operator marks every request with X-Kura-Operator: 1. Set this
	// once on construction for operator-only verbs; the gate stays
	// off otherwise.
	Operator bool

	// BearerToken, when non-empty, is sent as Authorization: Bearer
	// <token> on every request. Read from KURA_TOKEN by FromEnv.
	BearerToken string
}

// New constructs a Client targeting baseURL. If empty, falls back to
// DefaultBaseURL. The HTTP client gets a 5-minute timeout sufficient
// for sync mutations; async submissions return ack quickly and
// long-poll happens via Job streaming.
func New(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &Client{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: defaultTimeout},
	}
}

// FromEnv constructs a Client using getenv(EnvBaseURL) or
// DefaultBaseURL when unset. Also reads KURA_TOKEN for the bearer
// header sent on every request; when KURA_DISABLE_TOKEN=1, the
// header is intentionally suppressed (proxy-fronted deploys where
// the proxy injects auth).
func FromEnv(getenv func(string) string) *Client {
	if getenv == nil {
		return New("")
	}
	c := New(getenv(EnvBaseURL))
	if !isTrue(getenv(EnvDisableToken)) {
		c.BearerToken = strings.TrimSpace(getenv(EnvToken))
	}
	return c
}

// isTrue mirrors auth.isTrue; duplicated to avoid importing the
// server-side auth package from the client.
func isTrue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// AsOperator returns a copy of c with the operator gate set. Use for
// verbs that need X-Kura-Operator: 1 without flipping the receiver's
// flag for unrelated calls.
func (c *Client) AsOperator() *Client {
	cp := *c
	cp.Operator = true
	return &cp
}

// ErrorEnvelope is the wire shape for non-2xx responses. Callers use
// errors.As on the returned error to inspect kind/category/data.
type ErrorEnvelope struct {
	Status   int            `json:"-"`
	Kind     string         `json:"kind"`
	Category string         `json:"category"`
	Message  string         `json:"message"`
	Data     map[string]any `json:"data,omitempty"`
}

func (e *ErrorEnvelope) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return fmt.Sprintf("kura server returned %d %s", e.Status, e.Kind)
	}
	return e.Message
}

// IsKind reports whether err is an ErrorEnvelope with the given kind
// string. Convenience for verbs that want to distinguish 404 not_found
// from other failures without unpacking the envelope.
func IsKind(err error, kind string) bool {
	var env *ErrorEnvelope
	if !errors.As(err, &env) {
		return false
	}
	return env.Kind == kind
}

// Do builds, executes, and decodes a JSON request. respBody can be
// nil for endpoints that return no body or whose body the caller
// wants to ignore. confirm controls X-Confirm: 1 (destructive ops).
//
// The transport returns:
//
//	(nil)             on 2xx with body decoded into respBody
//	(*ErrorEnvelope)  on JSON-shaped non-2xx
//	(error)           on transport / decode failures
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, reqBody, respBody any, confirm bool) error {
	full := c.BaseURL + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	req, err := buildHTTPRequest(ctx, method, full, reqBody)
	if err != nil {
		return err
	}
	setRequestHeaders(req, c, confirm)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return discoveryHint(err, c.BaseURL)
	}
	defer resp.Body.Close()
	return decodeHTTPResponse(resp, respBody)
}

// buildHTTPRequest marshals reqBody (when non-nil) and constructs the
// underlying *http.Request, including the Content-Type header for
// requests that carry a body. Encoding + request-build errors are
// wrapped with the conventional "encode request" / "build request"
// prefixes the rest of the package speaks.
func buildHTTPRequest(ctx context.Context, method, fullURL string, reqBody any) (*http.Request, error) {
	var bodyReader io.Reader
	if reqBody != nil {
		buf, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// setRequestHeaders attaches every per-request header the kura REST
// client speaks: Accept always, then the conditional operator /
// confirm gates, then the bearer-token authorization when configured.
func setRequestHeaders(req *http.Request, c *Client, confirm bool) {
	req.Header.Set("Accept", "application/json")
	if c.Operator {
		req.Header.Set(headerOperator, "1")
	}
	if confirm {
		req.Header.Set(headerConfirm, "1")
	}
	if c.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
	}
}

// decodeHTTPResponse interprets resp.Body. Non-2xx responses decode
// into an *ErrorEnvelope (with the 401 hint attached when the bearer
// gate rejected the call). 2xx responses with respBody!=nil and a
// non-204 status decode the JSON body into respBody.
func decodeHTTPResponse(resp *http.Response, respBody any) error {
	if resp.StatusCode >= 400 {
		var env ErrorEnvelope
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			return fmt.Errorf("server returned %d (%s) and body did not decode: %w", resp.StatusCode, resp.Status, err)
		}
		env.Status = resp.StatusCode
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("%w\n  hint: kura server requires a bearer token. Read it from /var/lib/kura/token (or wherever your server persists it) and set "+EnvToken+"=<value>",
				&env)
		}
		return &env
	}
	if respBody == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// discoveryHint wraps connection errors with a hint about starting
// the server, since this is by far the most common failure mode for
// a fresh `kura` invocation.
func discoveryHint(err error, baseURL string) error {
	if _, ok := errors.AsType[*url.Error](err); ok {
		return fmt.Errorf("cannot reach kura server at %s — is it running?\n  hint: start `kura serve --rest=:8080` or set %s=<url>\n  underlying: %w",
			baseURL, EnvBaseURL, err)
	}
	return err
}
