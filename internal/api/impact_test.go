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
	"github.com/sumedhaerram/aquila/internal/diff"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/source"
)

func impactGraph(t *testing.T) *source.Graph {
	t.Helper()
	g, err := source.FromSnapshot(source.Snapshot{
		Module: "example.com/demo",
		Nodes: []source.Node{
			{ID: "file:internal/payment/handler.go", Kind: source.KindFile, Name: "handler.go", File: "internal/payment/handler.go"},
			{ID: "func:pay.Handler.chargeProcessor", Kind: source.KindFunction, Name: "chargeProcessor", File: "internal/payment/handler.go", Line: 40},
			{ID: "func:pay.Handler.authorize", Kind: source.KindFunction, Name: "authorize", File: "internal/payment/handler.go", Line: 10},
		},
		Edges: []source.Edge{
			{From: "func:pay.Handler.authorize", To: "func:pay.Handler.chargeProcessor", Kind: source.EdgeCalls, Provenance: source.ProvenanceTypes},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

const d1Patch = `--- a/internal/payment/handler.go
+++ b/internal/payment/handler.go
@@ -40,3 +40,3 @@
 context
-	old
+	new
`

func TestImpactMapsD1StylePatch(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Source: impactGraph(t), Spans: ingest.NewMemory()})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/impact", strings.NewReader(d1Patch))
	srv.http.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var rep impact.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Direct) == 0 || rep.Direct[0].Name != "chargeProcessor" {
		t.Fatalf("%+v", rep)
	}
	if len(rep.Likely) == 0 || rep.Likely[0].Name != "authorize" {
		t.Fatalf("%+v", rep)
	}
}

func TestImpactUnavailableWithoutSource(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/impact", strings.NewReader(d1Patch)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestImpactRejectsTraversalPath(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Source: impactGraph(t)})
	body := `--- a/../../etc/passwd
+++ b/../../etc/passwd
@@ -1 +1 @@
-a
+b
`
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/impact", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestImpactRejectsOversizedDiff(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Source: impactGraph(t)})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/impact", bytes.NewReader(bytes.Repeat([]byte("a"), diff.MaxBytes+2)))
	srv.http.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestImpactDoesNotEchoPatchBody(t *testing.T) {
	t.Parallel()
	secret := "charge-secret-xyz"
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Source: impactGraph(t)})
	body := `--- a/internal/payment/handler.go
+++ b/internal/payment/handler.go
@@ -40,3 +40,3 @@
 context
-	` + secret + `
+	new
`
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/impact", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatal("response leaked hunk text")
	}
}

func TestImpactRejectsGET(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Source: impactGraph(t)})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/impact", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
