package api

import (
	"net/http"

	"github.com/sumedhaerram/aquila/internal/graph"
)

func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	if s.spans == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ingest unavailable"})
		return
	}
	maxTraces, service, err := parseWindowQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	spans, err := s.spans.ListTraceWindow(r.Context(), maxTraces, service)
	if err != nil {
		s.log.Error("list trace window", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "graph failed"})
		return
	}
	writeJSON(w, http.StatusOK, graph.Build(spans))
}
