package rest

import (
	"net/http"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/planner"
	"github.com/wyvernzora/kura/services/tape-backup/internal/service"
)

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		OK        bool      `json:"ok"`
		Version   string    `json:"version"`
		UptimeMs  int64     `json:"uptimeMs"`
		StartedAt time.Time `json:"startedAt"`
	}{
		OK:        true,
		Version:   s.deps.Version,
		UptimeMs:  time.Since(s.startedAt).Milliseconds(),
		StartedAt: s.startedAt,
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	result, err := s.deps.Service.Status()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleConsult(w http.ResponseWriter, r *http.Request) {
	var request []planner.Blank
	if err := decodeJSON(r.Body, &request); err != nil {
		writeError(w, err)
		return
	}
	result, err := s.deps.Service.Consult(request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	var request service.PlanRequest
	if err := decodeJSON(r.Body, &request); err != nil {
		writeError(w, err)
		return
	}
	result, err := s.deps.Service.Plan(request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	var request service.PlanRequest
	if err := decodeJSON(r.Body, &request); err != nil {
		writeError(w, err)
		return
	}
	result, err := s.deps.Service.Run(r.Context(), request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Service.Approve(r.PathValue("planID")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDiscard(w http.ResponseWriter, r *http.Request) {
	if err := s.deps.Service.Discard(r.PathValue("planID")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleEject(w http.ResponseWriter, _ *http.Request) {
	if err := s.deps.Service.Eject(); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
