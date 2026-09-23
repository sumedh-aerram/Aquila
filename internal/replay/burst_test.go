package replay

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBurstParallel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	got, err := Burst(t.Context(), srv.URL, Workload{Steps: []Step{{Method: http.MethodGet, Path: "/healthz"}}}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("n=%d", len(got))
	}
	for _, r := range got {
		if len(r.Steps) != 1 || r.Steps[0].Status != http.StatusOK {
			t.Fatalf("%+v", r)
		}
	}
}

func TestBurstRejectsEmpty(t *testing.T) {
	t.Parallel()
	_, err := Burst(t.Context(), "http://127.0.0.1:18180", Workload{}, 2)
	if err == nil {
		t.Fatal("expected error")
	}
}
