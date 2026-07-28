package client

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDecodeAcceptsCreatedDocument(t *testing.T) {
	t.Parallel()

	var out struct {
		Ref string `json:"ref"`
	}
	err := (&Client{maxBody: 1024}).decode(&http.Response{
		StatusCode: http.StatusCreated,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"ref":"tvdb:12345"}`)),
	}, &out)
	if err != nil {
		t.Fatalf("decode 201 document: %v", err)
	}
	if out.Ref != "tvdb:12345" {
		t.Fatalf("decoded ref = %q, want tvdb:12345", out.Ref)
	}
}

func TestDecodeRejectsNoContentWhenDocumentExpected(t *testing.T) {
	t.Parallel()

	err := (&Client{maxBody: 1024}).decode(&http.Response{
		StatusCode: http.StatusNoContent,
		Header:     make(http.Header),
		Body:       http.NoBody,
	}, &struct{}{})
	if err == nil {
		t.Fatal("decode 204 unexpectedly succeeded")
	}
}
