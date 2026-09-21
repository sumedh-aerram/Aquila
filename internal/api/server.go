package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/sumedhaerram/aquila/internal/config"
	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/source"
	"github.com/sumedhaerram/aquila/internal/version"
)

// Dependencies are optional control-plane backends. A nil field disables that surface.
type Dependencies struct {
	Ready  ReadyChecker
	Spans  ingest.Store
	Source *source.Graph
}

// Server is the control-plane HTTP surface.
type Server struct {
	cfg   config.Config
	log   *slog.Logger
	ready ReadyChecker
	spans ingest.Store
	src   *source.Graph
	http  *http.Server
}

// NewServer constructs the HTTP API.
func NewServer(cfg config.Config, log *slog.Logger, deps Dependencies) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{cfg: cfg, log: log, ready: deps.Ready, spans: deps.Spans, src: deps.Source}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("GET /version", s.handleVersion)
	mux.HandleFunc("POST /v1/traces", s.handleOTLPTraces)
	mux.HandleFunc("GET /v1/spans", s.handleListSpans)
	mux.HandleFunc("GET /v1/graph", s.handleGraph)
	mux.HandleFunc("GET /v1/source", s.handleSource)
	mux.HandleFunc("GET /v1/source/neighbors", s.handleSourceNeighbors)
	mux.HandleFunc("GET /v1/locate", s.handleLocate)

	readTimeout := cfg.Server.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = config.DefaultReadTimeout
	}
	writeTimeout := cfg.Server.WriteTimeout
	if writeTimeout <= 0 {
		writeTimeout = config.DefaultWriteTimeout
	}
	idleTimeout := cfg.Server.IdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = config.DefaultIdleTimeout
	}
	s.http = &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    8 << 10,
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
