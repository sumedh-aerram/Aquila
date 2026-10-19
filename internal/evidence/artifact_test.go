package evidence

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/gitrev"
	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
	"github.com/sumedhaerram/aquila/internal/validate"
)

func TestBuildOmitsBodiesAndIsNeverValidated(t *testing.T) {
	t.Parallel()
	a := Build(Input{
		Now:      time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC),
		Baseline: "http://127.0.0.1:18180",
		Patch:    "http://127.0.0.1:18280",
		Workload: replay.ShopFixture(),
		Impact: impact.Report{
			Files:  []string{"internal/payment/handler.go"},
			Direct: []impact.Finding{{Name: "chargeProcessor"}},
		},
		Plan: plan.DAG{Steps: []plan.Step{{ID: "behavior", Kind: plan.KindBehavior}}},
		Result: plan.Evidence{
			Overall: replay.VerdictDiffer,
			Steps:   []plan.StepResult{{ID: "behavior", Kind: plan.KindBehavior, Verdict: replay.VerdictDiffer, Notes: []string{"json"}}},
			Notes:   []string{"not validated. no pass threshold. no impacted tests."},
		},
	})
	if a.Validated || a.Schema != SchemaV3 {
		t.Fatalf("%+v", a)
	}
	raw, err := Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("sku-widget")) || bytes.Contains(raw, []byte("\"body\"")) {
		t.Fatalf("must not persist request bodies: %s", raw)
	}
	if bytes.Contains(raw, []byte(`"validated": true`)) {
		t.Fatal("validated must be false")
	}
	if a.WorkloadDigest == "" || a.ArtifactDigest == "" {
		t.Fatal("digests required on built artifacts")
	}
}

func TestBuildEarnsValidated(t *testing.T) {
	t.Parallel()
	lat := []replay.StepLatency{{
		Method:   "GET",
		Path:     "/healthz",
		Baseline: replay.Summary{N: 20, HasP95: true},
		Patch:    replay.Summary{N: 20, HasP95: true},
	}}
	a := Build(Input{
		Now:      time.Date(2026, 9, 23, 20, 0, 0, 0, time.UTC),
		Baseline: "http://127.0.0.1:18180",
		Patch:    "http://127.0.0.1:18280",
		BaselineRev: gitrev.Revision{
			Commit: "abc123",
			Tree:   "base",
			Source: gitrev.SourceBuilt,
		},
		PatchRev: gitrev.Revision{
			Tree:   "patch",
			Source: gitrev.SourceBuilt,
		},
		Workload: replay.Workload{Steps: []replay.Step{{Method: "GET", Path: "/healthz"}}},
		Impact: impact.Report{
			Files:   []string{"a.go"},
			Direct:  []impact.Finding{{Name: "F", File: "a.go", SourceID: "f", Reason: "changed_lines"}},
			Runtime: []impact.Finding{{Name: "F", Route: "GET /healthz"}},
		},
		Plan: plan.DAG{Steps: []plan.Step{
			{ID: "env", Kind: plan.KindEnv, Required: true},
			{ID: "behavior", Kind: plan.KindBehavior, Required: true},
			{ID: "latency", Kind: plan.KindLatency, Required: true, N: 20},
		}},
		Result: plan.Evidence{
			Overall: replay.VerdictMatch,
			Steps: []plan.StepResult{
				{ID: "env", Kind: plan.KindEnv, Verdict: plan.VerdictPrepared},
				answered(replay.Step{Method: "GET", Path: "/healthz"}).Steps[0],
				{ID: "latency", Kind: plan.KindLatency, Verdict: plan.VerdictSamples, Latency: lat},
				{ID: "concurrency", Kind: plan.KindConcurrency, Verdict: replay.VerdictMatch},
			},
		},
	})
	if !a.Validated {
		t.Fatal("expected earned")
	}
	raw, err := Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Validated {
		t.Fatal("round trip dropped validated")
	}
	md := Markdown(a)
	if !strings.Contains(md, "validated: true") || strings.Contains(md, "overall: pass") {
		t.Fatalf("%s", md)
	}
}

func TestDecodeRejectsUnearnedValidated(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"schema":"aquila.evidence.v2","validated":true,"result":{"overall":"match"}}`)
	if _, err := Decode(bytes.NewReader(raw)); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeRejectsPassOverall(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"schema":"aquila.evidence.v1","validated":false,"result":{"overall":"pass"}}`)
	if _, err := Decode(bytes.NewReader(raw)); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeRejectsUnknownSchema(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"schema":"aquila.evidence.v0","validated":false,"result":{"overall":"match"}}`)
	if _, err := Decode(bytes.NewReader(raw)); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeNamesUnknownField(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"schema":"aquila.evidence.v1","validated":false,"result":{"overall":"match"},"from_newer_cli":1}`)
	_, err := Decode(bytes.NewReader(raw))
	if err == nil || !strings.Contains(err.Error(), "from_newer_cli") {
		t.Fatalf("err=%v", err)
	}
}

func TestMarkdownNeverSaysPass(t *testing.T) {
	t.Parallel()
	a := sampleDiffer(t)
	md := Markdown(a)
	if strings.Contains(md, "overall: pass") || strings.Contains(md, "validated: true") {
		t.Fatalf("%s", md)
	}
	if !strings.Contains(md, "overall: differ") || !strings.Contains(md, "validated: false") {
		t.Fatalf("%s", md)
	}
	if !strings.Contains(md, "not validated") {
		t.Fatalf("%s", md)
	}
	if !strings.Contains(md, "chargeProcessor") {
		t.Fatalf("%s", md)
	}
}

func TestMarkdownStable(t *testing.T) {
	t.Parallel()
	a := sampleDiffer(t)
	first := Markdown(a)
	second := Markdown(a)
	if first != second {
		t.Fatal("markdown must be deterministic")
	}
}

func TestRoundTripJSON(t *testing.T) {
	t.Parallel()
	a := sampleDiffer(t)
	raw, err := Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !EqualJSON(a, got) {
		t.Fatalf("round trip mismatch\n%s", raw)
	}
}

func TestDecodeRejectsArtifactDigestMismatch(t *testing.T) {
	t.Parallel()
	a := sampleDiffer(t)
	a.ArtifactDigest = strings.Repeat("a", 64)
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(bytes.NewReader(raw)); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeAllowsLegacyWithoutDigest(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"schema":"aquila.evidence.v1","validated":false,"baseline":"http://127.0.0.1:18180","patch":"http://127.0.0.1:18280","result":{"overall":"match"}}`)
	a, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if a.Validated || a.Result.Overall != "match" {
		t.Fatalf("%+v", a)
	}
}

func sampleDiffer(t *testing.T) Artifact {
	t.Helper()
	return Build(Input{
		Now:      time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC),
		Baseline: "http://127.0.0.1:18180",
		Patch:    "http://127.0.0.1:18280",
		Workload: replay.Workload{Steps: []replay.Step{{Method: "GET", Path: "/healthz", Provenance: replay.ProvenanceShopFixture}}},
		Impact: impact.Report{
			Files:  []string{"internal/payment/handler.go"},
			Direct: []impact.Finding{{Name: "chargeProcessor", File: "internal/payment/handler.go"}},
		},
		Plan: plan.DAG{Notes: []string{"not validated. no pass threshold. no impacted tests."}},
		Result: plan.Evidence{
			Overall: replay.VerdictDiffer,
			Steps: []plan.StepResult{
				{ID: "env", Kind: plan.KindEnv, Verdict: plan.VerdictSkipped, Notes: []string{"operator"}},
				{ID: "behavior", Kind: plan.KindBehavior, Verdict: replay.VerdictDiffer, Notes: []string{"json"}},
				{ID: "latency", Kind: plan.KindLatency, Verdict: plan.VerdictSamples, Latency: []replay.StepLatency{{
					Method: "GET", Path: "/healthz",
					Baseline: replay.Summary{N: 1, MedNS: 2e6},
					Patch:    replay.Summary{N: 1, MedNS: 3e6},
				}}},
			},
			Notes: []string{"not validated. no pass threshold. no impacted tests."},
		},
	})
}

func legacyShopArtifact(t *testing.T) Artifact {
	t.Helper()
	raw, err := os.ReadFile("testdata/v1_shop_validated.json")
	if err != nil {
		t.Fatal(err)
	}
	a, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestDecodeLegacyV1Validated(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/v1_shop_validated.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Validated || got.Decision().Validated || got.Decision().Blockers[0].Code != "legacy_schema" {
		t.Fatalf("%+v", got.Decision())
	}
	if err := Verify(got); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeV1TamperedDigest(t *testing.T) {
	t.Parallel()
	a := legacyShopArtifact(t)
	a.Patch = "http://127.0.0.1:9999"
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(bytes.NewReader(raw)); err == nil {
		t.Fatal("tampered v1 accepted")
	}
}

func TestCheckV3RejectsUnearned(t *testing.T) {
	t.Parallel()
	a := Build(earnedInput(replay.Step{Method: "POST", Path: "/checkout"}))
	a.BaselineRev.Dirty = true
	a.Validated = true
	a.ArtifactDigest = digestArtifact(a)
	if err := Check(a); err == nil || !strings.Contains(err.Error(), "unearned") {
		t.Fatalf("%v", err)
	}
}

func TestBuildV3DigestStable(t *testing.T) {
	t.Parallel()
	in := earnedInput(replay.Step{Method: "POST", Path: "/checkout"})
	a, b := Build(in), Build(in)
	if a.Schema != SchemaV3 || a.ArtifactDigest == "" || !EqualJSON(a, b) {
		t.Fatalf("unstable: %+v %+v", a, b)
	}
	a.Blockers = append(a.Blockers, validate.Blocker{Code: "tampered"})
	if err := Verify(a); err == nil {
		t.Fatal("blockers not digest covered")
	}
}

func TestCheckV3RejectsForgedBlockers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		blockers []validate.Blocker
	}{
		{name: "invented", blockers: []validate.Blocker{{Code: validate.CodeDirtyBaseline}}},
		{name: "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := earnedInput(replay.Step{Method: "POST", Path: "/checkout"})
			if tc.name == "missing" {
				in.BaselineRev.Dirty = true
			}
			a := Build(in)
			a.Blockers = tc.blockers
			a.ArtifactDigest = digestArtifact(a)
			if err := Verify(a); err != nil {
				t.Fatal(err)
			}
			if err := Check(a); err == nil || !strings.Contains(err.Error(), "blockers") {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestDecodeM1V2NoRevisions(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/v2_shop_m1_validated.json")
	if err != nil {
		t.Fatal(err)
	}
	a, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !a.Validated {
		t.Fatal("historical bit was rewritten")
	}
	if err := Verify(a); err != nil {
		t.Fatal(err)
	}
	d := a.Decision()
	if d.Validated || len(d.Blockers) != 2 || d.Blockers[0].Code != validate.CodeUnverifiedRevision || d.Blockers[1].Code != validate.CodeUnverifiedRevision {
		t.Fatalf("%+v", d)
	}
	if err := Check(a); err == nil {
		t.Fatal("v2 accepted for write")
	}
	if _, err := Marshal(a); err == nil {
		t.Fatal("v2 marshaled for write")
	}
	encoded, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Decode(bytes.NewReader(encoded))
	if err != nil || again.ArtifactDigest != a.ArtifactDigest {
		t.Fatalf("digest changed: %v", err)
	}
}

func TestCheckRejectsRevisionlessV3Claim(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/v2_shop_m1_validated.json")
	if err != nil {
		t.Fatal(err)
	}
	a, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	a.Schema = SchemaV3
	a.ArtifactDigest = digestArtifact(a)
	if err := Check(a); err == nil || !strings.Contains(err.Error(), "unearned") {
		t.Fatalf("revision-less v3 claim error=%v", err)
	}
	if _, err := Marshal(a); err == nil {
		t.Fatal("revision-less v3 claim marshaled")
	}
	raw, err = json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(bytes.NewReader(raw)); err == nil {
		t.Fatal("forged v3 decoded")
	}
}

func TestMarkdownLegacyOmitsMissingRevisions(t *testing.T) {
	t.Parallel()
	a := legacyShopArtifact(t)
	rendered := Markdown(a)
	if strings.Contains(rendered, "baseline tree:") || strings.Contains(rendered, "patch tree:") {
		t.Fatalf("missing revisions rendered: %s", rendered)
	}
}

func TestRoundTripRuntimeProvenance(t *testing.T) {
	t.Parallel()
	finding := impact.Finding{
		TraceID:     "checkout",
		SpanID:      "authorize",
		EntrySpanID: "gateway",
		Name:        "authorize",
		SourceID:    "func:authorize",
		Service:     "payment",
		Route:       "POST /checkout",
		Reason:      "bound_span",
		Provenance:  graph.ProvenanceTraceEntry,
	}
	a := Build(Input{
		Impact: impact.Report{Runtime: []impact.Finding{finding}},
		Result: plan.Evidence{Overall: replay.VerdictIncomplete},
	})
	raw, err := Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Impact.RuntimeFindings) != 1 || got.Impact.RuntimeFindings[0] != finding {
		t.Fatalf("runtime findings=%+v", got.Impact.RuntimeFindings)
	}
}

func TestDecodeHistoricalV3WithoutTraceCounts(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/v3_shop_m21b.json")
	if err != nil {
		t.Fatal(err)
	}
	a, err := Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(a); err != nil {
		t.Fatal(err)
	}
	if a.Coverage == nil || len(a.Coverage.TraceCounts) != 0 || !a.Decision().Validated || a.Decision().CoverageBasis != "inferred" {
		t.Fatalf("decision=%+v coverage=%+v", a.Decision(), a.Coverage)
	}
}
