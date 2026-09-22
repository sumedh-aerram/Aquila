package replay

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/fault"
)

func TestReplayThroughInjected502Differs(t *testing.T) {
	t.Parallel()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"status": "ok"})
	}))
	t.Cleanup(up.Close)
	h, err := fault.Handler(up.URL, fault.Spec{Status: http.StatusBadGateway})
	if err != nil {
		t.Fatal(err)
	}
	inj := httptest.NewServer(h)
	t.Cleanup(inj.Close)
	w := Workload{Steps: []Step{{Method: http.MethodGet, Path: "/healthz"}}}
	base, err := Run(t.Context(), up.URL, w)
	if err != nil {
		t.Fatal(err)
	}
	patch, err := Run(t.Context(), inj.URL, w)
	if err != nil {
		t.Fatal(err)
	}
	rep := Compare(base, patch)
	if rep.Verdict != VerdictDiffer {
		t.Fatalf("injected 502 must differ, got %+v", rep)
	}
}

func TestReplayThroughDelayStillMatches(t *testing.T) {
	t.Parallel()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]string{"status": "ok"})
	}))
	t.Cleanup(up.Close)
	h, err := fault.Handler(up.URL, fault.Spec{Delay: 25 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	slow := httptest.NewServer(h)
	t.Cleanup(slow.Close)
	w := Workload{Steps: []Step{{Method: http.MethodGet, Path: "/healthz"}}}
	base, err := Repeat(t.Context(), up.URL, w, 3)
	if err != nil {
		t.Fatal(err)
	}
	patch, err := Repeat(t.Context(), slow.URL, w, 3)
	if err != nil {
		t.Fatal(err)
	}
	rep := Compare(base[0], patch[0])
	if rep.Verdict != VerdictMatch {
		t.Fatalf("delay must not become a behavior differ: %+v", rep)
	}
	lat := Latency(base, patch)
	if len(lat) != 1 || lat[0].Patch.MedNS < 20e6 {
		t.Fatalf("injected delay missing from samples: %+v", lat)
	}
	if lat[0].Patch.HasP95 || lat[0].Baseline.HasP95 {
		t.Fatal("p95 must be withheld for n=3")
	}
}
