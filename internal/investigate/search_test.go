package investigate

import (
	"strings"
	"testing"

	"github.com/sumedhaerram/aquila/internal/impact"
)

func TestSearchMatchesObservedHop(t *testing.T) {
	t.Parallel()
	rep, err := Search(Input{
		Question: "why is checkout slow",
		Origin:   "cwd",
		Services: []string{"gateway", "checkout", "payment"},
		Hops:     []string{"gateway -> checkout", "checkout -> payment"},
		Paths:    []string{"gateway -> checkout -> payment -> processor"},
		Binds:    []string{"payment chargeProcessor internal/payment/handler.go:142"},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rep.Hits, "\n")
	if !strings.Contains(joined, "checkout") || !strings.Contains(joined, "payment") {
		t.Fatalf("%v", rep.Hits)
	}
	if !strings.Contains(strings.Join(rep.Notes, "\n"), "not a patch") {
		t.Fatalf("%v", rep.Notes)
	}
	if strings.Contains(joined, "pass") {
		t.Fatal("must not report pass")
	}
}

func TestSearchEmptyHitsWhenNoTokenMatches(t *testing.T) {
	t.Parallel()
	rep, err := Search(Input{
		Question: "redis lock contention",
		Hops:     []string{"gateway -> checkout"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Hits) != 0 {
		t.Fatalf("guessed hits: %v", rep.Hits)
	}
	if !strings.Contains(strings.Join(rep.Notes, "\n"), "no token matched") {
		t.Fatalf("%v", rep.Notes)
	}
}

func TestSearchIncludesImpactWhenPresent(t *testing.T) {
	t.Parallel()
	imp := impact.Report{
		Files:  []string{"internal/payment/handler.go"},
		Direct: []impact.Finding{{Name: "chargeProcessor", File: "internal/payment/handler.go"}},
	}
	rep, err := Search(Input{
		Question: "chargeProcessor authorize",
		Impact:   &imp,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rep.Hits, "\n")
	if !strings.Contains(joined, "chargeProcessor") {
		t.Fatalf("%v", rep.Hits)
	}
}

func TestSearchRejectsEmpty(t *testing.T) {
	t.Parallel()
	if _, err := Search(Input{}); err == nil {
		t.Fatal("expected error")
	}
}
