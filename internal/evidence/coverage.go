package evidence

import (
	"sort"
	"strconv"
	"strings"

	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/locate"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
	"github.com/sumedhaerram/aquila/internal/validate"
)

// Coverage compares production routes joined to the change against the
// workload that ran. Missed routes were on the blast radius but got no request,
// so baseline/patch agreement says nothing about them.
// Rejected routes got requests that both sides refused with 401/403; they are
// also in Missed because the handler under test never ran.
type Coverage struct {
	TraceCounts  []impact.RouteTraceCount  `json:"trace_counts,omitempty"`
	Attributions []impact.RouteAttribution `json:"attributions,omitempty"`
	Impacted     []string                  `json:"impacted"`
	Exercised    []string                  `json:"exercised"`
	Missed       []string                  `json:"missed"`
	Rejected     []string                  `json:"rejected,omitempty"`
}

func coverageOf(rep impact.Report, work []Request, res plan.Evidence) *Coverage {
	impacted := impact.RuntimeRoutes(rep)
	if len(impacted) == 0 {
		return nil
	}
	refused := map[string]struct{}{}
	for _, s := range res.Steps {
		if s.Kind != plan.KindBehavior {
			continue
		}
		for _, d := range s.Steps {
			if d.AuthRejected() {
				refused[d.Method+" "+d.Path] = struct{}{}
			}
		}
	}
	c := &Coverage{
		Impacted:    impacted,
		Exercised:   []string{},
		Missed:      []string{},
		TraceCounts: rep.RouteTraceCounts,
	}
	seen := make(map[impact.RouteAttribution]bool)
	for _, f := range rep.Runtime {
		if f.Route == "" {
			continue
		}
		a := impact.RouteAttribution{
			Route:      f.Route,
			Provenance: f.Provenance,
			Via:        f.Via,
		}
		if !seen[a] {
			seen[a] = true
			c.Attributions = append(c.Attributions, a)
		}
	}
	sort.Slice(c.Attributions, func(i, j int) bool {
		a, b := c.Attributions[i], c.Attributions[j]
		if a.Route != b.Route {
			return a.Route < b.Route
		}
		if a.Provenance != b.Provenance {
			return a.Provenance < b.Provenance
		}
		return a.Via < b.Via
	})
	for _, route := range impacted {
		var reached, rejected bool
		for _, r := range work {
			if !impact.RouteMatches(route, r.Method, r.Path) {
				continue
			}
			if validate.Covered([]string{route}, []replay.Step{{Method: r.Method, Path: r.Path}}, res) {
				reached = true
				break
			}
			if _, ok := refused[r.Method+" "+r.Path]; ok {
				rejected = true
			}
		}
		switch {
		case reached:
			c.Exercised = append(c.Exercised, route)
		case rejected:
			c.Missed = append(c.Missed, route)
			c.Rejected = append(c.Rejected, route)
		default:
			c.Missed = append(c.Missed, route)
		}
	}
	return c
}

// RouteLabel includes every recorded attribution for a route in human-readable coverage.
func (c Coverage) RouteLabel(route string) string {
	seen := make(map[string]bool)
	var labels []string
	for _, a := range c.Attributions {
		if a.Route != route {
			continue
		}
		label := "unclassified"
		switch a.Provenance {
		case graph.ProvenanceTraceEntry, locate.ProvenanceCodeAttrs:
			label = validate.CoverageObserved
		case impact.ProvenanceTraceEntryCaller:
			label = validate.CoverageInferred
			if a.Via != "" {
				label += " via " + a.Via
			}
		}
		if !seen[label] {
			seen[label] = true
			labels = append(labels, label)
		}
	}
	for _, count := range c.TraceCounts {
		if count.Route == route {
			labels = append(labels, "traces="+strconv.Itoa(count.Traces))
		}
	}
	if len(labels) == 0 {
		return route
	}
	sort.Strings(labels)
	return route + "  " + strings.Join(labels, "; ")
}
