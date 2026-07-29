package rest

import (
	"net/http"
)

// buildRouter wires the URL surface and applies the middleware chain.
// New handlers register here; their implementations live in
// handler_*.go.
//
// Two muxes compose into the final handler:
//
//   - apiMux owns every /api/v1/... route.
//   - rootMux owns /healthz and dispatches /api/* to apiMux.
//
// There is no authentication here. The deployment fronts this service with
// Pomerium and confines it with a NetworkPolicy; nothing downstream of that
// boundary re-checks a credential.
//
// Cross-cutting middleware (logging, version, CORS, recover) wraps
// rootMux so it observes every request — including static UI hits —
// uniformly.
//
// Middleware order matters. Outermost first:
//
//	logging   - timestamps every request including 4xx/5xx
//	version   - sets X-Kura-Version on every response
//	cors      - origin allow-list + preflight
//	recover   - turns panics into 500 internal errors
//	(rootMux: /api/* → apiMux; no other routes — the web UI lives in
//	services/gateway and fronts this API through its proxy)
//
// recover sits closest to the inner muxes so panics in middleware itself
// still propagate; they're rare enough not to deserve their own net.
func (s *Server) buildRouter() http.Handler {
	apiMux := http.NewServeMux()

	// library
	apiMux.HandleFunc("GET /api/v1/library", s.handleLibrary)

	// series
	apiMux.HandleFunc("GET /api/v1/series", s.handleList)
	apiMux.HandleFunc("GET /api/v1/series/{ref}", s.handleShow)
	apiMux.HandleFunc("POST /api/v1/series", s.handleAdd)
	apiMux.HandleFunc("POST /api/v1/series/import", s.handleImport)
	apiMux.HandleFunc("PATCH /api/v1/series/{ref}/tags", s.handleTagsUpdate)

	// resolve
	apiMux.HandleFunc("POST /api/v1/series/resolve", s.handleResolve)

	// reset
	apiMux.HandleFunc("POST /api/v1/series/{ref}/reset", s.handleReset)

	// reconcile sync
	apiMux.HandleFunc("POST /api/v1/series/{ref}/reconcile/plan", s.handleReconcilePlan)
	apiMux.HandleFunc("POST /api/v1/series/{ref}/reconcile/recover", s.handleReconcileRecover)

	// async mutations (job-shaped)
	apiMux.HandleFunc("POST /api/v1/series/{ref}/scan", s.handleScan)
	apiMux.HandleFunc("POST /api/v1/series/{ref}/stage", s.handleStage)
	apiMux.HandleFunc("POST /api/v1/series/{ref}/reconcile/apply", s.handleApply)

	// trash mutations
	apiMux.HandleFunc("POST /api/v1/series/{ref}/trash/{ulid}/restore", s.handleTrashRestore)
	apiMux.HandleFunc("DELETE /api/v1/series/{ref}/trash", s.handleTrashEmptySeries)
	apiMux.HandleFunc("DELETE /api/v1/trash", s.handleTrashEmptyAll)

	// library — long-running but non-destructive, ungated.
	apiMux.HandleFunc("POST /api/v1/library/reindex", s.handleReindex)
	apiMux.HandleFunc("POST /api/v1/library/scan", s.handleScanAll)

	// trash
	apiMux.HandleFunc("GET /api/v1/series/{ref}/trash", s.handleTrashListSeries)
	apiMux.HandleFunc("GET /api/v1/trash", s.handleTrashListAll)

	// inbox
	apiMux.HandleFunc("GET /api/v1/inbox", s.handleInboxList)

	// jobs
	apiMux.HandleFunc("GET /api/v1/jobs/{job}", s.handleJobStatus)
	apiMux.HandleFunc("GET /api/v1/jobs/{job}/stream", s.handleJobStream)

	rootMux := http.NewServeMux()
	rootMux.HandleFunc("GET /healthz", s.handleHealth)
	rootMux.Handle("/api/", apiMux)

	return s.applyMiddleware(rootMux)
}

// applyMiddleware wraps rootMux with cross-cutting middleware that observes
// every request.
func (s *Server) applyMiddleware(next http.Handler) http.Handler {
	h := next
	h = recoverMiddleware(s.deps.Logger)(h)
	h = corsMiddleware(s.deps.AllowedOrigins)(h)
	h = versionMiddleware(s.deps.Version)(h)
	// Metrics sit inside logging so both observe the final status,
	// including panics the recover middleware turned into 500s.
	h = s.deps.Metrics.WrapHTTP(h)
	h = loggingMiddleware(s.deps.Logger)(h)
	return h
}
