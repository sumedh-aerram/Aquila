package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/config"
	"github.com/sumedhaerram/aquila/internal/ingest"
)

func TestListAttaches(t *testing.T) {
	t.Parallel()
	store := ingest.NewMemory()
	now := time.Unix(200, 0).UTC()
	if err := store.UpsertSpans(t.Context(), []ingest.Span{
		{TraceID: "aa", SpanID: "11", ServiceName: "ledger", StartTime: now},
		{TraceID: "bb", SpanID: "22", ServiceName: "shop", StartTime: now.Add(-time.Hour)},
		{TraceID: "cc", SpanID: "33", ServiceName: "ledger", StartTime: now.Add(-time.Minute)},
		{TraceID: "dd", SpanID: "44", ServiceName: "  ", StartTime: now},
	}); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Spans: store})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/attaches", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Attaches []ingest.Attach `json:"attaches"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Attaches) != 2 {
		t.Fatalf("%+v", out.Attaches)
	}
	if out.Attaches[0].Service != "ledger" || out.Attaches[0].Spans != 2 {
		t.Fatalf("%+v", out.Attaches[0])
	}
	if out.Attaches[1].Service != "shop" || out.Attaches[1].Spans != 1 {
		t.Fatalf("%+v", out.Attaches[1])
	}
}

func TestListAttachesInvalidLimit(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Spans: ingest.NewMemory()})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/attaches?limit=nope", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestListAttachesUnavailable(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/attaches", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestMetricsHasNoSecrets(t *testing.T) {
	t.Parallel()
	store := ingest.NewMemory()
	srv := NewServer(config.Config{
		Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second},
		Ingest: config.IngestConfig{Token: "super-secret-ingest-token"},
	}, nil, Dependencies{Spans: store})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "aquila_up 1") || !strings.Contains(body, "aquila_ingest_spans_total") {
		t.Fatalf("%s", body)
	}
	if strings.Contains(body, "super-secret-ingest-token") || strings.Contains(body, "X-Aquila-Ingest-Token") {
		t.Fatalf("metrics leaked a secret:\n%s", body)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Fatalf("content-type=%q", ct)
	}
}
