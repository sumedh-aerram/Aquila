package plan

import (
	"fmt"

	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/replay"
)

const (
	KindEnv         = "env"
	KindBehavior    = "behavior"
	KindLatency     = "latency"
	KindConcurrency = "concurrency"
	KindTests       = "tests"
	KindFaultStatus = "fault_status"

	DefaultLatencyN     = 20
	DefaultConcurrencyN = 8
)

const (
	VerdictSkipped  = "skipped"
	VerdictSamples  = "samples"
	VerdictPrepared = "prepared"
)

// Step is one node in the minimum useful experiment DAG.
type Step struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Required  bool     `json:"required"`
	Operator  bool     `json:"operator,omitempty"`
	N         int      `json:"n,omitempty"`
	Status    int      `json:"status,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
	Reason    string   `json:"reason"`
}

// DAG is the smallest experiment set Aquila can currently execute or name.
type DAG struct {
	Steps        []Step   `json:"steps"`
	Notes        []string `json:"notes,omitempty"`
	Module       string   `json:"module,omitempty"`
	TestPackages []string `json:"test_packages,omitempty"`
}

// FromImpact builds a DAG from impact findings. Tests are added later via
// WithTests when a Go module is on the operator machine.
func FromImpact(rep impact.Report) (DAG, error) {
	if len(rep.Files) == 0 && len(rep.Direct) == 0 && len(rep.Likely) == 0 && len(rep.Runtime) == 0 && len(rep.Unobserved) == 0 {
		return DAG{}, fmt.Errorf("plan: empty impact")
	}
	dag := DAG{
		Steps: []Step{
			{
				ID:       "env",
				Kind:     KindEnv,
				Required: true,
				Reason:   "GET /healthz on baseline and patch; compose is a local shop path",
			},
			{
				ID:        "behavior",
				Kind:      KindBehavior,
				Required:  true,
				DependsOn: []string{"env"},
				Reason:    "first-run status and json compare",
			},
			{
				ID:        "latency",
				Kind:      KindLatency,
				Required:  true,
				N:         DefaultLatencyN,
				DependsOn: []string{"env"},
				Reason:    "successful-sample median; p95 withheld below 20; no regression threshold",
			},
		},
		Notes: []string{
			"not validated until required executable steps succeed on a clean recorded revision. match is not a pass.",
		},
	}
	if hasRuntime(rep) {
		dag.Steps = append(dag.Steps,
			Step{
				ID:        "concurrency",
				Kind:      KindConcurrency,
				Required:  true,
				N:         DefaultConcurrencyN,
				DependsOn: []string{"env"},
				Reason:    "parallel replay error-rate compare; not a latency threshold",
			},
			Step{
				ID:        "fault_status",
				Kind:      KindFaultStatus,
				Required:  false,
				Status:    502,
				DependsOn: []string{"env"},
				Reason:    "one-sided 502 inject on patch via loopback proxy; probe, not a patch verdict",
			},
		)
		dag.Notes = append(dag.Notes, "fault is a one-sided inject probe and does not vote overall")
	} else {
		dag.Notes = append(dag.Notes, "concurrency and fault omitted: no observed runtime path")
	}
	return dag, nil
}

// WithLatencyN returns a copy with latency repeats set to n when n >= 1.
func WithLatencyN(dag DAG, n int) DAG {
	if n < 1 {
		return dag
	}
	steps := append([]Step(nil), dag.Steps...)
	for i := range steps {
		if steps[i].Kind == KindLatency {
			steps[i].N = n
		}
	}
	dag.Steps = steps
	return dag
}

// WithLocalEnv marks the env step as executed by Aquila (shop pair only).
func WithLocalEnv(dag DAG) DAG {
	steps := append([]Step(nil), dag.Steps...)
	for i := range steps {
		if steps[i].Kind != KindEnv {
			continue
		}
		steps[i].Operator = false
		steps[i].Reason = "isolated shop pair started by aquila; GET /healthz; traces stay out of the live store"
	}
	dag.Steps = steps
	return dag
}

func hasRuntime(rep impact.Report) bool {
	return len(rep.Runtime) > 0
}

func hasKind(dag DAG, kind string) bool {
	for _, s := range dag.Steps {
		if s.Kind == kind {
			return true
		}
	}
	return false
}

func replayRepeats(dag DAG) (n int, need bool) {
	n = 1
	for _, s := range dag.Steps {
		if s.Operator {
			continue
		}
		switch s.Kind {
		case KindBehavior:
			need = true
		case KindLatency:
			need = true
			if s.N > n {
				n = s.N
			}
		}
	}
	return n, need
}

func concurrencyN(dag DAG) (n int, need bool) {
	n = DefaultConcurrencyN
	for _, s := range dag.Steps {
		if s.Operator || s.Kind != KindConcurrency {
			continue
		}
		need = true
		if s.N > 0 {
			n = s.N
		}
	}
	return n, need
}

func compareFirst(base, patch []replay.Result) replay.Report {
	if len(base) == 0 || len(patch) == 0 {
		return replay.Report{Verdict: replay.VerdictIncomplete}
	}
	return replay.Compare(base[0], patch[0])
}
