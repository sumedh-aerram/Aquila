package evidence

import (
	"github.com/sumedhaerram/aquila/internal/impact"
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
	Impacted  []string `json:"impacted"`
	Exercised []string `json:"exercised"`
	Missed    []string `json:"missed"`
	Rejected  []string `json:"rejected,omitempty"`
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
	c := &Coverage{Impacted: impacted, Exercised: []string{}, Missed: []string{}}
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
