// Package rest exposes the tape-backup service verbs over HTTP.
package rest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/wyvernzora/kura/services/tape-backup/internal/planner"
	"github.com/wyvernzora/kura/services/tape-backup/internal/server/auth"
	"github.com/wyvernzora/kura/services/tape-backup/internal/service"
)

const (
	defaultServerVersion = "dev"
	httpShutdownGrace    = 5 * time.Second
	readHeaderTimeout    = 10 * time.Second
)

// TapeService is the six-verb service facade consumed by the REST transport.
type TapeService interface {
	Status() (service.StatusResult, error)
	Consult([]planner.Blank) (service.ConsultResult, error)
	Plan(service.PlanRequest) (service.PlanResult, error)
	Run(context.Context, service.PlanRequest) error
	Approve(string) error
	Discard(string) error
}

// Deps are the process-owned REST dependencies.
type Deps struct {
	Service     TapeService
	Logger      *slog.Logger
	BearerToken string
	Version     string
}

// Server owns the prebuilt handler and process identity surfaced by health.
type Server struct {
	deps      Deps
	handler   http.Handler
	startedAt time.Time
}

// NewServer constructs the authenticated six-verb REST surface.
func NewServer(deps Deps) (*Server, error) {
	if deps.Service == nil {
		return nil, errors.New("tape rest: service is required")
	}
	if deps.Version == "" {
		deps.Version = defaultServerVersion
	}
	server := &Server{
		deps:      deps,
		startedAt: time.Now(),
	}
	server.handler = server.buildRouter()
	return server, nil
}

// Handler exposes the HTTP handler for tests and embedding.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Serve binds the REST surface and drains in-flight requests when ctx is
// cancelled.
func Serve(ctx context.Context, addr string, server *Server) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("tape rest listen %s: %w", addr, err)
	}
	httpServer := newHTTPServer(ctx, server)
	errCh := make(chan error, 1)
	go func() {
		err := httpServer.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), httpShutdownGrace)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			_ = listener.Close()
			return errors.Join(
				fmt.Errorf("tape rest shutdown: %w", err),
				<-errCh,
			)
		}
		return <-errCh
	case err := <-errCh:
		return err
	}
}

func newHTTPServer(ctx context.Context, server *Server) *http.Server {
	return &http.Server{
		Handler:           server.handler,
		ReadHeaderTimeout: readHeaderTimeout,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
}

func (s *Server) buildRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+auth.HealthPath, s.handleHealth)
	mux.HandleFunc("GET /api/tape/status", s.handleStatus)
	mux.HandleFunc("POST /api/tape/consult", s.handleConsult)
	mux.HandleFunc("POST /api/tape/plan", s.handlePlan)
	mux.HandleFunc("POST /api/tape/run", s.handleRun)
	mux.HandleFunc("POST /api/tape/approve/{planID}", s.handleApprove)
	mux.HandleFunc("POST /api/tape/discard/{planID}", s.handleDiscard)

	var handler http.Handler = auth.BearerMiddleware(s.deps.BearerToken)(mux)
	handler = recoverMiddleware(s.deps.Logger)(handler)
	handler = versionMiddleware(s.deps.Version)(handler)
	handler = loggingMiddleware(s.deps.Logger)(handler)
	return handler
}
