package client

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestTapeEjectPostsExactEndpoint(t *testing.T) {
	c := New("http://kura.example")
	c.HTTPClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %q, want %q", req.Method, http.MethodPost)
		}
		if req.URL.Path != "/api/tape/eject" {
			t.Fatalf("path = %q, want %q", req.URL.Path, "/api/tape/eject")
		}
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})

	if err := c.TapeEject(context.Background()); err != nil {
		t.Fatalf("TapeEject() error = %v", err)
	}
}
