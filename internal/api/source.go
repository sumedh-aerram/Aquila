package api

import (
	"net/http"
	"unicode/utf8"
)

const maxSourceNodeID = 1024

func (s *Server) handleSource(w http.ResponseWriter, _ *http.Request) {
	if s.src == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "source unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, s.src.Snapshot())
}

func (s *Server) handleSourceNeighbors(w http.ResponseWriter, r *http.Request) {
	if s.src == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "source unavailable"})
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
		return
	}
	if len(id) > maxSourceNodeID || !utf8.ValidString(id) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	neighbors, ok := s.src.Neighbors(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":        id,
		"neighbors": neighbors,
	})
}
