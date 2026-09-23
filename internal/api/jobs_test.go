package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestListJobsByService(t *testing.T) {
	t.Parallel()
	store := jobs.NewMemory()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Jobs: store})
	dag, err := plan.FromImpact(impact.Report{Files: []string{"a.go"}, Direct: []impact.Finding{{Name: "F"}}})
	if err != nil {
		t.Fatal(err)
	}
	for i, svc := range []string{"ledger", "shop"} {
		raw, err := json.Marshal(jobs.CreateOpts{
			Baseline: fmt.Sprintf("http://127.0.0.1:%d", 18180+i),
			Patch:    fmt.Sprintf("http://127.0.0.1:%d", 18280+i),
			Service:  svc,
			Workload: replay.Workload{Steps: []replay.Step{{Method: http.MethodGet, Path: "/healthz"}}},
			Plan:     dag,
		})
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/jobs", bytes.NewReader(raw)))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", svc, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/jobs?service=ledger", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body jobListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Jobs) != 1 || body.Jobs[0].Service != "ledger" {
		t.Fatalf("%+v", body.Jobs)
	}
}

func TestListJobsServiceQueryIsParameterized(t *testing.T) {
	t.Parallel()
	store := jobs.NewMemory()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Jobs: store})
	dag, err := plan.FromImpact(impact.Report{Files: []string{"a.go"}, Direct: []impact.Finding{{Name: "F"}}})
	if err != nil {
		t.Fatal(err)
	}
	payload := `' OR 1=1 --`
	raw, err := json.Marshal(jobs.CreateOpts{
		Baseline: "http://127.0.0.1:18180",
		Patch:    "http://127.0.0.1:18280",
		Service:  payload,
		Workload: replay.Workload{Steps: []replay.Step{{Method: http.MethodGet, Path: "/healthz"}}},
		Plan:     dag,
	})
	if err != nil {
		t.Fatal(err)
	}
	create := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/v1/jobs", bytes.NewReader(raw)))
	if create.Code != http.StatusOK {
		t.Fatalf("create status=%d", create.Code)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/jobs?service="+url.QueryEscape(payload), nil)
	srv.http.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body jobListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Jobs) != 1 || body.Jobs[0].Service != payload {
		t.Fatalf("%+v", body.Jobs)
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

func TestCreateJobBusy(t *testing.T) {
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
	first := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/v1/jobs", bytes.NewReader(raw)))
	if first.Code != http.StatusOK {
		t.Fatalf("status=%d", first.Code)
	}
	second := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/v1/jobs", bytes.NewReader(raw)))
	if second.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", second.Code, second.Body.String())
	}
}

func TestCancelJob(t *testing.T) {
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
	create := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/v1/jobs", bytes.NewReader(raw)))
	if create.Code != http.StatusOK {
		t.Fatalf("status=%d", create.Code)
	}
	var job jobs.Job
	if err := json.Unmarshal(create.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/jobs/"+job.ID+"/cancel", bytes.NewReader([]byte("{}"))))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.Status != jobs.StatusCanceled {
		t.Fatalf("%s", job.Status)
	}
}
