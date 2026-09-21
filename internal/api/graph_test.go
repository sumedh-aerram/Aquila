package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/config"
	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/ingest"
)

func TestGraphDerivedFromStoredSpans(t *testing.T) {
	t.Parallel()
	store := ingest.NewMemory()
	start := time.Unix(1, 0).UTC()
	err := store.UpsertSpans(t.Context(), []ingest.Span{
		{TraceID: "aa", SpanID: "01", ServiceName: "gateway", StartTime: start},
		{TraceID: "aa", SpanID: "02", ParentSpanID: "01", ServiceName: "checkout", StartTime: start.Add(time.Millisecond)},
		{TraceID: "aa", SpanID: "03", ParentSpanID: "02", ServiceName: "payment", StartTime: start.Add(2 * time.Millisecond)},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Spans: store})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/graph", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var snap graph.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.TraceCount != 1 || len(snap.Edges) != 2 {
		t.Fatalf("%+v", snap)
	}
	if len(snap.Paths) != 1 || strings.Join(snap.Paths[0].Services, "/") != "gateway/checkout/payment" {
		t.Fatalf("paths=%+v", snap.Paths)
	}
}

func TestGraphUnavailableWithoutStore(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/graph", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestGraphRejectsInvalidTracesParam(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Spans: ingest.NewMemory()})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/graph?traces=nope", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}
