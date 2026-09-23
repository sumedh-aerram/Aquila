package plan

import (
	"os"
	"path/filepath"
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
	if hasKind(dag, KindConcurrency) || hasKind(dag, KindTests) || hasKind(dag, KindFaultStatus) {
		t.Fatal("concurrency, tests, and fault require runtime or WithTests")
	}
}

func TestFromImpactIncludesConcurrencyAndFaultWhenRuntime(t *testing.T) {
	t.Parallel()
	dag, err := FromImpact(impact.Report{
		Files:   []string{"internal/payment/handler.go"},
		Direct:  []impact.Finding{{Name: "chargeProcessor", File: "internal/payment/handler.go", Reason: "changed_lines"}},
		Runtime: []impact.Finding{{Path: "gateway -> payment -> processor", Reason: "observed_path"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var fault, conc Step
	for _, s := range dag.Steps {
		switch s.Kind {
		case KindFaultStatus:
			fault = s
		case KindConcurrency:
			conc = s
		}
	}
	if fault.Operator || fault.Required || fault.Status != 502 {
		t.Fatalf("fault %+v", fault)
	}
	if conc.Operator || !conc.Required || conc.N != DefaultConcurrencyN {
		t.Fatalf("concurrency %+v", conc)
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

func TestWithLocalEnv(t *testing.T) {
	t.Parallel()
	dag, err := FromImpact(impact.Report{Files: []string{"a.go"}, Direct: []impact.Finding{{Name: "F"}}})
	if err != nil {
		t.Fatal(err)
	}
	got := WithLocalEnv(dag)
	for _, s := range got.Steps {
		if s.Kind == KindEnv && s.Operator {
			t.Fatal("local env must not stay operator")
		}
	}
	for _, s := range dag.Steps {
		if s.Kind == KindEnv && s.Operator {
			t.Fatal("FromImpact env must already be executable")
		}
	}
}

func TestFromImpactEnvIsExecutable(t *testing.T) {
	t.Parallel()
	dag, err := FromImpact(impact.Report{Files: []string{"a.go"}, Direct: []impact.Finding{{Name: "F"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range dag.Steps {
		if s.Kind == KindEnv && (s.Operator || !s.Required) {
			t.Fatalf("%+v", s)
		}
	}
}

func TestWithTestsAddsStepWhenPackagesExist(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/t\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(root, "p")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "p.go"), []byte("package p\nfunc F() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dag, err := FromImpact(impact.Report{Files: []string{"p/p.go"}, Direct: []impact.Finding{{Name: "F"}}})
	if err != nil {
		t.Fatal(err)
	}
	got := WithTests(dag, root, []string{"p/p.go"})
	if !hasKind(got, KindTests) || len(got.TestPackages) != 1 || got.TestPackages[0] != "./p" {
		t.Fatalf("%+v", got)
	}
}

func TestWithTestsOmitsMissingPackages(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/t\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dag, err := FromImpact(impact.Report{Files: []string{"p/p.go"}, Direct: []impact.Finding{{Name: "F"}}})
	if err != nil {
		t.Fatal(err)
	}
	got := WithTests(dag, root, []string{"p/p.go"})
	if hasKind(got, KindTests) {
		t.Fatal("must not add tests without packages on disk")
	}
}
