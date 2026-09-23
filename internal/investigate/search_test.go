package investigate

import (
	"strings"
	"testing"

	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/locate"
)

func TestSearchMatchesRoute(t *testing.T) {
	t.Parallel()
	rep, err := Search(Input{
		Question: "health",
		Origin:   "none",
		Services: []string{"reroute"},
		Routes:   []string{"route reroute  GET /api/health"},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rep.Hits, "\n")
	if !strings.Contains(joined, "/api/health") {
		t.Fatalf("%v", rep.Hits)
	}
}

func TestSearchMatchesObservedHop(t *testing.T) {
	t.Parallel()
	rep, err := Search(Input{
		Question: "why is checkout slow",
		Origin:   "cwd",
		Services: []string{"gateway", "checkout", "payment"},
		Hops: []Hop{
			{From: "gateway", To: "checkout", Provenance: graph.ProvenanceObservedParent},
			{From: "checkout", To: "payment", Provenance: graph.ProvenanceObservedParent},
		},
		Paths: []Path{{
			Services:   []string{"gateway", "checkout", "payment", "processor"},
			Provenance: graph.ProvenanceObservedParent,
		}},
		Binds: []Bind{{
			Service:    "payment",
			Name:       "chargeProcessor",
			File:       "internal/payment/handler.go",
			Line:       142,
			Provenance: locate.ProvenanceCodeAttrs,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(rep.Hits, "\n")
	if !strings.Contains(joined, "checkout") || !strings.Contains(joined, "payment") {
		t.Fatalf("%v", rep.Hits)
	}
	if !strings.Contains(joined, graph.ProvenanceObservedParent) {
		t.Fatalf("missing hop provenance: %v", rep.Hits)
	}
	if !strings.Contains(joined, locate.ProvenanceCodeAttrs) || !strings.Contains(joined, "chargeProcessor") {
		t.Fatalf("must join neighborhood binds: %v", rep.Hits)
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
		Hops:     []Hop{{From: "gateway", To: "checkout", Provenance: graph.ProvenanceObservedParent}},
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
		Direct: []impact.Finding{{Name: "chargeProcessor", File: "internal/payment/handler.go", Reason: "changed_lines"}},
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
	if !strings.Contains(joined, "changed_lines") {
		t.Fatalf("%v", rep.Hits)
	}
}

func TestSearchLocalEditCitesImpact(t *testing.T) {
	t.Parallel()
	imp := impact.Report{
		Files:  []string{"main.go"},
		Direct: []impact.Finding{{Name: "invoice", File: "main.go", Line: 59}},
		Runtime: []impact.Finding{
			{Name: "invoice", Service: "ledger", Route: "GET /invoice", Reason: "bound_span"},
		},
	}
	rep, err := Search(Input{Origin: "cwd", Impact: &imp})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Question != "local edit" {
		t.Fatalf("question=%q", rep.Question)
	}
	joined := strings.Join(rep.Hits, "\n")
	if !strings.Contains(joined, "invoice") || !strings.Contains(joined, "GET /invoice") {
		t.Fatalf("%v", rep.Hits)
	}
}

func TestSearchRejectsEmpty(t *testing.T) {
	t.Parallel()
	if _, err := Search(Input{}); err == nil {
		t.Fatal("expected error")
	}
}
