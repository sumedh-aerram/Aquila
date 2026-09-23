package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/jobs"
)

const maxJobBytes = 1 << 20

type jobListResponse struct {
	Jobs []jobs.Job `json:"jobs"`
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	if s.jobs == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "jobs unavailable"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, int64(maxJobBytes)+1)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid job"})
		return
	}
	if len(raw) > maxJobBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "job too large"})
		return
	}
	var opts jobs.CreateOpts
	if err := json.Unmarshal(raw, &opts); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid job"})
		return
	}
	job, err := s.jobs.Create(r.Context(), opts)
	if err != nil {
		if errors.Is(err, jobs.ErrBusy) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "gateway occupied"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid job"})
		return
	}
	if job.Validated {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store failed"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	if s.jobs == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "jobs unavailable"})
		return
	}
	limit := jobs.DefaultList
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n := 0
		for _, c := range raw {
			if c < '0' || c > '9' {
				n = 0
				break
			}
			n = n*10 + int(c-'0')
		}
		if n > 0 {
			limit = n
		}
	}
	service := ingest.ClipService(r.URL.Query().Get("service"))
	list, err := s.jobs.List(r.Context(), limit, service)
	if err != nil {
		s.log.Error("list jobs", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store failed"})
		return
	}
	writeJSON(w, http.StatusOK, jobListResponse{Jobs: list})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	if s.jobs == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "jobs unavailable"})
		return
	}
	id := r.PathValue("id")
	job, err := s.jobs.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	if s.jobs == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "jobs unavailable"})
		return
	}
	id := r.PathValue("id")
	job, err := s.jobs.Cancel(r.Context(), id)
	if err != nil {
		if errors.Is(err, jobs.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	writeJSON(w, http.StatusOK, job)
}
