package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/sumedhaerram/aquila/internal/config"
	"github.com/sumedhaerram/aquila/internal/version"
)

// Server is the Phase 0 control-plane HTTP surface.
type Server struct {
	cfg   config.Config
	log   *slog.Logger
	ready ReadyChecker
	http  *http.Server
}

// NewServer constructs the HTTP API.
func NewServer(cfg config.Config, log *slog.Logger, ready ReadyChecker) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{cfg: cfg, log: log, ready: ready}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("GET /version", s.handleVersion)

	s.http = &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"name":    "aquila",
		"version": version.Version,
	})
}

// ListenAndServe serves until the context is cancelled, then shuts down cleanly.
func (s *Server) ListenAndServe(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("listening", "addr", s.cfg.Server.Addr, "version", version.Version)
		errCh <- s.http.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.Server.ShutdownTimeout)
		defer cancel()
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			return err
		}
		err := <-errCh
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
