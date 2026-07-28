// Package rest wires takuhai's HTTP REST surfaces: pushed crawl ingest, queue
// claiming/stats, and matcher disposition submit.
package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/wyvernzora/kura/services/release-indexer/internal/dispatch"
	"github.com/wyvernzora/kura/services/release-indexer/internal/ingest"
	"github.com/wyvernzora/kura/services/release-indexer/internal/metrics"
	"github.com/wyvernzora/kura/services/release-indexer/internal/store"
	"github.com/wyvernzora/kura/services/release-indexer/pkg/api"
)

type Handler struct {
	dispatch *dispatch.Dispatcher
	ingest   *ingest.Processor
	stats    ingestStats
	mux      *http.ServeMux
	metrics  *metrics.Takuhai
	logger   *slog.Logger
}

type ingestStats interface {
	QueueStats(ctx context.Context) (store.QueueStats, error)
}

func New(s store.Store) *Handler {
	return NewWithMetrics(s, nil)
}

func NewWithMetrics(s store.Store, m *metrics.Takuhai) *Handler {
	return NewWithMetricsAndLogger(s, m, nil)
}

func NewWithMetricsAndLogger(s store.Store, m *metrics.Takuhai, logger *slog.Logger) *Handler {
	h := &Handler{
		dispatch: dispatch.New(s),
		ingest:   ingest.New(s, m),
		stats:    s,
		metrics:  m,
		logger:   logger,
	}
	mux := http.NewServeMux()
	// Literal segments outrank the {infohash} wildcard, so the queue
	// routes are unreachable-by-accident even though they sit at the
	// same depth as the per-release ones.
	mux.HandleFunc("/api/v1/releases", h.handleListReleases)
	mux.HandleFunc("/api/v1/releases/ingest", h.handleIngest)
	mux.HandleFunc("/api/v1/releases/queue/claim", h.handleClaim)
	mux.HandleFunc("/api/v1/releases/queue/stats", h.handleQueueStats)
	mux.HandleFunc("/api/v1/releases/queue/submit", h.handleSubmit)
	mux.HandleFunc("/api/v1/releases/{infohash}/magnet", h.handleGetMagnet)
	mux.HandleFunc("/api/v1/releases/{infohash}", h.handleGetRelease)
	h.mux = mux
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) { h.mux.ServeHTTP(w, r) }

func (h *Handler) log(r *http.Request, level slog.Level, msg string, attrs ...any) {
	if h.logger == nil {
		return
	}
	h.logger.Log(r.Context(), level, msg, attrs...)
}

func (h *Handler) requirePost(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return nil, false
	}
	body, err := readAll(r)
	if err != nil {
		// A body over the cap lands here too; MaxBytesError is the only
		// read failure worth distinguishing, since it is the client's
		// fault in a way a truncated stream is not.
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, api.KindInvalidRequest,
				"request body exceeds limit", map[string]any{"limitBytes": maxRequestBytes})
			return nil, false
		}
		writeInvalidRequest(w, "unreadable request body", nil)
		return nil, false
	}
	return body, true
}

// writeDispatchError maps a dispatch error onto the wire. infohash, when
// known, rides in data rather than as a sibling of message — the envelope has
// exactly three fields and per-error context belongs in data (plan §3.2).
//
// An unmodelled error is reported as internal WITHOUT its message: those wrap
// storage failures whose text can carry connection strings and SQL.
func (h *Handler) writeDispatchError(w http.ResponseWriter, infohash string, err error) {
	var data map[string]any
	if infohash != "" {
		data = map[string]any{"infohash": infohash}
	}
	kind := dispatch.ErrorKind(err)
	switch kind {
	case api.KindNotFound:
		writeError(w, http.StatusNotFound, kind, err.Error(), data)
	case api.KindNoActiveLease, api.KindStaleLease:
		writeError(w, http.StatusConflict, kind, err.Error(), data)
	case api.KindInvalidRef, api.KindInvalidCursor, api.KindInvalidRequest:
		writeError(w, http.StatusBadRequest, kind, err.Error(), data)
	default:
		writeError(w, http.StatusInternalServerError, api.KindInternal, "internal error", data)
	}
}

func dispatchLogLevel(err error) slog.Level {
	if dispatch.ErrorKind(err) == "" {
		return slog.LevelError
	}
	return slog.LevelInfo
}

func writeInvalidRequest(w http.ResponseWriter, msg string, data map[string]any) {
	writeError(w, http.StatusBadRequest, api.KindInvalidRequest, msg, data)
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, api.KindMethodNotAllowed, "method not allowed", nil)
}

func writeJSON(w http.ResponseWriter, status int, raw []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

func writeJSONValue(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, kind, msg string, data map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(api.Error{Kind: kind, Message: msg, Data: data})
}

// maxRequestBytes caps every request body. Ingest is the largest legitimate
// caller and its batch cap bounds it well below this; the limit exists so an
// unbounded body cannot be streamed into memory before that cap applies.
const maxRequestBytes = 8 << 20

func readAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(http.MaxBytesReader(nil, r.Body, maxRequestBytes))
}

// decodeJSON rejects unknown fields and trailing content. A body that is
// valid JSON followed by junk is a client bug worth surfacing, not something
// to silently accept the prefix of.
func decodeJSON(body []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("unexpected trailing content after JSON value")
	}
	return nil
}
