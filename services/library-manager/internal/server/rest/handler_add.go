package rest

import (
	"fmt"
	"net/http"

	"github.com/wyvernzora/kura/services/library-manager/internal/domain/refs"
	"github.com/wyvernzora/kura/services/library-manager/internal/workflow"
)

// addRequest is the POST /api/v1/series body. Mirrors the MCP
// kura_add tool shape: `ref` is the metadata ref (resolved upstream
// via /resolve), `directory` overrides the new on-disk directory
// name, `ordering` pins the spine ordering.
type addRequest struct {
	Ref       string `json:"ref"`
	Directory string `json:"directory,omitempty"`
	Ordering  string `json:"ordering,omitempty"`
}

// importRequest is the POST /api/v1/series/import body. Mirrors
// kura_import: `ref` is the metadata ref, `directory` is the existing
// directory under the library root to adopt.
type importRequest struct {
	Ref       string `json:"ref"`
	Directory string `json:"directory"`
	Force     bool   `json:"force,omitempty"`
	Ordering  string `json:"ordering,omitempty"`
}

func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	var req addRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, err)
		return
	}
	meta, err := refs.ParseMetadata(req.Ref)
	if err != nil {
		writeError(w, &validationError{msg: fmt.Sprintf("ref: %v (expected provider:id)", err)})
		return
	}
	in := workflow.AddInput{Metadata: meta, Ordering: req.Ordering}
	if req.Directory != "" {
		directory, perr := refs.ParseSeries(req.Directory)
		if perr != nil {
			writeError(w, &validationError{msg: fmt.Sprintf("directory: %v", perr)})
			return
		}
		in.Ref = directory
	}
	result, werr := workflow.Add(r.Context(), s.deps.Workflow, in)
	if werr != nil {
		writeError(w, werr)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := decodeJSON(r.Body, &req); err != nil {
		writeError(w, err)
		return
	}
	if req.Directory == "" {
		writeError(w, &validationError{msg: "directory is required"})
		return
	}
	directory, err := refs.ParseSeries(req.Directory)
	if err != nil {
		writeError(w, &validationError{msg: fmt.Sprintf("directory: %v", err)})
		return
	}
	meta, err := refs.ParseMetadata(req.Ref)
	if err != nil {
		writeError(w, &validationError{msg: fmt.Sprintf("ref: %v (expected provider:id)", err)})
		return
	}
	result, werr := workflow.Import(r.Context(), s.deps.Workflow, workflow.ImportInput{
		Metadata: meta,
		Ref:      directory,
		Force:    req.Force,
		Ordering: req.Ordering,
	})
	if werr != nil {
		writeError(w, werr)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}
