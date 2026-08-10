package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/pkg/api"
)

// HealthResponse mirrors the library-manager /healthz body.
type HealthResponse struct {
	Ok          bool      `json:"ok"`
	Version     string    `json:"version"`
	LibraryRoot string    `json:"libraryRoot"`
	UptimeMs    int64     `json:"uptimeMs"`
	StartedAt   time.Time `json:"startedAt"`
}

// LibraryResponse mirrors /api/library/v1.
type LibraryResponse struct {
	LibraryRoot string    `json:"libraryRoot"`
	SeriesCount int       `json:"seriesCount"`
	StartedAt   time.Time `json:"startedAt"`
	UptimeMs    int64     `json:"uptimeMs"`
}

// JobAck is the 202 body for async submissions.
type JobAck struct {
	JobID       string    `json:"jobId"`
	Kind        string    `json:"kind"`
	StatusURL   string    `json:"statusUrl"`
	StreamURL   string    `json:"streamUrl"`
	SubmittedAt time.Time `json:"submittedAt"`
}

// Health calls GET /healthz.
func (c *Client) Health(ctx context.Context) (HealthResponse, error) {
	var out HealthResponse
	err := c.Do(ctx, http.MethodGet, "/healthz", nil, nil, &out)
	return out, err
}

// Library calls GET /api/library/v1.
func (c *Client) Library(ctx context.Context) (LibraryResponse, error) {
	var out LibraryResponse
	err := c.Do(ctx, http.MethodGet, "/api/library/v1", nil, nil, &out)
	return out, err
}

// ListSeries calls GET /api/library/v1/series. airing is nil for "no filter",
// or a pointer for the airing-flag tri-state filter.
func (c *Client) ListSeries(ctx context.Context, statuses []string, airing *bool, tags []string, limit int, cursor string) (api.ListResult, error) {
	q := url.Values{}
	for _, s := range statuses {
		q.Add("status", s)
	}
	if airing != nil {
		if *airing {
			q.Set("airing", "true")
		} else {
			q.Set("airing", "false")
		}
	}
	for _, tag := range tags {
		q.Add("tags", tag)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var out api.ListResult
	err := c.Do(ctx, http.MethodGet, "/api/library/v1/series", q, nil, &out)
	return out, err
}

// UpdateTags calls PATCH /api/library/v1/series/{ref}/tags. Plain expressions add
// tags and expressions prefixed with ! remove tags.
func (c *Client) UpdateTags(ctx context.Context, ref string, tags []string) (api.SeriesTags, error) {
	var out api.SeriesTags
	err := c.Do(ctx, http.MethodPatch, "/api/library/v1/series/"+url.PathEscape(ref)+"/tags", nil, api.TagUpdate{Tags: tags}, &out)
	return out, err
}

// ShowOptions holds the GET /api/library/v1/series/{ref} query parameters.
type ShowOptions struct {
	Episodes   string
	Status     []string
	Source     []string
	Resolution []string
}

// ShowSeries calls GET /api/library/v1/series/{ref}.
func (c *Client) ShowSeries(ctx context.Context, ref string, opts ShowOptions) (api.Show, error) {
	q := url.Values{}
	if opts.Episodes != "" {
		q.Set("episodes", opts.Episodes)
	}
	for _, s := range opts.Status {
		q.Add("status", s)
	}
	for _, s := range opts.Source {
		q.Add("source", s)
	}
	for _, s := range opts.Resolution {
		q.Add("resolution", s)
	}
	var out api.Show
	err := c.Do(ctx, http.MethodGet, "/api/library/v1/series/"+url.PathEscape(ref), q, nil, &out)
	return out, err
}

// ResolveRequest is the POST /api/library/v1/series/resolve body.
type ResolveRequest struct {
	Terms []string `json:"terms"`
}

// Resolve calls POST /api/library/v1/series/resolve.
func (c *Client) Resolve(ctx context.Context, terms []string) (api.Resolution, error) {
	var out api.Resolution
	err := c.Do(ctx, http.MethodPost, "/api/library/v1/series/resolve", nil, ResolveRequest{Terms: terms}, &out)
	return out, err
}

// AddRequest is the POST /api/library/v1/series body. `Ref` is the metadata
// ref (provider:id); `Directory` overrides the new directory name.
// Field naming mirrors the MCP kura_add tool input shape.
type AddRequest struct {
	Ref       string `json:"ref"`
	Directory string `json:"directory,omitempty"`
	Ordering  string `json:"ordering,omitempty"`
}

// AddSeries calls POST /api/library/v1/series.
func (c *Client) AddSeries(ctx context.Context, req AddRequest) (api.AddResult, error) {
	var out api.AddResult
	err := c.Do(ctx, http.MethodPost, "/api/library/v1/series", nil, req, &out)
	return out, err
}

// ImportRequest is the POST /api/library/v1/series/import body. `Ref` is the
// metadata ref; `Directory` is the existing directory under the
// library root to adopt. Field naming mirrors MCP kura_import.
type ImportRequest struct {
	Ref       string `json:"ref"`
	Directory string `json:"directory"`
	Force     bool   `json:"force,omitempty"`
	Ordering  string `json:"ordering,omitempty"`
}

// ImportSeries calls POST /api/library/v1/series/import.
func (c *Client) ImportSeries(ctx context.Context, req ImportRequest) (api.AddResult, error) {
	var out api.AddResult
	err := c.Do(ctx, http.MethodPost, "/api/library/v1/series/import", nil, req, &out)
	return out, err
}

// ResetRequest is the POST /api/library/v1/series/{ref}/reset body.
type ResetRequest struct {
	Episode  string   `json:"episode,omitempty"`
	All      bool     `json:"all,omitempty"`
	TrashIDs []string `json:"trashIds,omitempty"`
	ExtraIDs []string `json:"extraIds,omitempty"`
}

// ResetSeries calls POST /api/library/v1/series/{ref}/reset.
func (c *Client) ResetSeries(ctx context.Context, ref string, req ResetRequest) (api.ResetResult, error) {
	var out api.ResetResult
	err := c.Do(ctx, http.MethodPost, "/api/library/v1/series/"+url.PathEscape(ref)+"/reset", nil, req, &out)
	return out, err
}

// ReconcilePlan calls POST /api/library/v1/series/{ref}/reconcile/plan.
func (c *Client) ReconcilePlan(ctx context.Context, ref string) (api.ReconcilePlan, error) {
	var out api.ReconcilePlan
	err := c.Do(ctx, http.MethodPost, "/api/library/v1/series/"+url.PathEscape(ref)+"/reconcile/plan", nil, nil, &out)
	return out, err
}

// ReconcileRecover calls POST /api/library/v1/series/{ref}/reconcile/recover.
func (c *Client) ReconcileRecover(ctx context.Context, ref string, force bool) (api.RecoverReconcile, error) {
	body := map[string]any{}
	if force {
		body["force"] = true
	}
	var out api.RecoverReconcile
	err := c.Do(ctx, http.MethodPost, "/api/library/v1/series/"+url.PathEscape(ref)+"/reconcile/recover", nil, body, &out)
	return out, err
}

// ScanRequest is the POST /api/library/v1/series/{ref}/scan body.
type ScanRequest struct {
	Refresh      bool   `json:"refresh,omitempty"`
	MetadataOnly bool   `json:"metadataOnly,omitempty"`
	Ordering     string `json:"ordering,omitempty"`
}

// SubmitScan returns a JobAck the caller can poll or stream.
func (c *Client) SubmitScan(ctx context.Context, ref string, req ScanRequest) (JobAck, error) {
	var out JobAck
	err := c.Do(ctx, http.MethodPost, "/api/library/v1/series/"+url.PathEscape(ref)+"/scan", nil, req, &out)
	return out, err
}

// SubmitApply returns a JobAck for reconcile apply.
func (c *Client) SubmitApply(ctx context.Context, ref, token string) (JobAck, error) {
	var out JobAck
	err := c.Do(ctx, http.MethodPost, "/api/library/v1/series/"+url.PathEscape(ref)+"/reconcile/apply", nil, map[string]string{"token": token}, &out)
	return out, err
}

// SubmitStage returns a JobAck for stage.
func (c *Client) SubmitStage(ctx context.Context, ref string, body any) (JobAck, error) {
	var out JobAck
	err := c.Do(ctx, http.MethodPost, "/api/library/v1/series/"+url.PathEscape(ref)+"/stage", nil, body, &out)
	return out, err
}

// TrashListSeries calls GET /api/library/v1/series/{ref}/trash.
func (c *Client) TrashListSeries(ctx context.Context, ref, olderThan string) (api.TrashList, error) {
	q := url.Values{}
	if olderThan != "" {
		q.Set("olderThan", olderThan)
	}
	var out api.TrashList
	err := c.Do(ctx, http.MethodGet, "/api/library/v1/series/"+url.PathEscape(ref)+"/trash", q, nil, &out)
	return out, err
}

// TrashListAll calls GET /api/library/v1/trash.
func (c *Client) TrashListAll(ctx context.Context, olderThan string) (api.TrashList, error) {
	q := url.Values{}
	if olderThan != "" {
		q.Set("olderThan", olderThan)
	}
	var out api.TrashList
	err := c.Do(ctx, http.MethodGet, "/api/library/v1/trash", q, nil, &out)
	return out, err
}

// TrashRestore calls POST /api/library/v1/series/{ref}/trash/{ulid}/restore.
func (c *Client) TrashRestore(ctx context.Context, ref, id string) (api.TrashRestore, error) {
	var out api.TrashRestore
	err := c.Do(ctx, http.MethodPost, "/api/library/v1/series/"+url.PathEscape(ref)+"/trash/"+url.PathEscape(id)+"/restore", nil, nil, &out)
	return out, err
}

// TrashEmptySeries calls DELETE /api/library/v1/series/{ref}/trash.
func (c *Client) TrashEmptySeries(ctx context.Context, ref, olderThan string) (api.TrashEmpty, error) {
	q := url.Values{}
	if olderThan != "" {
		q.Set("olderThan", olderThan)
	}
	var out api.TrashEmpty
	err := c.Do(ctx, http.MethodDelete, "/api/library/v1/series/"+url.PathEscape(ref)+"/trash", q, nil, &out)
	return out, err
}

// TrashEmptyAll calls DELETE /api/library/v1/trash.
func (c *Client) TrashEmptyAll(ctx context.Context, olderThan string) (api.TrashEmpty, error) {
	q := url.Values{}
	if olderThan != "" {
		q.Set("olderThan", olderThan)
	}
	var out api.TrashEmpty
	err := c.Do(ctx, http.MethodDelete, "/api/library/v1/trash", q, nil, &out)
	return out, err
}

// SubmitReindex calls POST /api/library/v1/reindex and returns the
// JobAck the caller streams via /jobs/{id}/stream.
func (c *Client) SubmitReindex(ctx context.Context) (JobAck, error) {
	var out JobAck
	err := c.Do(ctx, http.MethodPost, "/api/library/v1/reindex", nil, nil, &out)
	return out, err
}

// ScanAllRequest is the POST /api/library/v1/scan body.
type ScanAllRequest struct {
	Refresh      bool `json:"refresh,omitempty"`
	MetadataOnly bool `json:"metadataOnly,omitempty"`
	Concurrency  int  `json:"concurrency,omitempty"`
}

// SubmitScanAll calls POST /api/library/v1/scan and returns the
// JobAck the caller streams via /jobs/{id}/stream. The fan-out runs
// server-side; the response result decodes to api.ScanAllResult.
func (c *Client) SubmitScanAll(ctx context.Context, req ScanAllRequest) (JobAck, error) {
	var out JobAck
	err := c.Do(ctx, http.MethodPost, "/api/library/v1/scan", nil, req, &out)
	return out, err
}

// InboxListOptions holds the GET /api/library/v1/inbox query parameters.
// Zero values map to server defaults.
type InboxListOptions struct {
	Path          string
	Recursive     bool
	Depth         int
	Limit         int
	Kind          string
	NameGlob      string
	IncludeHidden bool
}

// InboxList calls GET /api/library/v1/inbox.
func (c *Client) InboxList(ctx context.Context, opts InboxListOptions) (api.InboxList, error) {
	q := url.Values{}
	if opts.Path != "" {
		q.Set("path", opts.Path)
	}
	if opts.Recursive {
		q.Set("recursive", "1")
	}
	if opts.Depth > 0 {
		q.Set("depth", strconv.Itoa(opts.Depth))
	}
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Kind != "" {
		q.Set("kind", opts.Kind)
	}
	if opts.NameGlob != "" {
		q.Set("name_glob", opts.NameGlob)
	}
	if opts.IncludeHidden {
		q.Set("include_hidden", "1")
	}
	var out api.InboxList
	err := c.Do(ctx, http.MethodGet, "/api/library/v1/inbox", q, nil, &out)
	return out, err
}
