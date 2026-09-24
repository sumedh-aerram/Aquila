package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/config"
	"github.com/sumedhaerram/aquila/internal/evidence"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
	"github.com/sumedhaerram/aquila/internal/runs"
)

func TestCreateRunPersistsAndLists(t *testing.T) {
	t.Parallel()
	store := runs.NewMemory()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Runs: store})
	art := runArtifact(t, replay.VerdictDiffer)
	raw, err := evidence.Marshal(art)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/runs", bytes.NewReader(raw))
	srv.http.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created runCreated
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Validated || created.Overall != replay.VerdictDiffer {
		t.Fatalf("%+v", created)
	}

	listRec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/v1/runs", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var listed runListResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Runs) != 1 || listed.Runs[0].ID != created.ID || listed.Runs[0].Validated {
		t.Fatalf("%+v", listed)
	}

	getRec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/v1/runs/"+created.ID, nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	if strings.Contains(getRec.Body.String(), `"validated": true`) || strings.Contains(getRec.Body.String(), "sku-widget") {
		t.Fatalf("%s", getRec.Body.String())
	}
	var got runGetResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Artifact.Result.Overall != replay.VerdictDiffer || got.Validated {
		t.Fatalf("%+v", got)
	}
}

func TestCreateRunRejectsPass(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Runs: runs.NewMemory()})
	raw := []byte(`{"schema":"aquila.evidence.v1","validated":false,"result":{"overall":"pass"}}`)
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/runs", bytes.NewReader(raw)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateRunExplainsRejection(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Runs: runs.NewMemory()})
	raw := []byte(`{"schema":"aquila.evidence.v1","validated":false,"result":{"overall":"match"},"module":"example.com/x"}`)
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/runs", bytes.NewReader(raw)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `unknown field \"module\"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateRunUnavailableWithoutStore(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{})
	art := runArtifact(t, replay.VerdictMatch)
	raw, err := evidence.Marshal(art)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/runs", bytes.NewReader(raw)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestGetRunRejectsTraversalID(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Runs: runs.NewMemory()})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/runs/../spans", nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCreateRunIdempotent(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Runs: runs.NewMemory()})
	raw, err := evidence.Marshal(runArtifact(t, replay.VerdictMatch))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/runs", bytes.NewReader(raw)))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	listRec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/v1/runs", nil))
	var listed runListResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Runs) != 1 {
		t.Fatalf("%+v", listed)
	}
}

func TestListRunsByService(t *testing.T) {
	t.Parallel()
	store := runs.NewMemory()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Runs: store})
	ledger := evidence.Build(evidence.Input{
		Now:         time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC),
		Baseline:    "http://127.0.0.1:18180",
		Patch:       "http://127.0.0.1:18280",
		BaselineSHA: "deadbeef",
		Service:     "ledger",
		Workload:    replay.Workload{Steps: []replay.Step{{Method: "GET", Path: "/healthz"}}},
		Impact:      impact.Report{Files: []string{"a.go"}},
		Plan:        plan.DAG{},
		Result:      plan.Evidence{Overall: replay.VerdictDiffer, Notes: []string{"not validated"}},
	})
	shop := evidence.Build(evidence.Input{
		Now:         time.Date(2026, 9, 22, 21, 0, 0, 0, time.UTC),
		Baseline:    "http://127.0.0.1:18180",
		Patch:       "http://127.0.0.1:18280",
		BaselineSHA: "deadbeef",
		Service:     "shop",
		Workload:    replay.Workload{Steps: []replay.Step{{Method: "GET", Path: "/healthz"}}},
		Impact:      impact.Report{Files: []string{"a.go"}},
		Plan:        plan.DAG{},
		Result:      plan.Evidence{Overall: replay.VerdictMatch, Notes: []string{"not validated"}},
	})
	for _, a := range []evidence.Artifact{ledger, shop} {
		raw, err := evidence.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/runs", bytes.NewReader(raw)))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/runs?service=ledger", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body runListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Runs) != 1 || body.Runs[0].Service != "ledger" || body.Runs[0].Overall != replay.VerdictDiffer {
		t.Fatalf("%+v", body.Runs)
	}
}

func runArtifact(t *testing.T, overall string) evidence.Artifact {
	t.Helper()
	return evidence.Build(evidence.Input{
		Now:         time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC),
		Baseline:    "http://127.0.0.1:18180",
		Patch:       "http://127.0.0.1:18280",
		BaselineSHA: "deadbeef",
		Workload:    replay.Workload{Steps: []replay.Step{{Method: "GET", Path: "/healthz"}}},
		Impact:      impact.Report{Files: []string{"a.go"}},
		Plan:        plan.DAG{},
		Result:      plan.Evidence{Overall: overall, Notes: []string{"not validated"}},
	})
}
