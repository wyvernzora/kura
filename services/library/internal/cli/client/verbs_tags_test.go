package client

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestUpdateTagsRequest(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	c := New("http://kura.invalid")
	c.HTTPClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotMethod = req.Method
		gotPath = req.URL.EscapedPath()
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		gotBody = string(body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"metadataRef":"tvdb:42","tags":["priority"]}`)),
		}, nil
	})
	out, err := c.UpdateTags(context.Background(), "tvdb:42", []string{"Priority", "!Disabled"})
	if err != nil {
		t.Fatalf("UpdateTags: %v", err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/api/v1/series/tvdb:42/tags" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
	if gotBody != `{"tags":["Priority","!Disabled"]}` {
		t.Fatalf("body = %s", gotBody)
	}
	if len(out.Tags) != 1 || out.Tags[0] != "priority" {
		t.Fatalf("response = %+v", out)
	}
}

func TestListSeriesSendsTagFilter(t *testing.T) {
	var got []string
	c := New("http://kura.invalid")
	c.HTTPClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		got = req.URL.Query()["tags"]
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"rows":[]}`)),
		}, nil
	})
	if _, err := c.ListSeries(context.Background(), nil, nil, []string{"priority", "!disabled"}, 100, ""); err != nil {
		t.Fatalf("ListSeries: %v", err)
	}
	if len(got) != 2 || got[0] != "priority" || got[1] != "!disabled" {
		t.Fatalf("tags query = %#v", got)
	}
}
