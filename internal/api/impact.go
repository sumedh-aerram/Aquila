package api

import (
	"bytes"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/sumedhaerram/aquila/internal/diff"
	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/locate"
)

func (s *Server) handleImpact(w http.ResponseWriter, r *http.Request) {
	if s.src == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "source unavailable"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, int64(diff.MaxBytes)+1)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid diff"})
		return
	}
	if len(raw) > diff.MaxBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "diff too large"})
		return
	}
	if len(raw) > 0 && !utf8.Valid(raw) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid diff"})
		return
	}
	parsed, err := diff.Parse(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid diff"})
		return
	}
	if len(bytes.TrimSpace(raw)) > 0 && len(parsed.Files) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no file headers in diff"})
		return
	}

	maxTraces, service, err := parseWindowQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	var loc locate.Snapshot
	var rt graph.Snapshot
	if s.spans != nil {
		spans, listErr := s.spans.ListTraceWindow(r.Context(), maxTraces, service)
		if listErr != nil {
			s.log.Error("list trace window", "err", listErr)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "impact failed"})
			return
		}
		loc = locate.Bind(s.src, spans)
		rt = graph.Build(spans)
	}
	writeJSON(w, http.StatusOK, impact.Analyze(s.src, parsed, loc, rt))
}
