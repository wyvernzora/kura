package mcp

import (
	"context"

	"net/url"
	"strconv"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// listReleasesIn mirrors GET /api/v1/releases' query parameters.
type listReleasesIn struct {
	Ref    string `json:"ref,omitempty" jsonschema:"optional opaque metadata ref in provider:id form; omit to list recent matched releases across all refs"`
	Since  string `json:"since,omitempty" jsonschema:"RFC3339 timestamp; when present, page by first-matched time instead of publication time"`
	Limit  int    `json:"limit,omitempty" jsonschema:"maximum releases to return; clamped to 100"`
	Cursor string `json:"cursor,omitempty" jsonschema:"opaque nextCursor from the previous response"`
}

type infohashIn struct {
	Infohash string `json:"infohash" jsonschema:"canonical 40-hex v1 btih infohash"`
}

// listReleasesCap bounds a page into model context. REST keeps its larger
// ceiling for callers streaming to a UI.
const listReleasesCap = 100

func registerReleaseTools(srv *mcpsdk.Server, s *Server) {
	addTool(srv, &mcpsdk.Tool{
		Name:        "list_releases",
		Description: docListReleases,
		Annotations: readOnlyClosed(),
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in listReleasesIn) (*mcpsdk.CallToolResult, map[string]any, error) {
		start := time.Now()
		q := url.Values{}
		if in.Ref != "" {
			q.Set("ref", in.Ref)
		}
		if in.Since != "" {
			q.Set("since", in.Since)
		}
		if in.Cursor != "" {
			q.Set("cursor", in.Cursor)
		}
		limit := in.Limit
		if limit <= 0 || limit > listReleasesCap {
			limit = listReleasesCap
		}
		q.Set("limit", strconv.Itoa(limit))

		var out map[string]any
		err := s.releases.Get(ctx, "/releases", q, &out)
		s.observe(ctx, "list_releases", start, err, "ref", in.Ref, "limit", limit)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return nil, out, nil
	})

	addTool(srv, &mcpsdk.Tool{
		Name:        "get_release",
		Description: docGetRelease,
		Annotations: readOnlyClosed(),
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in infohashIn) (*mcpsdk.CallToolResult, map[string]any, error) {
		start := time.Now()
		var out map[string]any
		err := s.releases.Get(ctx, "/releases/"+url.PathEscape(in.Infohash), nil, &out)
		s.observe(ctx, "get_release", start, err, "infohash", in.Infohash)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return nil, out, nil
	})

	addTool(srv, &mcpsdk.Tool{
		Name:        "get_magnet",
		Description: docGetMagnet,
		Annotations: readOnlyClosed(),
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in infohashIn) (*mcpsdk.CallToolResult, map[string]any, error) {
		start := time.Now()
		var out map[string]any
		err := s.releases.Get(ctx, "/releases/"+url.PathEscape(in.Infohash)+"/magnet", nil, &out)
		// The magnet URI itself is never logged.
		s.observe(ctx, "get_magnet", start, err, "infohash", in.Infohash)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return nil, out, nil
	})
}
