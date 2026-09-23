package replay

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthzBothOK(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	if err := Healthz(t.Context(), srv.URL, srv.URL); err != nil {
		t.Fatal(err)
	}
}

func TestHealthzRejectsDown(t *testing.T) {
	t.Parallel()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(up.Close)
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(down.Close)
	if err := Healthz(t.Context(), up.URL, down.URL); err == nil {
		t.Fatal("expected error")
	}
}
