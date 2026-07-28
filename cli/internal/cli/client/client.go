// Package client is the HTTP client the CLI uses to talk to a running
// kura serve REST transport. CLI verbs dispatch through this package instead
// of importing internal/workflow directly, keeping the server as the
// normal filesystem writer.
//
// Discovery: `KURA_SERVER_URL` (default `http://127.0.0.1:8080`), plus
// `KURA_TOKEN` for the bearer header a fronting proxy may want.
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

	// EnvToken is the env var holding the bearer token sent on every
	// request. Kura itself no longer authenticates; the header is
	// kept for whatever proxy fronts the deployment.
	EnvToken = "KURA_TOKEN"

	defaultTimeout = 5 * time.Minute

	// maxErrBodyBytes caps how much of an error body is buffered, and
	// errBodySnippetMax how much of it an undecodable body echoes
	// back: enough to recognize a proxy's HTML error page, short
	// enough not to bury the real message.
	maxErrBodyBytes   = 1 << 20
	errBodySnippetMax = 256
)

// Client wraps net/http with kura-aware request building and error
// decoding. One Client per CLI invocation; safe for sequential use.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client

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
// header sent on every request.
func FromEnv(getenv func(string) string) *Client {
	if getenv == nil {
		return New("")
	}
	c := New(getenv(EnvBaseURL))
	c.BearerToken = strings.TrimSpace(getenv(EnvToken))
	return c
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
// wants to ignore.
//
// The transport returns:
//
//	(nil)             on 2xx with body decoded into respBody
//	(*ErrorEnvelope)  on JSON-shaped non-2xx
//	(error)           on transport / decode failures
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, reqBody, respBody any) error {
	full := c.BaseURL + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	req, err := buildHTTPRequest(ctx, method, full, reqBody)
	if err != nil {
		return err
	}
	setRequestHeaders(req, c)

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
// client speaks: Accept always, then the bearer-token authorization
// when configured.
func setRequestHeaders(req *http.Request, c *Client) {
	req.Header.Set("Accept", "application/json")
	if c.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
	}
}

// decodeHTTPResponse interprets resp.Body. Non-2xx responses go to
// decodeErrorBody. 2xx responses with respBody!=nil and a non-204
// status decode the JSON body into respBody.
func decodeHTTPResponse(resp *http.Response, respBody any) error {
	if resp.StatusCode >= 400 {
		return decodeErrorBody(resp)
	}
	if respBody == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// decodeErrorBody turns a non-2xx response into an *ErrorEnvelope.
// When the body is not the envelope at all — a proxy's HTML error
// page, most likely — the returned error carries a bounded snippet
// of it, so the operator can tell who actually answered. Shared with
// the SSE path, where a fronting proxy is the likeliest respondent.
func decodeErrorBody(resp *http.Response) error {
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrBodyBytes))
	var env ErrorEnvelope
	// Decoder, not Unmarshal: it stops at the first JSON value and
	// tolerates trailing bytes, matching what this path did before
	// the body had to be buffered for the snippet.
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&env); err != nil {
		return fmt.Errorf("server returned %d (%s) and body did not decode: %w\n  body: %s",
			resp.StatusCode, resp.Status, errors.Join(err, readErr), bodySnippet(body))
	}
	env.Status = resp.StatusCode
	return &env
}

// bodySnippet renders an undecodable error body on one line, capped
// at errBodySnippetMax bytes. ToValidUTF8 runs after the cut so it
// drops both the partial rune a byte-wise cut leaves behind and any
// garbage a binary body carried in the first place.
func bodySnippet(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	if len(s) > errBodySnippetMax {
		s = strings.ToValidUTF8(s[:errBodySnippetMax], "") + "…"
	} else {
		s = strings.ToValidUTF8(s, "")
	}
	if s == "" {
		return "(empty)"
	}
	return s
}

// discoveryHint wraps connection errors with a hint about starting
// the server, since this is by far the most common failure mode for
// a fresh `kura` invocation.
func discoveryHint(err error, baseURL string) error {
	if _, ok := errors.AsType[*url.Error](err); ok {
		return fmt.Errorf("cannot reach kura server at %s — is it running?\n  hint: start `kura-library-manager --config=/path/to/library-manager.toml` or set %s=<url>\n  underlying: %w",
			baseURL, EnvBaseURL, err)
	}
	return err
}
