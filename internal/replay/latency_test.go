package replay

import (
	"testing"
)

func TestSummarizeKnownRanks(t *testing.T) {
	t.Parallel()
	samples := make([]int64, 100)
	for i := 0; i < 100; i++ {
		samples[i] = int64(i+1) * 1e6
	}
	s := Summarize(samples)
	if s.N != 100 || !s.HasP95 || !s.HasP99 {
		t.Fatalf("%+v", s)
	}
	if s.MinNS != 1e6 {
		t.Fatalf("min=%d", s.MinNS)
	}
	if s.MedNS != 50e6 {
		t.Fatalf("median=%d want 50ms", s.MedNS)
	}
	if s.P95NS != 95e6 {
		t.Fatalf("p95=%d want 95ms", s.P95NS)
	}
	if s.P99NS != 99e6 {
		t.Fatalf("p99=%d want 99ms", s.P99NS)
	}
}

func TestSummarizeWithholdsPercentilesWhenNSmall(t *testing.T) {
	t.Parallel()
	s := Summarize([]int64{1e6, 2e6, 3e6, 4e6, 5e6})
	if s.N != 5 || s.HasP95 || s.HasP99 || s.P95NS != 0 {
		t.Fatalf("p95 must be withheld for n=5: %+v", s)
	}
	if s.MedNS == 0 {
		t.Fatal("median should still be reported")
	}
}

func TestSummarizeEmpty(t *testing.T) {
	t.Parallel()
	s := Summarize(nil)
	if s.N != 0 || s.HasP95 || s.MedNS != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestLatencyExcludesFailedSteps(t *testing.T) {
	t.Parallel()
	runs := []Result{
		{Steps: []Observation{{Method: "GET", Path: "/healthz", Status: 200, DurationNS: 10e6}}},
		{Steps: []Observation{{Method: "GET", Path: "/healthz", Err: "timeout", DurationNS: 1}}},
		{Steps: []Observation{{Method: "GET", Path: "/healthz", Status: 200, DurationNS: 12e6}}},
	}
	got := successfulDurations(runs, 0)
	if len(got) != 2 || got[0] != 10e6 || got[1] != 12e6 {
		t.Fatalf("%v", got)
	}
}

func TestLatencyDoesNotChangeCompareVerdict(t *testing.T) {
	t.Parallel()
	base := Result{Steps: []Observation{{Method: "GET", Path: "/healthz", Status: 200, Body: []byte(`{"status":"ok"}`), DurationNS: 50e6}}}
	patch := Result{Steps: []Observation{{Method: "GET", Path: "/healthz", Status: 200, Body: []byte(`{"status":"ok"}`), DurationNS: 5e6}}}
	rep := Compare(base, patch)
	if rep.Verdict != VerdictMatch {
		t.Fatalf("faster patch must not become a behavior differ: %+v", rep)
	}
	lat := Latency([]Result{base}, []Result{patch})
	if len(lat) != 1 || lat[0].Baseline.N != 1 || lat[0].Patch.N != 1 {
		t.Fatalf("%+v", lat)
	}
}
