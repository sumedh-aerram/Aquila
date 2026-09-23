package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/config"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/jobs"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

func TestCreateJobSkipsOperator(t *testing.T) {
	t.Parallel()
	store := jobs.NewMemory()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Jobs: store})
	dag, err := plan.FromImpact(impact.Report{Files: []string{"a.go"}, Direct: []impact.Finding{{Name: "F"}}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(jobs.CreateOpts{
		Baseline: "http://127.0.0.1:18180",
		Patch:    "http://127.0.0.1:18280",
		Workload: replay.Workload{Steps: []replay.Step{{Method: http.MethodGet, Path: "/healthz"}}},
		Plan:     dag,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/jobs", bytes.NewReader(raw)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var job jobs.Job
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.Validated || job.ID == "" {
		t.Fatalf("%+v", job)
	}
	if job.Status != jobs.StatusRunning {
		t.Fatalf("%s", job.Status)
	}
}

func TestCreateJobUnavailableWithoutStore(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/jobs", bytes.NewReader([]byte(`{}`))))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
}
