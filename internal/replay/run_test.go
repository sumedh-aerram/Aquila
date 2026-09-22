package replay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunRecordsStatusAndDuration(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		time.Sleep(2 * time.Millisecond)
		writeJSON(w, map[string]string{"status": "ok"})
	}))
	t.Cleanup(srv.Close)
	res, err := Run(t.Context(), srv.URL, Workload{Steps: []Step{{Method: http.MethodGet, Path: "/healthz"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1 || res.Steps[0].Status != 200 || res.Steps[0].DurationNS <= 0 {
		t.Fatalf("%+v", res.Steps)
	}
}

func TestRunRecordsTransportErrorNotSuccess(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	res, err := Run(t.Context(), srv.URL, Workload{Steps: []Step{{Method: http.MethodGet, Path: "/healthz"}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Steps[0].Err == "" || res.Steps[0].Status != 0 {
		t.Fatalf("down server must be an error: %+v", res.Steps[0])
	}
}

func TestRunRejectsFileTarget(t *testing.T) {
	t.Parallel()
	_, err := Run(t.Context(), "file:///etc/passwd", ShopFixture())
	if err == nil || !strings.Contains(err.Error(), "http") {
		t.Fatalf("%v", err)
	}
}

func TestRunRejectsEmptyWorkload(t *testing.T) {
	t.Parallel()
	_, err := Run(t.Context(), "http://127.0.0.1:18180", Workload{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunSendsCheckoutFixtureBody(t *testing.T) {
	t.Parallel()
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/checkout" {
			gotBody, _ = io.ReadAll(r.Body)
		}
		writeJSON(w, map[string]string{"status": "ok"})
	}))
	t.Cleanup(srv.Close)
	_, err := Run(t.Context(), srv.URL, ShopFixture())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gotBody), "sku-widget") {
		t.Fatalf("fixture body not sent: %s", gotBody)
	}
}

func writeJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func TestRunRespectsCancel(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		<-started
		cancel()
	}()
	res, err := Run(ctx, srv.URL, Workload{Steps: []Step{{Method: http.MethodGet, Path: "/healthz"}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Steps[0].Err == "" {
		t.Fatal("canceled request must surface an error")
	}
}
