package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/wyvernzora/kura/services/library-manager/internal/response"
)

// HealthResponse mirrors the /api/v1/health body.
type HealthResponse struct {
	Ok          bool      `json:"ok"`
	Version     string    `json:"version"`
	LibraryRoot string    `json:"libraryRoot"`
	UptimeMs    int64     `json:"uptimeMs"`
	StartedAt   time.Time `json:"startedAt"`
}

// LibraryResponse mirrors /api/v1/library.
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

// Health calls GET /api/v1/health.
func (c *Client) Health(ctx context.Context) (HealthResponse, error) {
	var out HealthResponse
	err := c.Do(ctx, http.MethodGet, "/api/v1/health", nil, nil, &out, false)
	return out, err
}

// Library calls GET /api/v1/library.
func (c *Client) Library(ctx context.Context) (LibraryResponse, error) {
	var out LibraryResponse
	err := c.Do(ctx, http.MethodGet, "/api/v1/library", nil, nil, &out, false)
	return out, err
}

// ListSeries calls GET /api/v1/series. airing is nil for "no filter",
// or a pointer for the airing-flag tri-state filter.
func (c *Client) ListSeries(ctx context.Context, statuses []string, airing *bool, tags []string, limit int, cursor string) (response.ListResult, error) {
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
	var out response.ListResult
	err := c.Do(ctx, http.MethodGet, "/api/v1/series", q, nil, &out, false)
	return out, err
}

// UpdateTags calls PATCH /api/v1/series/{ref}/tags. Plain expressions add
// tags and expressions prefixed with ! remove tags.
func (c *Client) UpdateTags(ctx context.Context, ref string, tags []string) (response.SeriesTags, error) {
	var out response.SeriesTags
	err := c.Do(ctx, http.MethodPatch, "/api/v1/series/"+url.PathEscape(ref)+"/tags", nil, response.TagUpdate{Tags: tags}, &out, false)
	return out, err
}

// ShowOptions holds the GET /api/v1/series/{ref} query parameters.
type ShowOptions struct {
	Episodes   string
	Status     []string
	Source     []string
	Resolution []string
}

// ShowSeries calls GET /api/v1/series/{ref}.
func (c *Client) ShowSeries(ctx context.Context, ref string, opts ShowOptions) (response.Show, error) {
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
	var out response.Show
	err := c.Do(ctx, http.MethodGet, "/api/v1/series/"+url.PathEscape(ref), q, nil, &out, false)
	return out, err
}

// ResolveRequest is the POST /api/v1/resolve body.
type ResolveRequest struct {
	Terms []string `json:"terms"`
}

// Resolve calls POST /api/v1/resolve.
func (c *Client) Resolve(ctx context.Context, terms []string) (response.Resolution, error) {
	var out response.Resolution
	err := c.Do(ctx, http.MethodPost, "/api/v1/resolve", nil, ResolveRequest{Terms: terms}, &out, false)
	return out, err
}

// AddRequest is the POST /api/v1/series body. `Ref` is the metadata
// ref (provider:id); `Dirname` overrides the new directory name.
// Field naming mirrors the MCP kura_add tool input shape.
type AddRequest struct {
	Ref      string `json:"ref"`
	Dirname  string `json:"dirname,omitempty"`
	Ordering string `json:"ordering,omitempty"`
}

// AddSeries calls POST /api/v1/series.
func (c *Client) AddSeries(ctx context.Context, req AddRequest) (response.AddResult, error) {
	var out response.AddResult
	err := c.Do(ctx, http.MethodPost, "/api/v1/series", nil, req, &out, false)
	return out, err
}

// ImportRequest is the POST /api/v1/series/import body. `Ref` is the
// metadata ref; `Dirname` is the existing directory under the
// library root to adopt. Field naming mirrors MCP kura_import.
type ImportRequest struct {
	Ref      string `json:"ref"`
	Dirname  string `json:"dirname"`
	Force    bool   `json:"force,omitempty"`
	Ordering string `json:"ordering,omitempty"`
}

// ImportSeries calls POST /api/v1/series/import.
func (c *Client) ImportSeries(ctx context.Context, req ImportRequest) (response.AddResult, error) {
	var out response.AddResult
	err := c.Do(ctx, http.MethodPost, "/api/v1/series/import", nil, req, &out, false)
	return out, err
}

// RemoveSeries calls DELETE /api/v1/series/{ref}. Set purge=true for
// the wholesale-delete flow (operator + confirm).
func (c *Client) RemoveSeries(ctx context.Context, ref string, purge bool) (response.Remove, error) {
	q := url.Values{}
	if purge {
		q.Set("purge", "1")
	}
	var out response.Remove
	confirm := purge
	err := c.Do(ctx, http.MethodDelete, "/api/v1/series/"+url.PathEscape(ref), q, nil, &out, confirm)
	return out, err
}

// ResetRequest is the POST /api/v1/series/{ref}/reset body.
type ResetRequest struct {
	Episode  string   `json:"episode,omitempty"`
	All      bool     `json:"all,omitempty"`
	TrashIDs []string `json:"trashIds,omitempty"`
	ExtraIDs []string `json:"extraIds,omitempty"`
}

// ResetSeries calls POST /api/v1/series/{ref}/reset.
func (c *Client) ResetSeries(ctx context.Context, ref string, req ResetRequest) (response.ResetResult, error) {
	var out response.ResetResult
	err := c.Do(ctx, http.MethodPost, "/api/v1/series/"+url.PathEscape(ref)+"/reset", nil, req, &out, false)
	return out, err
}

// ListAliases calls GET /api/v1/series/{ref}/aliases.
func (c *Client) ListUserAliases(ctx context.Context, ref string) (response.UserAliasList, error) {
	var out response.UserAliasList
	err := c.Do(ctx, http.MethodGet, "/api/v1/series/"+url.PathEscape(ref)+"/aliases", nil, nil, &out, false)
	return out, err
}

// AddAliases calls POST /api/v1/series/{ref}/aliases. Returns the
// resulting alias list. Idempotent.
func (c *Client) AddUserAliases(ctx context.Context, ref string, aliases []string) (response.UserAliasList, error) {
	var out response.UserAliasList
	err := c.Do(ctx, http.MethodPost, "/api/v1/series/"+url.PathEscape(ref)+"/aliases", nil, response.UserAliasMutation{Aliases: aliases}, &out, false)
	return out, err
}

// RemoveAliases calls DELETE /api/v1/series/{ref}/aliases. Returns
// the resulting alias list. Idempotent.
func (c *Client) RemoveUserAliases(ctx context.Context, ref string, aliases []string) (response.UserAliasList, error) {
	var out response.UserAliasList
	err := c.Do(ctx, http.MethodDelete, "/api/v1/series/"+url.PathEscape(ref)+"/aliases", nil, response.UserAliasMutation{Aliases: aliases}, &out, false)
	return out, err
}

// ReconcilePlan calls POST /api/v1/series/{ref}/reconcile/plan.
func (c *Client) ReconcilePlan(ctx context.Context, ref string) (response.ReconcilePlan, error) {
	var out response.ReconcilePlan
	err := c.Do(ctx, http.MethodPost, "/api/v1/series/"+url.PathEscape(ref)+"/reconcile/plan", nil, nil, &out, false)
	return out, err
}

// ReconcileRecover calls POST /api/v1/series/{ref}/reconcile/recover.
// Operator-only.
func (c *Client) ReconcileRecover(ctx context.Context, ref string, force bool) (response.RecoverReconcile, error) {
	body := map[string]any{}
	if force {
		body["force"] = true
	}
	var out response.RecoverReconcile
	err := c.Do(ctx, http.MethodPost, "/api/v1/series/"+url.PathEscape(ref)+"/reconcile/recover", nil, body, &out, false)
	return out, err
}

// ScanRequest is the POST /api/v1/series/{ref}/scan body.
type ScanRequest struct {
	Refresh      bool   `json:"refresh,omitempty"`
	MetadataOnly bool   `json:"metadataOnly,omitempty"`
	Ordering     string `json:"ordering,omitempty"`
}

// SubmitScan returns a JobAck the caller can poll or stream.
func (c *Client) SubmitScan(ctx context.Context, ref string, req ScanRequest) (JobAck, error) {
	var out JobAck
	err := c.Do(ctx, http.MethodPost, "/api/v1/series/"+url.PathEscape(ref)+"/scan", nil, req, &out, false)
	return out, err
}

// SubmitApply returns a JobAck for reconcile apply.
func (c *Client) SubmitApply(ctx context.Context, ref, token string) (JobAck, error) {
	var out JobAck
	err := c.Do(ctx, http.MethodPost, "/api/v1/series/"+url.PathEscape(ref)+"/reconcile/apply", nil, map[string]string{"token": token}, &out, false)
	return out, err
}

// SubmitStage returns a JobAck for stage.
func (c *Client) SubmitStage(ctx context.Context, ref string, body any) (JobAck, error) {
	var out JobAck
	err := c.Do(ctx, http.MethodPost, "/api/v1/series/"+url.PathEscape(ref)+"/stage", nil, body, &out, false)
	return out, err
}

// TrashListSeries calls GET /api/v1/series/{ref}/trash.
func (c *Client) TrashListSeries(ctx context.Context, ref, olderThan string) (response.TrashList, error) {
	q := url.Values{}
	if olderThan != "" {
		q.Set("olderThan", olderThan)
	}
	var out response.TrashList
	err := c.Do(ctx, http.MethodGet, "/api/v1/series/"+url.PathEscape(ref)+"/trash", q, nil, &out, false)
	return out, err
}

// TrashListAll calls GET /api/v1/trash.
func (c *Client) TrashListAll(ctx context.Context, olderThan string) (response.TrashList, error) {
	q := url.Values{}
	if olderThan != "" {
		q.Set("olderThan", olderThan)
	}
	var out response.TrashList
	err := c.Do(ctx, http.MethodGet, "/api/v1/trash", q, nil, &out, false)
	return out, err
}

// TrashRestore calls POST /api/v1/series/{ref}/trash/{ulid}/restore.
// Operator-only.
func (c *Client) TrashRestore(ctx context.Context, ref, id string) (response.TrashRestore, error) {
	var out response.TrashRestore
	err := c.Do(ctx, http.MethodPost, "/api/v1/series/"+url.PathEscape(ref)+"/trash/"+url.PathEscape(id)+"/restore", nil, nil, &out, false)
	return out, err
}

// TrashEmptySeries calls DELETE /api/v1/series/{ref}/trash. Operator + confirm.
func (c *Client) TrashEmptySeries(ctx context.Context, ref, olderThan string) (response.TrashEmpty, error) {
	q := url.Values{}
	if olderThan != "" {
		q.Set("olderThan", olderThan)
	}
	var out response.TrashEmpty
	err := c.Do(ctx, http.MethodDelete, "/api/v1/series/"+url.PathEscape(ref)+"/trash", q, nil, &out, true)
	return out, err
}

// TrashEmptyAll calls DELETE /api/v1/trash. Operator + confirm.
func (c *Client) TrashEmptyAll(ctx context.Context, olderThan string) (response.TrashEmpty, error) {
	q := url.Values{}
	if olderThan != "" {
		q.Set("olderThan", olderThan)
	}
	var out response.TrashEmpty
	err := c.Do(ctx, http.MethodDelete, "/api/v1/trash", q, nil, &out, true)
	return out, err
}

// SubmitReindex calls POST /api/v1/library/reindex and returns the
// JobAck the caller streams via /jobs/{id}/stream.
func (c *Client) SubmitReindex(ctx context.Context) (JobAck, error) {
	var out JobAck
	err := c.Do(ctx, http.MethodPost, "/api/v1/library/reindex", nil, nil, &out, false)
	return out, err
}

// ScanAllRequest is the POST /api/v1/library/scan body.
type ScanAllRequest struct {
	Refresh      bool `json:"refresh,omitempty"`
	MetadataOnly bool `json:"metadataOnly,omitempty"`
	Concurrency  int  `json:"concurrency,omitempty"`
}

// SubmitScanAll calls POST /api/v1/library/scan and returns the
// JobAck the caller streams via /jobs/{id}/stream. The fan-out runs
// server-side; the response result decodes to response.ScanAllResult.
func (c *Client) SubmitScanAll(ctx context.Context, req ScanAllRequest) (JobAck, error) {
	var out JobAck
	err := c.Do(ctx, http.MethodPost, "/api/v1/library/scan", nil, req, &out, false)
	return out, err
}

// InboxListOptions holds the GET /api/v1/inbox query parameters.
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

// InboxList calls GET /api/v1/inbox.
func (c *Client) InboxList(ctx context.Context, opts InboxListOptions) (response.InboxList, error) {
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
	var out response.InboxList
	err := c.Do(ctx, http.MethodGet, "/api/v1/inbox", q, nil, &out, false)
	return out, err
}
