package runs

import (
	"strings"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/evidence"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

func TestMemoryInsertGetList(t *testing.T) {
	t.Parallel()
	m := NewMemory()
	older := sampleArtifact(t, time.Date(2026, 9, 22, 18, 0, 0, 0, time.UTC), replay.VerdictMatch)
	newer := sampleArtifact(t, time.Date(2026, 9, 22, 19, 0, 0, 0, time.UTC), replay.VerdictDiffer)
	if _, err := m.Insert(t.Context(), Record{Artifact: older}); err != nil {
		t.Fatal(err)
	}
	got, err := m.Insert(t.Context(), Record{Artifact: newer})
	if err != nil {
		t.Fatal(err)
	}
	if got.Validated || got.ID == "" || got.ArtifactDigest == "" {
		t.Fatalf("%+v", got)
	}
	listed, err := m.List(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].Overall != replay.VerdictDiffer || listed[1].Overall != replay.VerdictMatch {
		t.Fatalf("%+v", listed)
	}
	if listed[0].Artifact.Schema != "" {
		t.Fatal("list must omit artifact bodies")
	}
	loaded, err := m.Get(t.Context(), got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Artifact.Result.Overall != replay.VerdictDiffer || loaded.Validated {
		t.Fatalf("%+v", loaded)
	}
}

func TestMemoryInsertIdempotent(t *testing.T) {
	t.Parallel()
	m := NewMemory()
	a := sampleArtifact(t, time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC), replay.VerdictMatch)
	first, err := m.Insert(t.Context(), Record{Artifact: a})
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Insert(t.Context(), Record{Artifact: a})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("%s vs %s", first.ID, second.ID)
	}
	listed, err := m.List(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("len=%d", len(listed))
	}
}

func TestMemoryRejectsPass(t *testing.T) {
	t.Parallel()
	a := sampleArtifact(t, time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC), replay.VerdictMatch)
	a.Result.Overall = "pass"
	a.ArtifactDigest = ""
	_, err := NewMemory().Insert(t.Context(), Record{Artifact: a})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMemoryGetMissing(t *testing.T) {
	t.Parallel()
	_, err := NewMemory().Get(t.Context(), "aaaaaaaaaaaaaaaa")
	if err != ErrNotFound {
		t.Fatalf("err=%v", err)
	}
}

func TestMemoryRejectsInvalidID(t *testing.T) {
	t.Parallel()
	_, err := NewMemory().Get(t.Context(), "../spans")
	if err == nil || !strings.Contains(err.Error(), "invalid id") {
		t.Fatalf("err=%v", err)
	}
}

func TestFromArtifactNeverValidated(t *testing.T) {
	t.Parallel()
	a := sampleArtifact(t, time.Date(2026, 9, 22, 20, 0, 0, 0, time.UTC), replay.VerdictIncomplete)
	rec, err := FromArtifact(a)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Validated || rec.Overall != replay.VerdictIncomplete || rec.BaselineSHA != "abc123" {
		t.Fatalf("%+v", rec)
	}
}

func sampleArtifact(t *testing.T, now time.Time, overall string) evidence.Artifact {
	t.Helper()
	return evidence.Build(evidence.Input{
		Now:         now,
		Baseline:    "http://127.0.0.1:18180",
		Patch:       "http://127.0.0.1:18280",
		BaselineSHA: "abc123",
		Dirty:       true,
		Workload:    replay.Workload{Steps: []replay.Step{{Method: "GET", Path: "/healthz", Provenance: replay.ProvenanceShopFixture}}},
		Impact:      impact.Report{Files: []string{"internal/payment/handler.go"}, Direct: []impact.Finding{{Name: "chargeProcessor"}}},
		Plan:        plan.DAG{Steps: []plan.Step{{ID: "behavior", Kind: plan.KindBehavior}}},
		Result: plan.Evidence{
			Overall: overall,
			Steps:   []plan.StepResult{{ID: "behavior", Kind: plan.KindBehavior, Verdict: overall}},
			Notes:   []string{"not validated"},
		},
	})
}
