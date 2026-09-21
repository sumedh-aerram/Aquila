package api

import (
	"net/http"
	"strconv"

	"github.com/sumedhaerram/aquila/internal/graph"
)

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	if s.spans == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ingest unavailable"})
		return
	}
	maxTraces := 0
	if raw := r.URL.Query().Get("traces"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid traces"})
			return
		}
		maxTraces = n
	}
	spans, err := s.spans.ListTraceWindow(r.Context(), maxTraces)
	if err != nil {
		s.log.Error("list trace window", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "graph failed"})
		return
	}
	writeJSON(w, http.StatusOK, graph.Build(spans))
}
