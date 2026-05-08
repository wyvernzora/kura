package rest

import (
	"net/http"

	"github.com/wyvernzora/kura/internal/server/webui"
)

// buildRouter wires the URL surface and applies the middleware chain.
// New handlers register here; their implementations live in
// handler_*.go.
//
// Two muxes compose into the final handler:
//
//   - apiMux owns every /api/v1/... route and is wrapped by the
//     bearer-auth middleware. The web UI bundle is not gated, so the
//     login flow itself can load before the user has a token.
//   - rootMux dispatches requests: /api/* goes to the bearer-wrapped
//     apiMux, everything else falls through to the embedded SPA.
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
//	(rootMux: /api/* → bearer → apiMux; / → webui)
//
// recover sits closest to the inner muxes so panics in middleware itself
// still propagate; they're rare enough not to deserve their own net.
func (s *Server) buildRouter() http.Handler {
	apiMux := http.NewServeMux()

	// health + library
	apiMux.HandleFunc("GET /api/v1/health", s.handleHealth)
	apiMux.HandleFunc("GET /api/v1/library", s.handleLibrary)

	// series
	apiMux.HandleFunc("GET /api/v1/series", s.handleList)
	apiMux.HandleFunc("GET /api/v1/series/{ref}", s.handleShow)
	apiMux.HandleFunc("POST /api/v1/series", s.handleAdd)
	apiMux.HandleFunc("POST /api/v1/series/import", s.handleImport)
	apiMux.HandleFunc("DELETE /api/v1/series/{ref}", s.handleRemoveDispatch)

	// resolve
	apiMux.HandleFunc("POST /api/v1/resolve", s.handleResolve)

	// reset
	apiMux.HandleFunc("POST /api/v1/series/{ref}/reset", s.handleReset)

	// user aliases (search-key shorthands)
	apiMux.HandleFunc("GET /api/v1/series/{ref}/aliases", s.handleAliasesList)
	apiMux.HandleFunc("POST /api/v1/series/{ref}/aliases", s.handleAliasesAdd)
	apiMux.HandleFunc("DELETE /api/v1/series/{ref}/aliases", s.handleAliasesRemove)

	// reconcile sync
	apiMux.HandleFunc("POST /api/v1/series/{ref}/reconcile/plan", s.handleReconcilePlan)
	apiMux.HandleFunc("POST /api/v1/series/{ref}/reconcile/recover", requireOperator(s.handleReconcileRecover))

	// async mutations (job-shaped)
	apiMux.HandleFunc("POST /api/v1/series/{ref}/scan", s.handleScan)
	apiMux.HandleFunc("POST /api/v1/series/{ref}/stage", s.handleStage)
	apiMux.HandleFunc("POST /api/v1/series/{ref}/reconcile/apply", s.handleApply)

	// trash mutations (operator-only)
	apiMux.HandleFunc("POST /api/v1/series/{ref}/trash/{ulid}/restore", requireOperator(s.handleTrashRestore))
	apiMux.HandleFunc("DELETE /api/v1/series/{ref}/trash", requireOperator(s.handleTrashEmptySeries))
	apiMux.HandleFunc("DELETE /api/v1/trash", requireOperator(s.handleTrashEmptyAll))

	// library
	apiMux.HandleFunc("POST /api/v1/library/reindex", requireOperator(s.handleReindex))

	// trash
	apiMux.HandleFunc("GET /api/v1/series/{ref}/trash", s.handleTrashListSeries)
	apiMux.HandleFunc("GET /api/v1/trash", s.handleTrashListAll)

	// inbox
	apiMux.HandleFunc("GET /api/v1/inbox", s.handleInboxList)

	// jobs
	apiMux.HandleFunc("GET /api/v1/jobs/{job}", s.handleJobStatus)
	apiMux.HandleFunc("GET /api/v1/jobs/{job}/stream", s.handleJobStream)

	apiHandler := bearerAuthMiddleware(s.deps.BearerToken)(apiMux)

	rootMux := http.NewServeMux()
	rootMux.Handle("/api/", apiHandler)
	rootMux.Handle("/", webui.Handler())

	return s.applyMiddleware(rootMux)
}

// handleRemoveDispatch routes DELETE /series/{ref}: ?purge=1 invokes
// the operator-gated handler, default mode is open. Inline-gating
// keeps a single mux entry per HTTP method.
func (s *Server) handleRemoveDispatch(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("purge") == "1" {
		requireOperator(s.handleRemove)(w, r)
		return
	}
	s.handleRemove(w, r)
}

// applyMiddleware wraps rootMux with cross-cutting middleware that
// observes every request, web UI included. The bearer-auth gate is
// not in this chain; it lives inside the rootMux dispatch so it only
// applies to /api/* paths.
func (s *Server) applyMiddleware(next http.Handler) http.Handler {
	h := next
	h = recoverMiddleware(s.deps.Logger)(h)
	h = corsMiddleware(s.deps.AllowedOrigins)(h)
	h = versionMiddleware()(h)
	h = loggingMiddleware(s.deps.Logger)(h)
	return h
}
