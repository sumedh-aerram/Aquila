package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/config"
	"github.com/sumedhaerram/aquila/internal/ingest"
)

func TestAPIRequiresTokenWhenConfigured(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{
		Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second, Token: "control-token"},
	}, nil, Dependencies{Ready: stubReady{}, Spans: ingest.NewMemory()})

	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/graph", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}

	ok := httptest.NewRequest(http.MethodGet, "/v1/graph", nil)
	ok.Header.Set("Authorization", "Bearer control-token")
	rec = httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, ok)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer status=%d body=%s", rec.Code, rec.Body.String())
	}

	header := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	header.Header.Set(apiTokenHeader, "control-token")
	rec = httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, header)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status=%d", rec.Code)
	}

	health := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("healthz status=%d", health.Code)
	}
}
