// Package api implements the GFire REST HTTP API (Band 5).
package api

import (
	"context"
	"net/http"

	"github.com/hrodrig/gfire/internal/config"
	"github.com/hrodrig/gfire/internal/engine"
	"github.com/hrodrig/gfire/internal/storage"
)

// Server serves REST routes and holds runtime dependencies.
type Server struct {
	cfg    *config.Config
	store  storage.Storage
	engine *engine.Engine
	mux    *http.ServeMux
}

// NewServer builds an API server. eng may be nil only in tests without cancel.
func NewServer(cfg *config.Config, store storage.Storage, eng *engine.Engine) *Server {
	s := &Server{cfg: cfg, store: store, engine: eng, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler returns the root HTTP handler with middleware.
func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	h = maxBody(s.cfg.Server.MaxBodySize, h)
	if s.cfg.Auth.Enabled {
		h = bearerAuth(s.cfg.Auth.Token, h)
	}
	return h
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	srv := &http.Server{
		Addr:    s.cfg.ListenAddr(),
		Handler: s.Handler(),
	}
	return srv.ListenAndServe()
}

// Shutdown is a placeholder for graceful HTTP shutdown (Band 7 polish).
func (s *Server) Shutdown(ctx context.Context) error {
	return nil
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)
	s.mux.HandleFunc("POST /v1/jobs/enqueue", s.handleEnqueue)
	s.mux.HandleFunc("POST /v1/jobs/schedule", s.handleSchedule)
	s.mux.HandleFunc("GET /v1/jobs/{id}", s.handleGetJob)
	s.mux.HandleFunc("GET /v1/jobs", s.handleListJobs)
	s.mux.HandleFunc("POST /v1/jobs/{id}/requeue", s.handleRequeue)
	s.mux.HandleFunc("POST /v1/jobs/{id}/cancel", s.handleCancel)
	s.mux.HandleFunc("POST /v1/jobs/{id}/continue", s.handleContinue)
	s.mux.HandleFunc("GET /v1/queues", s.handleListQueues)
	s.mux.HandleFunc("GET /v1/queues/{name}", s.handleGetQueue)
	s.mux.HandleFunc("GET /v1/servers", s.handleListServers)
}
