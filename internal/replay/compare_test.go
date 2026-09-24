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

func TestCompareAuthRejectedOnBothSidesIsIncomplete(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		base, patch int
		want        string
	}{
		{"401 both", 401, 401, VerdictIncomplete},
		{"403 both", 403, 403, VerdictIncomplete},
		{"401 vs 403", 401, 403, VerdictIncomplete},
		{"patch broke auth", 200, 401, VerdictDiffer},
		{"ok both", 200, 200, VerdictMatch},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base := Result{Steps: []Observation{{Method: http.MethodGet, Path: "/user/me", Status: tc.base}}}
			patch := Result{Steps: []Observation{{Method: http.MethodGet, Path: "/user/me", Status: tc.patch}}}
			rep := Compare(base, patch)
			if rep.Verdict != tc.want {
				t.Fatalf("verdict=%s steps=%+v", rep.Verdict, rep.Steps)
			}
			d := rep.Steps[0]
			if d.BaselineStatus != tc.base || d.PatchStatus != tc.patch {
				t.Fatalf("statuses not recorded: %+v", d)
			}
			if d.AuthRejected() != (tc.want == VerdictIncomplete) {
				t.Fatalf("auth_rejected=%v", d.AuthRejected())
			}
		})
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
