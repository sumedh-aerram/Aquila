package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/config"
	"github.com/sumedhaerram/aquila/internal/source"
)

func testSourceGraph(t *testing.T) *source.Graph {
	t.Helper()
	g, err := source.FromSnapshot(source.Snapshot{
		Module: "example.com/demo",
		Nodes: []source.Node{
			{ID: "pkg:example.com/demo/pay", Kind: source.KindPackage, Name: "pay", Pkg: "example.com/demo/pay"},
			{ID: "file:pay.go", Kind: source.KindFile, Name: "pay.go", Pkg: "example.com/demo/pay", File: "pay.go"},
			{ID: "func:example.com/demo/pay.Authorize", Kind: source.KindFunction, Name: "Authorize", Pkg: "example.com/demo/pay", File: "pay.go", Line: 10},
			{ID: "func:example.com/demo/pay.charge", Kind: source.KindFunction, Name: "charge", Pkg: "example.com/demo/pay", File: "pay.go", Line: 40},
		},
		Edges: []source.Edge{
			{From: "pkg:example.com/demo/pay", To: "file:pay.go", Kind: source.EdgeContains, Provenance: source.ProvenanceSyntax},
			{From: "file:pay.go", To: "func:example.com/demo/pay.Authorize", Kind: source.EdgeContains, Provenance: source.ProvenanceSyntax},
			{From: "func:example.com/demo/pay.Authorize", To: "func:example.com/demo/pay.charge", Kind: source.EdgeCalls, Provenance: source.ProvenanceTypes},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestSourceUnavailableWithoutGraph(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/source", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestSourceReturnsSnapshot(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Source: testSourceGraph(t)})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/source", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var snap source.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Module != "example.com/demo" || len(snap.Nodes) != 4 || len(snap.Edges) != 3 {
		t.Fatalf("%+v", snap)
	}
}

func TestSourceNeighborsTraversesCalls(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Source: testSourceGraph(t)})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/source/neighbors?id=func:example.com/demo/pay.Authorize", nil)
	srv.http.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		ID        string            `json:"id"`
		Neighbors []source.Neighbor `json:"neighbors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range body.Neighbors {
		if n.Direction == source.DirOut && n.Kind == source.EdgeCalls && n.Node.Name == "charge" {
			found = true
		}
	}
	if !found {
		t.Fatalf("neighbors=%+v", body.Neighbors)
	}
}

func TestSourceNeighborsMissingID(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Source: testSourceGraph(t)})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/source/neighbors", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestSourceNeighborsUnknownNode(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Source: testSourceGraph(t)})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/source/neighbors?id=func:missing", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestSourceNeighborsRejectsOversizedID(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Source: testSourceGraph(t)})
	rec := httptest.NewRecorder()
	id := strings.Repeat("a", maxSourceNodeID+1)
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/source/neighbors?id="+id, nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestSourceNeighborsDoesNotTreatIDAsPath(t *testing.T) {
	t.Parallel()
	srv := NewServer(config.Config{Server: config.ServerConfig{Addr: ":0", ShutdownTimeout: time.Second}}, nil, Dependencies{Ready: stubReady{}, Source: testSourceGraph(t)})
	rec := httptest.NewRecorder()
	srv.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/source/neighbors?id=../../etc/passwd", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rec.Code)
	}
}
