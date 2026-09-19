package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// ReadyChecker reports dependency readiness. PostgreSQL implements this in Phase 0.
type ReadyChecker interface {
	Ready(ctx context.Context) error
}

type healthResponse struct {
	Status string `json:"status"`
}

type readyResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if s.ready == nil {
		writeJSON(w, http.StatusServiceUnavailable, readyResponse{
			Status: "unavailable",
			Error:  "readiness checker is not configured",
		})
		return
	}
	if err := s.ready.Ready(ctx); err != nil {
		s.log.Warn("readiness check failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, readyResponse{
			Status: "unavailable",
			Error:  "postgres unreachable",
		})
		return
	}
	writeJSON(w, http.StatusOK, readyResponse{Status: "ready"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
