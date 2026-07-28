package rest

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wyvernzora/kura/services/release-indexer/internal/ingest"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

// maxBatchPosts is the hard cap on one POST /ingest body (an oversized batch -> 400
// rather than an unbounded transaction stream). n8n keeps batches modest; this is the
// boundary backstop.
const maxBatchPosts = 1000

// ingestRequest is the POST /ingest request body: a batch of raw crawled posts.
type ingestRequest struct {
	Posts []api.RawPost `json:"posts"`
}

func (h *Handler) handleIngest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method != http.MethodPost {
		h.log(r, slog.LevelDebug, "ingest rejected", "reason", "method_not_allowed", "method", r.Method)
		writeMethodNotAllowed(w)
		return
	}

	body, ok := h.requirePost(w, r)
	if !ok {
		h.metrics.IngestBatch(0, "error")
		return
	}
	var req ingestRequest
	if err := decodeJSON(body, &req); err != nil {
		h.metrics.IngestBatch(0, "error")
		h.log(r, slog.LevelInfo, "ingest rejected", "reason", "invalid_body", "err", err)
		writeInvalidRequest(w, "invalid request body", nil)
		return
	}
	if len(req.Posts) > maxBatchPosts {
		h.metrics.IngestBatch(len(req.Posts), "error")
		h.log(r, slog.LevelInfo, "ingest rejected",
			"reason", "batch_too_large",
			"post_count", len(req.Posts),
			"source_counts", sourceCounts(req.Posts),
			"max_post_count", maxBatchPosts,
		)
		writeError(w, http.StatusBadRequest, api.KindBatchTooLarge, "batch too large",
			map[string]any{"postCount": len(req.Posts), "maxPostCount": maxBatchPosts})
		return
	}

	ctx := r.Context()
	for i := range req.Posts {
		post := req.Posts[i]
		if strings.TrimSpace(post.Source) == "" || strings.TrimSpace(post.SourceID) == "" {
			h.metrics.IngestBatch(len(req.Posts), "error")
			h.log(r, slog.LevelInfo, "ingest rejected",
				"reason", "missing_source",
				"post_count", len(req.Posts),
				"post_index", i,
			)
			writeInvalidRequest(w, "post missing source or sourceId",
				map[string]any{"index": i})
			return
		}
	}

	batch, err := h.ingest.Batch(ctx, req.Posts)
	if err != nil {
		var failure *ingest.Failure
		if !errors.As(err, &failure) {
			failure = &ingest.Failure{Index: -1, Err: err}
		}
		h.metrics.IngestBatch(len(req.Posts), "error")
		h.log(r, slog.LevelError, "ingest failed",
			"post_count", len(req.Posts),
			"source_counts", sourceCounts(req.Posts),
			"post_index", failure.Index,
			"source", failure.Source,
			"source_id", failure.SourceID,
			"status", http.StatusInternalServerError,
			"duration_ms", time.Since(start).Milliseconds(),
			"err", err,
		)
		writeError(w, http.StatusInternalServerError, api.KindInternal, "ingest failed", nil)
		return
	}

	qs, err := h.stats.QueueStats(ctx)
	if err != nil {
		h.metrics.IngestBatch(len(req.Posts), "error")
		h.log(r, slog.LevelError, "ingest queue stats failed",
			"post_count", len(req.Posts),
			"duration_ms", time.Since(start).Milliseconds(),
			"err", err,
		)
		writeError(w, http.StatusInternalServerError, api.KindInternal, "queue stats failed", nil)
		return
	}
	h.metrics.IngestBatch(len(req.Posts), "ok")
	h.log(r, slog.LevelInfo, "ingest completed",
		"post_count", len(req.Posts),
		"source_counts", sourceCounts(req.Posts),
		"new_count", batch.New,
		"updated_count", batch.Updated,
		"duplicate_count", batch.Duplicate,
		"conflict_count", batch.Conflict,
		"skipped_count", batch.Skipped,
		"queue_available", qs.Available,
		"queue_leased", qs.Leased,
		"queue_exhausted", qs.Exhausted,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	resp := api.IngestSummary{
		Batch: batch,
		Queue: api.QueueCounts{
			Available: qs.Available,
			Leased:    qs.Leased,
			Exhausted: qs.Exhausted,
		},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func sourceCounts(posts []api.RawPost) map[string]int {
	counts := make(map[string]int)
	for i := range posts {
		source := strings.TrimSpace(posts[i].Source)
		if source == "" {
			source = "unknown"
		}
		counts[source]++
	}
	return counts
}
