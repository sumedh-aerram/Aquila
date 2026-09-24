package evidence

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

func TestCoverageSplitsHitAndMissed(t *testing.T) {
	t.Parallel()
	rep := impact.Report{Runtime: []impact.Finding{
		{Name: "path gateway -> payment", Provenance: "observed_path"},
		{Name: "Lookup", Route: "GET /users/{id}", Provenance: "bound_span"},
		{Name: "Checkout", Route: "POST /checkout", Provenance: "bound_span"},
		{Name: "Items", Route: "GET /items/:sku", Provenance: "bound_span"},
	}}
	work := []Request{{Method: "GET", Path: "/users/user-1"}, {Method: "GET", Path: "/items/a/b"}}
	c := coverageOf(rep, work, answered(replay.Step{Method: "GET", Path: "/users/user-1"}, replay.Step{Method: "GET", Path: "/items/a/b"}))
	if c == nil {
		t.Fatal("nil coverage")
	}
	if want := []string{"GET /items/:sku", "GET /users/{id}", "POST /checkout"}; !slices.Equal(c.Impacted, want) {
		t.Fatalf("impacted=%v", c.Impacted)
	}
	if !slices.Equal(c.Exercised, []string{"GET /users/{id}"}) {
		t.Fatalf("exercised=%v", c.Exercised)
	}
	if !slices.Equal(c.Missed, []string{"GET /items/:sku", "POST /checkout"}) {
		t.Fatalf("missed=%v", c.Missed)
	}
}

func TestArtifactKeepsHeaderNamesNotValues(t *testing.T) {
	t.Parallel()
	in := earnedInput(replay.Step{Method: "POST", Path: "/checkout", Headers: map[string][]string{"Authorization": {"Bearer hunter2"}}})
	a := Build(in)
	raw, err := Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hunter2") {
		t.Fatal("header value persisted")
	}
	if len(a.Workload) != 1 || strings.Join(a.Workload[0].Headers, ",") != "Authorization" {
		t.Fatalf("%+v", a.Workload)
	}
}

func TestCoverageNilWithoutRoutes(t *testing.T) {
	t.Parallel()
	rep := impact.Report{Runtime: []impact.Finding{{Name: "path a -> b"}}}
	if c := coverageOf(rep, []Request{{Method: "GET", Path: "/"}}, plan.Evidence{}); c != nil {
		t.Fatalf("%+v", c)
	}
}

func TestCoverageNeedsReplyOnBothSides(t *testing.T) {
	t.Parallel()
	rep := impact.Report{Runtime: []impact.Finding{{Name: "Lookup", Route: "GET /users/{id}"}}}
	work := []Request{{Method: "GET", Path: "/users/1"}}
	down := plan.Evidence{Steps: []plan.StepResult{{Kind: plan.KindBehavior, Verdict: replay.VerdictIncomplete}}}
	if c := coverageOf(rep, work, down); len(c.Exercised) != 0 || len(c.Rejected) != 0 {
		t.Fatalf("patch never answered: exercised=%v rejected=%v", c.Exercised, c.Rejected)
	}
	half := plan.Evidence{Steps: []plan.StepResult{{Kind: plan.KindBehavior, Steps: []replay.Delta{
		{Method: "GET", Path: "/users/1", BaselineStatus: 200},
	}}}}
	if c := coverageOf(rep, work, half); len(c.Exercised) != 0 {
		t.Fatalf("one-sided reply counted: %v", c.Exercised)
	}
}

// answered is the behavior result of a run where both revisions returned
// 200 for every step.
func answered(work ...replay.Step) plan.Evidence {
	ds := make([]replay.Delta, 0, len(work))
	for _, st := range work {
		ds = append(ds, replay.Delta{Method: st.Method, Path: st.Path, Status: replay.VerdictMatch, BaselineStatus: 200, PatchStatus: 200})
	}
	return plan.Evidence{Steps: []plan.StepResult{{ID: "behavior", Kind: plan.KindBehavior, Verdict: replay.VerdictMatch, Steps: ds}}}
}

func earnedInput(work ...replay.Step) Input {
	lat := replay.Summary{N: 20, MedNS: 1e6, P95NS: 2e6, HasP95: true}
	return Input{
		Now:         time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC),
		Baseline:    "http://127.0.0.1:18180",
		Patch:       "http://127.0.0.1:18280",
		BaselineSHA: "deadbeef",
		Workload:    replay.Workload{Steps: work},
		Impact: impact.Report{
			Files:   []string{"a.go"},
			Runtime: []impact.Finding{{Name: "Charge", Route: "POST /checkout"}},
		},
		Plan: plan.DAG{Steps: []plan.Step{
			{ID: "env", Kind: plan.KindEnv, Required: true},
			{ID: "behavior", Kind: plan.KindBehavior, Required: true},
			{ID: "latency", Kind: plan.KindLatency, Required: true, N: 20},
		}},
		Result: plan.Evidence{Overall: replay.VerdictMatch, Steps: []plan.StepResult{
			{ID: "env", Kind: plan.KindEnv, Verdict: plan.VerdictPrepared},
			answered(work...).Steps[0],
			{ID: "latency", Kind: plan.KindLatency, Verdict: plan.VerdictSamples, Latency: []replay.StepLatency{{Method: "GET", Path: "/healthz", Baseline: lat, Patch: lat}}},
		}},
	}
}

func TestValidatedNotEarnedWhenNoImpactedRouteRan(t *testing.T) {
	t.Parallel()
	hit := Build(earnedInput(replay.Step{Method: "POST", Path: "/checkout"}))
	if !hit.Validated {
		t.Fatal("control: a clean matched run that hits the impacted route must earn validated")
	}
	a := Build(earnedInput(replay.Step{Method: "GET", Path: "/healthz"}))
	if a.Validated {
		t.Fatal("validated without exercising any impacted route")
	}
	forged := a
	forged.Validated = true
	forged.ArtifactDigest = digestArtifact(forged)
	raw, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(bytes.NewReader(raw)); err == nil || !strings.Contains(err.Error(), "unearned") {
		t.Fatalf("forged validated must be rejected, err=%v", err)
	}
	if md := Markdown(a); !strings.Contains(md, "missed POST /checkout") {
		t.Fatalf("%s", md)
	}
}

func TestAuthRejectedRouteIsNotExercised(t *testing.T) {
	t.Parallel()
	in := earnedInput(replay.Step{Method: "POST", Path: "/checkout"})
	in.Result.Steps[1].Steps = []replay.Delta{{
		Method: "POST", Path: "/checkout", Status: replay.VerdictIncomplete,
		BaselineStatus: 401, PatchStatus: 401, Notes: []string{replay.NoteAuthRejected},
	}}
	a := Build(in)
	if a.Validated {
		t.Fatal("validated when the only impacted route was refused by auth on both sides")
	}
	if a.Coverage == nil || len(a.Coverage.Exercised) != 0 || !slices.Equal(a.Coverage.Rejected, []string{"POST /checkout"}) {
		t.Fatalf("coverage=%+v", a.Coverage)
	}
}

func TestValidatedNotEarnedWhenChangedCodeNeverRan(t *testing.T) {
	t.Parallel()
	in := earnedInput(replay.Step{Method: "GET", Path: "/health"})
	in.Impact = impact.Report{Files: []string{"errors.go"}, Direct: []impact.Finding{{Name: "writeMetricError"}}}
	a := Build(in)
	if a.Coverage != nil {
		t.Fatalf("no runtime join means no coverage section: %+v", a.Coverage)
	}
	if a.Validated {
		t.Fatal("validated a change whose code never joined an observed route")
	}
}
