package api

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/sumedhaerram/aquila/internal/evidence"
	"github.com/sumedhaerram/aquila/internal/runs"
)

type runCreated struct {
	ID             string `json:"id"`
	Overall        string `json:"overall"`
	Validated      bool   `json:"validated"`
	ArtifactDigest string `json:"artifact_digest"`
	BaselineSHA    string `json:"baseline_sha,omitempty"`
	Dirty          bool   `json:"dirty,omitempty"`
}

type runListResponse struct {
	Runs []runCreated `json:"runs"`
}

type runGetResponse struct {
	ID             string            `json:"id"`
	Overall        string            `json:"overall"`
	Validated      bool              `json:"validated"`
	ArtifactDigest string            `json:"artifact_digest"`
	BaselineSHA    string            `json:"baseline_sha,omitempty"`
	Dirty          bool              `json:"dirty,omitempty"`
	Artifact       evidence.Artifact `json:"artifact"`
}

func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	if s.runStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runs unavailable"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, int64(evidence.MaxBytes)+1)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid artifact"})
		return
	}
	if len(raw) > evidence.MaxBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "artifact too large"})
		return
	}
	a, err := evidence.Decode(bytes.NewReader(raw))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid artifact"})
		return
	}
	rec, err := s.runStore.Insert(r.Context(), runs.Record{Artifact: a})
	if err != nil {
		if strings.Contains(err.Error(), "id collision") {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "id collision"})
			return
		}
		s.log.Error("insert run", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store failed"})
		return
	}
	if rec.Validated {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store failed"})
		return
	}
	writeJSON(w, http.StatusOK, summaryFrom(rec))
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	if s.runStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runs unavailable"})
		return
	}
	limit := runs.DefaultList
	if q := r.URL.Query().Get("limit"); q != "" {
		n, convErr := strconv.Atoi(q)
		if convErr != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		limit = n
	}
	list, err := s.runStore.List(r.Context(), limit)
	if err != nil {
		s.log.Error("list runs", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store failed"})
		return
	}
	out := make([]runCreated, 0, len(list))
	for _, rec := range list {
		if rec.Validated {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store failed"})
			return
		}
		out = append(out, summaryFrom(rec))
	}
	writeJSON(w, http.StatusOK, runListResponse{Runs: out})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	if s.runStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "runs unavailable"})
		return
	}
	id, ok := runs.NormalizeID(r.PathValue("id"))
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	rec, err := s.runStore.Get(r.Context(), id)
	if errors.Is(err, runs.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		s.log.Error("get run", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store failed"})
		return
	}
	if rec.Validated {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store failed"})
		return
	}
	writeJSON(w, http.StatusOK, runGetResponse{
		ID:             rec.ID,
		Overall:        rec.Overall,
		Validated:      false,
		ArtifactDigest: rec.ArtifactDigest,
		BaselineSHA:    rec.BaselineSHA,
		Dirty:          rec.Dirty,
		Artifact:       rec.Artifact,
	})
}

func summaryFrom(rec runs.Record) runCreated {
	return runCreated{
		ID:             rec.ID,
		Overall:        rec.Overall,
		Validated:      false,
		ArtifactDigest: rec.ArtifactDigest,
		BaselineSHA:    rec.BaselineSHA,
		Dirty:          rec.Dirty,
	}
}
