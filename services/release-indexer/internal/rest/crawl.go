package rest

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/internal/ingest"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/crawl"
)

// ChunkCrawler consumes one exact-sized chunk of a source's listing from an
// opaque cursor. The rest layer receives the same crawler instances the
// scheduled loop uses, so each source's rate limiter and page cache govern
// scheduled and backfill traffic together.
type ChunkCrawler interface {
	CrawlChunk(ctx context.Context, pageSize int, cursor string, lookback time.Duration) (crawl.CrawlResponse, error)
}

// RegisterCrawler exposes a source through POST /api/v1/sources/{source}/crawl.
// Wiring-time only; not safe to call once the handler is serving.
func (h *Handler) RegisterCrawler(source string, c ChunkCrawler) {
	h.crawlers[source] = c
}

// handleSourceCrawl serves POST /api/v1/sources/{source}/crawl: consume one
// chunk server-side and ingest it directly. This is the sanctioned backfill
// producer — a remote loop (normally `kura crawl`) threads the returned
// cursor, and the server holds no crawl state between requests.
func (h *Handler) handleSourceCrawl(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method != http.MethodPost {
		h.log(r, slog.LevelDebug, "source crawl rejected", "reason", "method_not_allowed", "method", r.Method)
		writeMethodNotAllowed(w)
		return
	}
	source := r.PathValue("source")
	crawler := h.crawlers[source]
	if crawler == nil {
		h.log(r, slog.LevelInfo, "source crawl rejected", "reason", "unknown_source", "source", source)
		writeError(w, http.StatusNotFound, api.KindNotFound, "unknown or unconfigured source",
			map[string]any{"source": source, "known": api.Sources()})
		return
	}

	body, ok := h.requirePost(w, r)
	if !ok {
		return
	}
	var req api.CrawlRequest
	if err := decodeJSON(body, &req); err != nil {
		h.log(r, slog.LevelInfo, "source crawl rejected", "reason", "invalid_body", "source", source, "err", err)
		writeInvalidRequest(w, "invalid request body", nil)
		return
	}
	if req.PageSize == 0 {
		req.PageSize = 200
	}
	if req.PageSize < 1 || req.PageSize > 200 {
		h.log(r, slog.LevelInfo, "source crawl rejected", "reason", "invalid_page_size", "source", source, "page_size", req.PageSize)
		writeInvalidRequest(w, "pageSize must be within 1..200", map[string]any{"pageSize": req.PageSize})
		return
	}
	// Validate the client-side params BEFORE the walk: a malformed lookback
	// or cursor is a bad request (400), never an upstream failure (502).
	lookback, err := crawl.ParseLookback(source, req.Lookback)
	if err != nil {
		h.log(r, slog.LevelInfo, "source crawl rejected", "reason", "invalid_lookback", "source", source, "lookback", req.Lookback, "err", err)
		writeInvalidRequest(w, err.Error(), map[string]any{"lookback": req.Lookback})
		return
	}
	if _, _, err := crawl.ParseCursor(source, req.Cursor); err != nil {
		h.log(r, slog.LevelInfo, "source crawl rejected", "reason", "invalid_cursor", "source", source, "cursor_len", len(req.Cursor), "err", err)
		writeInvalidRequest(w, err.Error(), nil)
		return
	}

	ctx := r.Context()
	chunk, err := crawler.CrawlChunk(ctx, req.PageSize, req.Cursor, lookback)
	if err != nil {
		// A fetch/parse failure is a transient upstream problem: the cursor
		// was not advanced past the failed page, so the caller retries the
		// same request. Never the floor.
		h.log(r, slog.LevelWarn, "source crawl fetch failed",
			"source", source,
			"page_size", req.PageSize,
			"cursor_len", len(req.Cursor),
			"duration_ms", time.Since(start).Milliseconds(),
			"err", err,
		)
		writeError(w, http.StatusBadGateway, api.KindUpstreamError, "page fetch failed",
			map[string]any{"source": source})
		return
	}

	batch, err := h.ingest.Batch(ctx, chunk.Posts)
	if err != nil {
		var failure *ingest.Failure
		if !errors.As(err, &failure) {
			failure = &ingest.Failure{Index: -1, Err: err}
		}
		h.metrics.IngestBatch(len(chunk.Posts), "error")
		h.log(r, slog.LevelError, "source crawl ingest failed",
			"source", source,
			"post_count", len(chunk.Posts),
			"post_index", failure.Index,
			"source_id", failure.SourceID,
			"duration_ms", time.Since(start).Milliseconds(),
			"err", err,
		)
		writeError(w, http.StatusInternalServerError, api.KindInternal, "ingest failed", nil)
		return
	}

	qs, err := h.stats.QueueStats(ctx)
	if err != nil {
		h.metrics.IngestBatch(len(chunk.Posts), "error")
		h.log(r, slog.LevelError, "source crawl queue stats failed",
			"source", source,
			"duration_ms", time.Since(start).Milliseconds(),
			"err", err,
		)
		writeError(w, http.StatusInternalServerError, api.KindInternal, "queue stats failed", nil)
		return
	}
	h.metrics.IngestBatch(len(chunk.Posts), "ok")

	resp := api.CrawlResult{
		Source:       source,
		Posts:        len(chunk.Posts),
		PagesFetched: chunk.PagesFetched,
		Batch:        batch,
		Queue: api.QueueCounts{
			Available: qs.Available,
			Leased:    qs.Leased,
			Exhausted: qs.Exhausted,
		},
		NextCursor: chunk.NextCursor,
		HasMore:    chunk.HasMore,
		StopReason: chunk.StopReason,
	}
	resp.OldestPublishedAt, resp.NewestPublishedAt = publishedBounds(chunk.Posts)

	h.log(r, slog.LevelInfo, "source crawl completed",
		"source", source,
		"page_size", req.PageSize,
		"post_count", len(chunk.Posts),
		"pages_fetched", chunk.PagesFetched,
		"last_page", chunk.LastPage,
		"stop_reason", chunk.StopReason,
		"has_more", chunk.HasMore,
		"new_count", batch.New,
		"duplicate_count", batch.Duplicate,
		"skipped_count", batch.Skipped,
		"queue_available", qs.Available,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// publishedBounds returns the oldest and newest non-zero publishedAt stamps.
func publishedBounds(posts []api.RawPost) (oldest, newest *time.Time) {
	for i := range posts {
		t := posts[i].PublishedAt
		if t.IsZero() {
			continue
		}
		if oldest == nil || t.Before(*oldest) {
			stamp := t
			oldest = &stamp
		}
		if newest == nil || t.After(*newest) {
			stamp := t
			newest = &stamp
		}
	}
	return oldest, newest
}
