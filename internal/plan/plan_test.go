package plan

import (
	"strings"
	"testing"

	"github.com/sumedhaerram/aquila/internal/impact"
)

func TestFromImpactEmpty(t *testing.T) {
	t.Parallel()
	if _, err := FromImpact(impact.Report{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestFromImpactAlwaysBehaviorAndLatency(t *testing.T) {
	t.Parallel()
	dag, err := FromImpact(impact.Report{
		Files:  []string{"internal/payment/handler.go"},
		Direct: []impact.Finding{{Name: "chargeProcessor", File: "internal/payment/handler.go", Reason: "changed_lines"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasKind(dag, KindBehavior) || !hasKind(dag, KindLatency) || !hasKind(dag, KindEnv) {
		t.Fatalf("missing required steps: %+v", dag.Steps)
	}
	if hasKind(dag, KindFaultStatus) {
		t.Fatal("fault requires observed runtime")
	}
	joined := strings.Join(dag.Notes, "\n")
	if !strings.Contains(joined, "not validated") {
		t.Fatalf("notes: %v", dag.Notes)
	}
	if strings.Contains(joined, "validated.") && !strings.Contains(joined, "not validated") {
		t.Fatal("must not claim validated")
	}
	if hasKind(dag, "concurrency") || hasKind(dag, "tests") || hasKind(dag, "ask") {
		t.Fatal("must not invent later experiment classes")
	}
}

func TestFromImpactIncludesOperatorFaultWhenRuntime(t *testing.T) {
	t.Parallel()
	dag, err := FromImpact(impact.Report{
		Files:   []string{"internal/payment/handler.go"},
		Direct:  []impact.Finding{{Name: "chargeProcessor", File: "internal/payment/handler.go", Reason: "changed_lines"}},
		Runtime: []impact.Finding{{Path: "gateway -> payment -> processor", Reason: "observed_path"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var fault Step
	for _, s := range dag.Steps {
		if s.Kind == KindFaultStatus {
			fault = s
		}
	}
	if !fault.Operator || fault.Status != 502 {
		t.Fatalf("%+v", fault)
	}
}

func TestWithLatencyN(t *testing.T) {
	t.Parallel()
	dag, err := FromImpact(impact.Report{Files: []string{"a.go"}, Direct: []impact.Finding{{Name: "F"}}})
	if err != nil {
		t.Fatal(err)
	}
	got := WithLatencyN(dag, 3)
	for _, s := range got.Steps {
		if s.Kind == KindLatency && s.N != 3 {
			t.Fatalf("%+v", s)
		}
	}
	origN := 0
	for _, s := range dag.Steps {
		if s.Kind == KindLatency {
			origN = s.N
		}
	}
	if origN != DefaultLatencyN {
		t.Fatal("WithLatencyN must not mutate the original")
	}
}

func hasKind(dag DAG, kind string) bool {
	for _, s := range dag.Steps {
		if s.Kind == kind {
			return true
		}
	}
	return false
}
