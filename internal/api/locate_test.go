package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/config"
	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/locate"
	"github.com/sumedhaerram/aquila/internal/source"
)

func TestLocateBindsStoredSpan(t *testing.T) {
	t.Parallel()
	store := ingest.NewMemory()
	start := time.Unix(1, 0).UTC()
	err := store.UpsertSpans(t.Context(), []ingest.Span{
		{
			TraceID:      "aa",
			SpanID:       "01",
			ServiceName:  "payment",
			Name:         "payment.Authorize",
			CodeFunction: "Handler.authorize",
			CodeFile:     "examples/shop/internal/payment/handler.go",
			StartTime:    start,
		},
		{
			TraceID:     "aa",
			SpanID:      "02",
			ServiceName: "payment",
			Name:        "POST /charge",
			StartTime:   start.Add(time.Millisecond),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	g, err := source.FromSnapshot(source.Snapshot{
		Module: "example.com/demo",
		Nodes: []source.Node{
			{ID: "file:internal/payment/handler.go", Kind: source.KindFile, Name: "handler.go", File: "internal/payment/handler.go"},
			{ID: "func:pay.Handler.authorize", Kind: source.KindFunction, Name: "authorize", File: "internal/payment/handler.go", Line: 58},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Spans: store, Source: g})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/locate", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var snap locate.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Bound != 1 || snap.UnmappedCount != 1 || snap.SpanCount != 2 {
		t.Fatalf("%+v", snap)
	}
	if snap.Bindings[0].SourceID != "func:pay.Handler.authorize" {
		t.Fatalf("%+v", snap.Bindings[0])
	}
	if snap.Unmapped[0].Reason != locate.ReasonMissingAttrs {
		t.Fatalf("%+v", snap.Unmapped[0])
	}
}

func TestLocateUnavailableWithoutSource(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Spans: ingest.NewMemory()})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/locate", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestLocateUnavailableWithoutStore(t *testing.T) {
	t.Parallel()
	g, err := source.FromSnapshot(source.Snapshot{
		Module: "example.com/demo",
		Nodes:  []source.Node{{ID: "pkg:demo", Kind: source.KindPackage, Name: "demo"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Source: g})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/locate", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestLocateRejectsInvalidTracesParam(t *testing.T) {
	t.Parallel()
	g, err := source.FromSnapshot(source.Snapshot{
		Module: "example.com/demo",
		Nodes:  []source.Node{{ID: "pkg:demo", Kind: source.KindPackage, Name: "demo"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Spans: ingest.NewMemory(), Source: g})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/locate?traces=nope", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}
