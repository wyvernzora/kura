package mcp

import (
	"context"
	"net/url"
	"strconv"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wyvernzora/kura/services/gateway/internal/client"
)

// Handler-side caps. Deliberately not JSON-Schema maxima — see addTool.
const (
	listSeriesCap = 100
	listInboxCap  = 500
)

type (
	resolveIn struct {
		Terms []string `json:"terms" jsonschema:"resolver terms: free text, or an exact provider ref like tvdb:370070"`
	}
	listSeriesIn struct {
		Status []string `json:"status,omitempty" jsonschema:"filter to these library statuses"`
		Airing *bool    `json:"airing,omitempty" jsonschema:"filter on the observed-airing flag"`
		Tags   []string `json:"tags,omitempty" jsonschema:"conjunctive tag filter"`
		Limit  int      `json:"limit,omitempty" jsonschema:"maximum rows to return; clamped to 100"`
		Cursor string   `json:"cursor,omitempty" jsonschema:"opaque nextCursor from the previous response"`
	}
	getSeriesIn struct {
		Ref        string   `json:"ref" jsonschema:"metadata ref of the series, e.g. tvdb:370070"`
		Episodes   string   `json:"episodes,omitempty" jsonschema:"episode selector, e.g. S01 or S01E03-E05"`
		Status     []string `json:"status,omitempty" jsonschema:"filter episodes by state"`
		Source     []string `json:"source,omitempty" jsonschema:"filter episodes by media source"`
		Resolution []string `json:"resolution,omitempty" jsonschema:"filter episodes by resolution"`
	}
	updateTagsIn struct {
		Ref  string   `json:"ref" jsonschema:"metadata ref of the series"`
		Tags []string `json:"tags" jsonschema:"tag changes: a plain tag adds, a !tag removes"`
	}
	addSeriesIn struct {
		Ref       string `json:"ref" jsonschema:"metadata ref to add, e.g. tvdb:370070"`
		Directory string `json:"directory,omitempty" jsonschema:"override for the on-disk directory name"`
		Ordering  string `json:"ordering,omitempty" jsonschema:"provider episode ordering to track"`
	}
	importSeriesIn struct {
		Ref       string `json:"ref" jsonschema:"metadata ref to bind to the directory"`
		Directory string `json:"directory" jsonschema:"existing directory basename under the library root"`
		Ordering  string `json:"ordering,omitempty" jsonschema:"provider episode ordering to track"`
	}
	scanSeriesIn struct {
		Ref          string `json:"ref" jsonschema:"metadata ref of the series to scan"`
		Refresh      bool   `json:"refresh,omitempty" jsonschema:"re-probe media facts even when size and mtime are unchanged"`
		MetadataOnly bool   `json:"metadataOnly,omitempty" jsonschema:"skip the filesystem walk and refresh provider data only"`
		Ordering     string `json:"ordering,omitempty" jsonschema:"provider episode ordering to track"`
	}
	stageIn struct {
		Ref      string           `json:"ref" jsonschema:"metadata ref of the target series"`
		Episodes []stageEpisodeIn `json:"episodes,omitempty" jsonschema:"episode staging entries"`
		Trash    []stageTrashIn   `json:"trash,omitempty" jsonschema:"paths to queue for trashing on the next reconcile"`
		Extras   []stageExtraIn   `json:"extras,omitempty" jsonschema:"files or directories to queue as season extras"`
	}
	stageEpisodeIn struct {
		Episode    string            `json:"episode"`
		Media      string            `json:"media"`
		Source     string            `json:"source,omitempty"`
		Companions []string          `json:"companions,omitempty"`
		Replace    bool              `json:"replace,omitempty"`
		Attrs      map[string]string `json:"attrs,omitempty"`
	}
	stageTrashIn struct {
		Path       string   `json:"path"`
		Companions []string `json:"companions,omitempty"`
	}
	stageExtraIn struct {
		Season int    `json:"season"`
		Source string `json:"source"`
		Prefix string `json:"prefix,omitempty"`
	}
	resetIn struct {
		Ref      string   `json:"ref" jsonschema:"metadata ref of the series"`
		Episode  string   `json:"episode,omitempty" jsonschema:"single episode marker to clear, e.g. S01E03"`
		TrashIDs []string `json:"trashIds,omitempty" jsonschema:"staged trash ids to drop"`
		ExtraIDs []string `json:"extraIds,omitempty" jsonschema:"staged extra ids to drop"`
		All      bool     `json:"all,omitempty" jsonschema:"clear every staged record on the series"`
	}
	refIn struct {
		Ref string `json:"ref" jsonschema:"metadata ref of the series"`
	}
	applyIn struct {
		Ref   string `json:"ref" jsonschema:"metadata ref of the series"`
		Token string `json:"token" jsonschema:"plan token returned by plan_series_reconcile"`
	}
	getJobIn struct {
		JobID string `json:"jobId" jsonschema:"job id returned by an async tool"`
	}
	listInboxIn struct {
		Path          string `json:"path,omitempty" jsonschema:"inbox-relative directory or exact file"`
		Recursive     bool   `json:"recursive,omitempty" jsonschema:"walk subdirectories"`
		Depth         int    `json:"depth,omitempty" jsonschema:"maximum walk depth"`
		Limit         int    `json:"limit,omitempty" jsonschema:"maximum entries to return; clamped to 500"`
		Kind          string `json:"kind,omitempty" jsonschema:"filter to file or dir"`
		NameGlob      string `json:"nameGlob,omitempty" jsonschema:"glob applied to entry names"`
		IncludeHidden bool   `json:"includeHidden,omitempty" jsonschema:"include dotfiles and in-flight download markers"`
	}
)

// jobRef is what every async tool returns: the id and nothing else. The REST
// statusUrl and streamUrl are affordances an MCP client cannot follow.
type jobRef struct {
	JobID string `json:"jobId"`
}

func registerLibraryTools(srv *mcpsdk.Server, s *Server) {
	registerSeriesReadTools(srv, s)
	registerSeriesWriteTools(srv, s)
	registerReconcileTools(srv, s)
}

func registerSeriesReadTools(srv *mcpsdk.Server, s *Server) {
	registerResolveAndList(srv, s)
	registerGetSeries(srv, s)
	registerInboxAndJob(srv, s)
}

func registerResolveAndList(srv *mcpsdk.Server, s *Server) {
	// resolve_series reaches the metadata provider, hence open-world.
	addTool(srv, &mcpsdk.Tool{Name: "resolve_series", Description: docResolveSeries, Annotations: readOnly()},
		func(ctx context.Context, _ *mcpsdk.CallToolRequest, in resolveIn) (*mcpsdk.CallToolResult, map[string]any, error) {
			return passthrough(ctx, s, "resolve_series", func() (map[string]any, error) {
				var out map[string]any
				err := s.library.Post(ctx, "/series/resolve", map[string]any{"terms": in.Terms}, &out)
				return out, err
			})
		})

	addTool(srv, &mcpsdk.Tool{Name: "list_series", Description: docListSeries, Annotations: readOnlyClosed()},
		func(ctx context.Context, _ *mcpsdk.CallToolRequest, in listSeriesIn) (*mcpsdk.CallToolResult, map[string]any, error) {
			q := url.Values{}
			for _, v := range in.Status {
				q.Add("status", v)
			}
			for _, v := range in.Tags {
				q.Add("tags", v)
			}
			if in.Airing != nil {
				q.Set("airing", strconv.FormatBool(*in.Airing))
			}
			if in.Cursor != "" {
				q.Set("cursor", in.Cursor)
			}
			limit := in.Limit
			if limit <= 0 || limit > listSeriesCap {
				limit = listSeriesCap
			}
			q.Set("limit", strconv.Itoa(limit))
			return passthrough(ctx, s, "list_series", func() (map[string]any, error) {
				var out map[string]any
				err := s.library.Get(ctx, "/series", q, &out)
				return out, err
			})
		})

}

func registerGetSeries(srv *mcpsdk.Server, s *Server) {
	addTool(srv, &mcpsdk.Tool{Name: "get_series", Description: docGetSeries, Annotations: readOnlyClosed()},
		func(ctx context.Context, _ *mcpsdk.CallToolRequest, in getSeriesIn) (*mcpsdk.CallToolResult, map[string]any, error) {
			q := url.Values{}
			if in.Episodes != "" {
				q.Set("episodes", in.Episodes)
			}
			for _, v := range in.Status {
				q.Add("status", v)
			}
			for _, v := range in.Source {
				q.Add("source", v)
			}
			for _, v := range in.Resolution {
				q.Add("resolution", v)
			}
			start := time.Now()
			var raw map[string]any
			err := s.library.Get(ctx, "/series/"+url.PathEscape(in.Ref), q, &raw)
			if err == nil {
				raw, err = projectSeries(raw)
			}
			s.observe(ctx, "get_series", start, err, "ref", in.Ref)
			if err != nil {
				return errorResult(err), nil, nil
			}
			return nil, raw, nil
		})

}

func registerInboxAndJob(srv *mcpsdk.Server, s *Server) {
	addTool(srv, &mcpsdk.Tool{Name: "list_inbox", Description: docListInbox, Annotations: readOnlyClosed()},
		func(ctx context.Context, _ *mcpsdk.CallToolRequest, in listInboxIn) (*mcpsdk.CallToolResult, map[string]any, error) {
			q := url.Values{}
			if in.Path != "" {
				q.Set("path", in.Path)
			}
			if in.Recursive {
				q.Set("recursive", "1")
			}
			if in.Depth > 0 {
				q.Set("depth", strconv.Itoa(in.Depth))
			}
			if in.Kind != "" {
				q.Set("kind", in.Kind)
			}
			if in.NameGlob != "" {
				q.Set("name_glob", in.NameGlob)
			}
			if in.IncludeHidden {
				q.Set("include_hidden", "1")
			}
			limit := in.Limit
			if limit <= 0 || limit > listInboxCap {
				limit = listInboxCap
			}
			q.Set("limit", strconv.Itoa(limit))
			return passthrough(ctx, s, "list_inbox", func() (map[string]any, error) {
				var out map[string]any
				err := s.library.Get(ctx, "/inbox", q, &out)
				return out, err
			})
		})

	addTool(srv, &mcpsdk.Tool{Name: "get_job", Description: docGetJob, Annotations: readOnlyClosed()},
		func(ctx context.Context, _ *mcpsdk.CallToolRequest, in getJobIn) (*mcpsdk.CallToolResult, map[string]any, error) {
			start := time.Now()
			var status client.JobStatus
			err := s.library.Get(ctx, "/jobs/"+url.PathEscape(in.JobID), nil, &status)
			var out map[string]any
			if err == nil {
				out, err = projectJob(status)
			}
			s.observe(ctx, "get_job", start, err, "job_id", in.JobID, "state", status.State)
			if err != nil {
				return errorResult(err), nil, nil
			}
			return nil, out, nil
		})
}

func registerSeriesWriteTools(srv *mcpsdk.Server, s *Server) {
	addTool(srv, &mcpsdk.Tool{Name: "update_series_tags", Description: docUpdateTags, Annotations: mutating(true, false, false)},
		func(ctx context.Context, _ *mcpsdk.CallToolRequest, in updateTagsIn) (*mcpsdk.CallToolResult, map[string]any, error) {
			return passthrough(ctx, s, "update_series_tags", func() (map[string]any, error) {
				var out map[string]any
				err := s.library.Patch(ctx, "/series/"+url.PathEscape(in.Ref)+"/tags", map[string]any{"tags": in.Tags}, &out)
				return out, err
			})
		})

	// add and import reach the provider for metadata, hence open-world, and
	// are not idempotent: both create tracked state.
	addTool(srv, &mcpsdk.Tool{Name: "add_series", Description: docAddSeries, Annotations: mutating(false, false, true)},
		func(ctx context.Context, _ *mcpsdk.CallToolRequest, in addSeriesIn) (*mcpsdk.CallToolResult, map[string]any, error) {
			body := map[string]any{"ref": in.Ref}
			addOptional(body, "directory", in.Directory)
			addOptional(body, "ordering", in.Ordering)
			return passthrough(ctx, s, "add_series", func() (map[string]any, error) {
				var out map[string]any
				err := s.library.Post(ctx, "/series", body, &out)
				return out, err
			})
		})

	addTool(srv, &mcpsdk.Tool{Name: "import_series", Description: docImportSeries, Annotations: mutating(false, false, true)},
		func(ctx context.Context, _ *mcpsdk.CallToolRequest, in importSeriesIn) (*mcpsdk.CallToolResult, map[string]any, error) {
			// force is deliberately absent: it overwrites a tracked
			// series' metadata and stays REST-only.
			body := map[string]any{"ref": in.Ref, "directory": in.Directory}
			addOptional(body, "ordering", in.Ordering)
			return passthrough(ctx, s, "import_series", func() (map[string]any, error) {
				var out map[string]any
				err := s.library.Post(ctx, "/series/import", body, &out)
				return out, err
			})
		})

	addTool(srv, &mcpsdk.Tool{Name: "scan_series", Description: docScanSeries, Annotations: mutating(false, false, true)},
		func(ctx context.Context, _ *mcpsdk.CallToolRequest, in scanSeriesIn) (*mcpsdk.CallToolResult, jobRef, error) {
			body := map[string]any{}
			if in.Refresh {
				body["refresh"] = true
			}
			if in.MetadataOnly {
				body["metadataOnly"] = true
			}
			addOptional(body, "ordering", in.Ordering)
			return submitJob(ctx, s, "scan_series", "/series/"+url.PathEscape(in.Ref)+"/scan", body, in.Ref)
		})

	addTool(srv, &mcpsdk.Tool{Name: "stage_series_media", Description: docStageMedia, Annotations: mutating(false, false, false)},
		func(ctx context.Context, _ *mcpsdk.CallToolRequest, in stageIn) (*mcpsdk.CallToolResult, jobRef, error) {
			body := map[string]any{}
			if len(in.Episodes) > 0 {
				body["episodes"] = in.Episodes
			}
			if len(in.Trash) > 0 {
				body["trash"] = in.Trash
			}
			if len(in.Extras) > 0 {
				body["extras"] = in.Extras
			}
			return submitJob(ctx, s, "stage_series_media", "/series/"+url.PathEscape(in.Ref)+"/stage", body, in.Ref)
		})

	// reset drops staged records: destructive to intent, though it never
	// touches media on disk.
	addTool(srv, &mcpsdk.Tool{Name: "reset_series_staging", Description: docResetStaging, Annotations: mutating(true, true, false)},
		func(ctx context.Context, _ *mcpsdk.CallToolRequest, in resetIn) (*mcpsdk.CallToolResult, resetSummary, error) {
			start := time.Now()
			body := map[string]any{}
			addOptional(body, "episode", in.Episode)
			if in.All {
				body["all"] = true
			}
			if len(in.TrashIDs) > 0 {
				body["trashIds"] = in.TrashIDs
			}
			if len(in.ExtraIDs) > 0 {
				body["extraIds"] = in.ExtraIDs
			}
			var raw client.ResetResult
			err := s.library.Post(ctx, "/series/"+url.PathEscape(in.Ref)+"/reset", body, &raw)
			s.observe(ctx, "reset_series_staging", start, err, "ref", in.Ref)
			if err != nil {
				return errorResult(err), resetSummary{}, nil
			}
			return nil, projectReset(raw), nil
		})
}

func registerReconcileTools(srv *mcpsdk.Server, s *Server) {
	addTool(srv, &mcpsdk.Tool{Name: "plan_series_reconcile", Description: docPlanReconcile, Annotations: mutating(true, false, false)},
		func(ctx context.Context, _ *mcpsdk.CallToolRequest, in refIn) (*mcpsdk.CallToolResult, map[string]any, error) {
			start := time.Now()
			var raw map[string]any
			err := s.library.Post(ctx, "/series/"+url.PathEscape(in.Ref)+"/reconcile/plan", nil, &raw)
			if err == nil {
				raw, err = projectPlan(raw)
			}
			s.observe(ctx, "plan_series_reconcile", start, err, "ref", in.Ref)
			if err != nil {
				return errorResult(err), nil, nil
			}
			return nil, raw, nil
		})

	// apply moves and trashes files: destructive, but idempotent on its
	// plan token — re-applying a spent token is rejected, not repeated.
	addTool(srv, &mcpsdk.Tool{Name: "apply_series_reconcile", Description: docApplyReconcile, Annotations: mutating(true, true, false)},
		func(ctx context.Context, _ *mcpsdk.CallToolRequest, in applyIn) (*mcpsdk.CallToolResult, jobRef, error) {
			return submitJob(ctx, s, "apply_series_reconcile",
				"/series/"+url.PathEscape(in.Ref)+"/reconcile/apply", map[string]any{"token": in.Token}, in.Ref)
		})
}

// passthrough forwards a leaf response to structuredContent unchanged.
func passthrough(ctx context.Context, s *Server, tool string, call func() (map[string]any, error)) (*mcpsdk.CallToolResult, map[string]any, error) {
	start := time.Now()
	out, err := call()
	s.observe(ctx, tool, start, err)
	if err != nil {
		return errorResult(err), nil, nil
	}
	return nil, out, nil
}

// submitJob posts an async request and returns only the job id.
func submitJob(ctx context.Context, s *Server, tool, path string, body any, ref string) (*mcpsdk.CallToolResult, jobRef, error) {
	start := time.Now()
	var ack client.JobAck
	err := s.library.Post(ctx, path, body, &ack)
	s.observe(ctx, tool, start, err, "ref", ref, "job_id", ack.JobID)
	if err != nil {
		return errorResult(err), jobRef{}, nil
	}
	return nil, jobRef{JobID: ack.JobID}, nil
}

func addOptional(body map[string]any, key, value string) {
	if value != "" {
		body[key] = value
	}
}
