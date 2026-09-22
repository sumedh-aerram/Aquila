package replay

import (
	"net/http"
	"testing"
)

func TestCompareMatchIgnoresVolatileIDs(t *testing.T) {
	t.Parallel()
	base := Result{Steps: []Observation{{
		Method: http.MethodPost, Path: "/checkout", Status: 200,
		Body: []byte(`{"id":"chk-a","status":"confirmed","total_cents":499,"user_id":"user-1"}`),
	}}}
	patch := Result{Steps: []Observation{{
		Method: http.MethodPost, Path: "/checkout", Status: 200,
		Body: []byte(`{"id":"chk-b","status":"confirmed","total_cents":499,"user_id":"user-1"}`),
	}}}
	rep := Compare(base, patch)
	if rep.Verdict != VerdictMatch {
		t.Fatalf("%+v", rep)
	}
}

func TestCompareDetectsExtraJSONField(t *testing.T) {
	t.Parallel()
	base := Result{Steps: []Observation{{
		Method: http.MethodGet, Path: "/healthz", Status: 200,
		Body: []byte(`{"status":"ok"}`),
	}}}
	patch := Result{Steps: []Observation{{
		Method: http.MethodGet, Path: "/healthz", Status: 200,
		Body: []byte(`{"status":"ok","debug":true}`),
	}}}
	rep := Compare(base, patch)
	if rep.Verdict != VerdictDiffer {
		t.Fatalf("extra field must differ, got %+v", rep)
	}
}

func TestCompareDetectsStatusMismatch(t *testing.T) {
	t.Parallel()
	base := Result{Steps: []Observation{{Method: http.MethodGet, Path: "/healthz", Status: 200, Body: []byte(`{"status":"ok"}`)}}}
	patch := Result{Steps: []Observation{{Method: http.MethodGet, Path: "/healthz", Status: 500, Body: []byte(`{"status":"ok"}`)}}}
	rep := Compare(base, patch)
	if rep.Verdict != VerdictDiffer {
		t.Fatalf("%+v", rep)
	}
}

func TestCompareDetectsSelectedFieldChange(t *testing.T) {
	t.Parallel()
	base := Result{Steps: []Observation{{
		Method: http.MethodPost, Path: "/checkout", Status: 200,
		Body: []byte(`{"id":"chk-a","status":"confirmed","total_cents":499}`),
	}}}
	patch := Result{Steps: []Observation{{
		Method: http.MethodPost, Path: "/checkout", Status: 200,
		Body: []byte(`{"id":"chk-a","status":"confirmed","total_cents":1}`),
	}}}
	rep := Compare(base, patch)
	if rep.Verdict != VerdictDiffer {
		t.Fatalf("total_cents change must differ: %+v", rep)
	}
}

func TestCompareTransportErrorIsIncompleteNotMatch(t *testing.T) {
	t.Parallel()
	base := Result{Steps: []Observation{{Method: http.MethodGet, Path: "/healthz", Status: 200, Body: []byte(`{}`)}}}
	patch := Result{Steps: []Observation{{Method: http.MethodGet, Path: "/healthz", Err: "connection refused"}}}
	rep := Compare(base, patch)
	if rep.Verdict != VerdictIncomplete {
		t.Fatalf("down patch must be incomplete, got %+v", rep)
	}
	if rep.Verdict == VerdictMatch {
		t.Fatal("must not treat a failed replay as a match")
	}
}

func TestCompareNeverSaysPass(t *testing.T) {
	t.Parallel()
	rep := Compare(
		Result{Steps: []Observation{{Method: http.MethodGet, Path: "/healthz", Status: 200, Body: []byte(`{"status":"ok"}`)}}},
		Result{Steps: []Observation{{Method: http.MethodGet, Path: "/healthz", Status: 200, Body: []byte(`{"status":"ok"}`)}}},
	)
	if rep.Verdict == "pass" || rep.Verdict == "PASS" || rep.Verdict == "failed" {
		t.Fatalf("verdict must be match/differ/incomplete, got %q", rep.Verdict)
	}
	if rep.Verdict != VerdictMatch {
		t.Fatalf("%q", rep.Verdict)
	}
}

func TestCompareStepCountMismatchIncomplete(t *testing.T) {
	t.Parallel()
	base := Result{Steps: []Observation{
		{Method: http.MethodGet, Path: "/healthz", Status: 200, Body: []byte(`{}`)},
		{Method: http.MethodGet, Path: "/users/user-1", Status: 200, Body: []byte(`{}`)},
	}}
	patch := Result{Steps: []Observation{
		{Method: http.MethodGet, Path: "/healthz", Status: 200, Body: []byte(`{}`)},
	}}
	rep := Compare(base, patch)
	if rep.Verdict != VerdictIncomplete {
		t.Fatalf("%+v", rep)
	}
}
