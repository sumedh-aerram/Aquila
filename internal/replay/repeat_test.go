package replay

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRepeatRunsNTimes(t *testing.T) {
	t.Parallel()
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		writeJSON(w, map[string]string{"status": "ok"})
	}))
	t.Cleanup(srv.Close)
	runs, err := Repeat(t.Context(), srv.URL, Workload{Steps: []Step{{Method: http.MethodGet, Path: "/healthz"}}}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 || n != 3 {
		t.Fatalf("runs=%d hits=%d", len(runs), n)
	}
}

func TestRepeatRejectsZero(t *testing.T) {
	t.Parallel()
	_, err := Repeat(t.Context(), "http://127.0.0.1:18180", ShopFixture(), 0)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRepeatRejectsOverMax(t *testing.T) {
	t.Parallel()
	_, err := Repeat(t.Context(), "http://127.0.0.1:18180", ShopFixture(), maxRepeats+1)
	if err == nil {
		t.Fatal("expected error")
	}
}
