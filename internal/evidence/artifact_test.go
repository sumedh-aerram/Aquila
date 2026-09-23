package evidence

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
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
	if a.Validated || a.Schema != SchemaV1 {
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
		Now:         time.Date(2026, 9, 23, 20, 0, 0, 0, time.UTC),
		Baseline:    "http://127.0.0.1:18180",
		Patch:       "http://127.0.0.1:18280",
		BaselineSHA: "abc123",
		Workload:    replay.Workload{Steps: []replay.Step{{Method: "GET", Path: "/healthz"}}},
		Impact:      impact.Report{Files: []string{"a.go"}, Direct: []impact.Finding{{Name: "F"}}},
		Plan: plan.DAG{Steps: []plan.Step{
			{ID: "env", Kind: plan.KindEnv, Required: true},
			{ID: "behavior", Kind: plan.KindBehavior, Required: true},
			{ID: "latency", Kind: plan.KindLatency, Required: true, N: 20},
		}},
		Result: plan.Evidence{
			Overall: replay.VerdictMatch,
			Steps: []plan.StepResult{
				{ID: "env", Kind: plan.KindEnv, Verdict: plan.VerdictPrepared},
				{ID: "behavior", Kind: plan.KindBehavior, Verdict: replay.VerdictMatch},
				{ID: "latency", Kind: plan.KindLatency, Verdict: plan.VerdictSamples, Latency: lat},
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
	raw := []byte(`{"schema":"aquila.evidence.v1","validated":true,"result":{"overall":"match"}}`)
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
