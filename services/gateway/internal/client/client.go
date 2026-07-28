// Package client is the bridge's REST client for the two leaf services.
//
// It declares its own request and response structs and imports neither leaf's
// pkg/api. That keeps services/gateway a service-local Docker build context
// like every other image: importing the leaves would make this the only build
// needing go.work and all three module trees. Agreement with the leaves is
// enforced by contract tests, not by a module edge.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Error is the suite error envelope, as emitted by both leaves and by this
// package when it cannot reach one.
type Error struct {
	Status  int            `json:"-"`
	Kind    string         `json:"kind"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message == "" {
		return fmt.Sprintf("upstream returned %d %s", e.Status, e.Kind)
	}
	return e.Message
}

// Error kinds this package originates. Everything else is decoded from the
// leaf's own envelope and passed through unchanged.
const (
	KindBackendUnavailable = "backend_unavailable"
	KindInternal           = "internal"
)

// Client calls one leaf. Two are constructed — one per upstream — so the
// §4 path split is a routing decision made once by the caller rather than a
// string comparison repeated in every tool handler.
type Client struct {
	baseURL    string
	healthURL  string
	http       *http.Client
	maxBody    int64
	reqTimeout time.Duration
}

// Options are the per-client timeouts and limits.
type Options struct {
	RequestTimeout   time.Duration
	MaxResponseBytes int64
}

// New builds a client for an upstream given as host:port. The /api/v1 base and
// the /healthz probe are both derived here so callers cannot spell either
// inconsistently.
func New(upstream string, opts Options) *Client {
	upstream = strings.TrimSuffix(strings.TrimSpace(upstream), "/")
	if !strings.Contains(upstream, "://") {
		upstream = "http://" + upstream
	}
	return &Client{
		baseURL:   upstream + "/api/v1",
		healthURL: upstream + "/healthz",
		http: &http.Client{
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				ResponseHeaderTimeout: opts.RequestTimeout,
				MaxIdleConnsPerHost:   4,
			},
			Timeout: opts.RequestTimeout,
		},
		maxBody:    opts.MaxResponseBytes,
		reqTimeout: opts.RequestTimeout,
	}
}

// HealthURL is the leaf's own /healthz. The bridge probes this directly rather
// than going through Caddy, whose /healthz and /api/v1/health both route back
// to the bridge itself.
func (c *Client) HealthURL() string { return c.healthURL }

// Get issues a GET and decodes a 2xx JSON body into out.
func (c *Client) Get(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, out)
}

// Post issues a POST and decodes a 2xx JSON body into out. Mutations are never
// retried: a retried stage or apply is a second filesystem effect, and the
// bridge cannot tell a lost response from an unprocessed request.
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, nil, body, out)
}

// Patch issues a PATCH and decodes a 2xx JSON body into out.
func (c *Client) Patch(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPatch, path, nil, body, out)
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, c.reqTimeout)
	defer cancel()

	full := c.baseURL + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return &Error{Status: 0, Kind: KindInternal, Message: "encode request: " + err.Error()}
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, full, reader)
	if err != nil {
		return &Error{Status: 0, Kind: KindInternal, Message: "build request: " + err.Error()}
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if id := RequestIDFromContext(ctx); id != "" {
		req.Header.Set("X-Request-Id", id)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// Context cancellation is the caller giving up, not the leaf
		// being down; reporting it as backend_unavailable would blame
		// the wrong side.
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			return &Error{Status: 0, Kind: KindBackendUnavailable, Message: "upstream call cancelled or timed out"}
		}
		return &Error{Status: 0, Kind: KindBackendUnavailable, Message: "upstream service is unreachable"}
	}
	defer resp.Body.Close()

	return c.decode(resp, out)
}

// decode turns one upstream response into either a decoded body or the suite
// error envelope.
func (c *Client) decode(resp *http.Response, out any) error {
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, c.maxBody))
	if readErr != nil {
		return &Error{Status: resp.StatusCode, Kind: KindBackendUnavailable, Message: "upstream response could not be read"}
	}

	if resp.StatusCode >= 400 {
		return decodeErrorEnvelope(resp.StatusCode, resp.Header.Get("Content-Type"), raw)
	}
	// An unexpected 2xx is not success: a 204 where a document was
	// promised means the contract moved.
	if resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusCreated &&
		resp.StatusCode != http.StatusAccepted {
		return &Error{Status: resp.StatusCode, Kind: KindInternal,
			Message: fmt.Sprintf("upstream returned unexpected status %d", resp.StatusCode)}
	}
	if out == nil {
		return nil
	}
	if err := requireJSON(resp.Header.Get("Content-Type")); err != nil {
		return &Error{Status: resp.StatusCode, Kind: KindBackendUnavailable, Message: err.Error()}
	}
	if err := strictUnmarshal(raw, out); err != nil {
		return &Error{Status: resp.StatusCode, Kind: KindInternal,
			Message: "upstream response did not decode: " + err.Error()}
	}
	return nil
}

// decodeErrorEnvelope turns a non-2xx into the suite envelope. A body that is
// not the envelope means something other than a leaf answered — a proxy, most
// likely — so it reports backend_unavailable rather than inventing a kind.
func decodeErrorEnvelope(status int, contentType string, raw []byte) error {
	if err := requireJSON(contentType); err != nil {
		return &Error{Status: status, Kind: KindBackendUnavailable, Message: err.Error()}
	}
	var env Error
	if err := strictUnmarshal(raw, &env); err != nil || env.Kind == "" {
		return &Error{Status: status, Kind: KindBackendUnavailable,
			Message: fmt.Sprintf("upstream returned %d with an unrecognized error body", status)}
	}
	env.Status = status
	return &env
}

func requireJSON(contentType string) error {
	if contentType == "" {
		return errors.New("upstream response had no content type")
	}
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil || mt != "application/json" {
		return fmt.Errorf("upstream response was %q, want application/json", contentType)
	}
	return nil
}

// strictUnmarshal rejects trailing content. It deliberately does NOT reject
// unknown fields: the leaves may add response fields without the gateway
// having been rebuilt, and refusing those would make every additive leaf
// change a gateway outage.
func strictUnmarshal(raw []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	// UseNumber keeps int64 exact through a map[string]any round-trip.
	// Without it a sizeBytes near 2^53 would come back rounded.
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("unexpected trailing content")
	}
	return nil
}
