package rest

import "net/http"

// buildRouter wires the URL surface and applies the middleware chain.
// New handlers register here; their implementations live in
// handler_*.go.
//
// Middleware order matters. Outermost first:
//
//	logging   - timestamps every request including 4xx/5xx
//	version   - sets X-Kura-Version on every response
//	cors      - origin allow-list + preflight
//	recover   - turns panics into 500 internal errors
//	(handler)
//
// recover sits closest to the handler so panics in middleware itself
// still propagate; they're rare enough not to deserve their own net.
func (s *Server) buildRouter() http.Handler {
	mux := http.NewServeMux()

	// health + library
	mux.HandleFunc("GET /api/v1/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/library", s.handleLibrary)

	// series
	mux.HandleFunc("GET /api/v1/series", s.handleList)
	mux.HandleFunc("GET /api/v1/series/{ref}", s.handleShow)
	mux.HandleFunc("POST /api/v1/series", s.handleAdd)
	mux.HandleFunc("POST /api/v1/series/import", s.handleImport)
	mux.HandleFunc("DELETE /api/v1/series/{ref}", s.handleRemoveDispatch)

	// resolve
	mux.HandleFunc("POST /api/v1/resolve", s.handleResolve)

	// reset
	mux.HandleFunc("POST /api/v1/series/{ref}/reset", s.handleReset)

	// reconcile sync
	mux.HandleFunc("POST /api/v1/series/{ref}/reconcile/plan", s.handleReconcilePlan)
	mux.HandleFunc("POST /api/v1/series/{ref}/reconcile/recover", requireOperator(s.handleReconcileRecover))

	// async mutations (job-shaped)
	mux.HandleFunc("POST /api/v1/series/{ref}/scan", s.handleScan)
	mux.HandleFunc("POST /api/v1/series/{ref}/stage", s.handleStage)
	mux.HandleFunc("POST /api/v1/series/{ref}/reconcile/apply", s.handleApply)

	// trash mutations (operator-only)
	mux.HandleFunc("POST /api/v1/series/{ref}/trash/{ulid}/restore", requireOperator(s.handleTrashRestore))
	mux.HandleFunc("DELETE /api/v1/series/{ref}/trash", requireOperator(s.handleTrashEmptySeries))
	mux.HandleFunc("DELETE /api/v1/trash", requireOperator(s.handleTrashEmptyAll))

	// library
	mux.HandleFunc("POST /api/v1/library/reindex", requireOperator(s.handleReindex))

	// trash
	mux.HandleFunc("GET /api/v1/series/{ref}/trash", s.handleTrashListSeries)
	mux.HandleFunc("GET /api/v1/trash", s.handleTrashListAll)

	// inbox
	mux.HandleFunc("GET /api/v1/inbox", s.handleInboxList)

	// jobs
	mux.HandleFunc("GET /api/v1/jobs/{job}", s.handleJobStatus)
	mux.HandleFunc("GET /api/v1/jobs/{job}/stream", s.handleJobStream)

	return s.applyMiddleware(mux)
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

func (s *Server) applyMiddleware(next http.Handler) http.Handler {
	h := next
	h = bearerAuthMiddleware(s.deps.BearerToken)(h)
	h = recoverMiddleware(s.deps.Logger)(h)
	h = corsMiddleware(s.deps.AllowedOrigins)(h)
	h = versionMiddleware()(h)
	h = loggingMiddleware(s.deps.Logger)(h)
	return h
}
