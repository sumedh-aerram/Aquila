package validate

import (
	"net"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/sumedhaerram/aquila/internal/gitrev"
	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/locate"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

const minLatencyN = 20

const (
	// CoverageObserved identifies coverage attributed directly to runtime evidence.
	CoverageObserved = "observed"
	// CoverageInferred identifies coverage attributed through a static caller.
	CoverageInferred = "inferred"
)

// Validation blocker codes are stable identifiers for unmet policy requirements.
const (
	CodeLegacySchema       = "legacy_schema"
	CodeDirtyBaseline      = "dirty_baseline"
	CodeUnverifiedRevision = "unverified_revision"
	CodeMissingTarget      = "missing_target"
	CodeSameTarget         = "same_target"
	CodeNotMatch           = "not_match"
	CodeStepMissing        = "step_missing"
	CodeStepFailed         = "step_failed"
	CodeDuplicateStep      = "duplicate_step"
	CodeLatencyN           = "latency_n"
	CodeLatencyShift       = "latency_shift"
	CodeUnanalyzedChange   = "unanalyzed_change"
	CodeNoFunctionBinding  = "no_function_binding"
	CodeNoRuntimeRoute     = "no_runtime_route"
	CodeRouteMissed        = "route_missed"
)

var coreKinds = []string{plan.KindEnv, plan.KindBehavior, plan.KindLatency, plan.KindConcurrency}

// Blocker explains why executed evidence cannot validate a change.
type Blocker struct {
	Code    string `json:"code"`
	Subject string `json:"subject,omitempty"`
}

// String renders a stable code and optional subject.
func (b Blocker) String() string {
	if b.Subject == "" {
		return b.Code
	}
	return b.Code + ":" + b.Subject
}

// Decision is the validation policy result.
type Decision struct {
	CoverageBasis string    `json:"coverage_basis,omitempty"`
	Validated     bool      `json:"validated"`
	Blockers      []Blocker `json:"blockers"`
}

// Input contains executed evidence and the classified change.
type Input struct {
	Attributions []impact.RouteAttribution
	BaselineRev  gitrev.Revision
	PatchRev     gitrev.Revision
	Baseline     string
	Patch        string
	Plan         plan.DAG
	Result       plan.Evidence
	Impacted     []string
	Workload     []replay.Step
	Changes      impact.Changes
}

// Legacy prevents historical evidence from earning current validation.
func Legacy() Decision { return Decision{Blockers: []Blocker{{Code: CodeLegacySchema}}} }

// Decide evaluates every validation requirement in deterministic policy order.
func Decide(in Input) Decision {
	blockers := []Blocker{}
	if in.BaselineRev.Dirty {
		blockers = append(blockers, Blocker{Code: CodeDirtyBaseline})
	}
	for _, side := range []struct {
		name string
		rev  gitrev.Revision
	}{{"baseline", in.BaselineRev}, {"patch", in.PatchRev}} {
		if side.rev.Tree == "" || side.rev.Source != gitrev.SourceBuilt {
			blockers = append(blockers, Blocker{Code: CodeUnverifiedRevision, Subject: side.name})
		}
	}
	if in.BaselineRev.Tree != "" && in.BaselineRev.Tree == in.PatchRev.Tree {
		blockers = append(blockers, Blocker{Code: CodeSameTarget, Subject: in.BaselineRev.Tree})
	}
	blockers = append(blockers, targetBlockers(in)...)
	if strings.ToLower(strings.TrimSpace(in.Result.Overall)) != replay.VerdictMatch {
		blockers = append(blockers, Blocker{Code: CodeNotMatch, Subject: in.Result.Overall})
	}
	blockers = append(blockers, duplicateBlockers(in)...)
	blockers = append(blockers, stepBlockers(coreKinds, in.Result.Steps)...)
	blockers = append(blockers, requiredStepBlockers(in)...)
	blockers = append(blockers, changeBlockers(in)...)
	blockers = append(blockers, coverageBlockers(in)...)
	return Decision{
		Validated:     len(blockers) == 0,
		Blockers:      blockers,
		CoverageBasis: coverageBasis(in),
	}
}

func targetBlockers(in Input) []Blocker {
	var blockers []Blocker
	if strings.TrimSpace(in.Baseline) == "" {
		blockers = append(blockers, Blocker{Code: CodeMissingTarget, Subject: "baseline"})
	}
	if strings.TrimSpace(in.Patch) == "" {
		blockers = append(blockers, Blocker{Code: CodeMissingTarget, Subject: "patch"})
	}
	if in.Baseline != "" && in.Patch != "" && normURL(in.Baseline) == normURL(in.Patch) {
		blockers = append(blockers, Blocker{Code: CodeSameTarget, Subject: normURL(in.Baseline)})
	}
	return sortedBlockers(blockers)
}

func duplicateBlockers(in Input) []Blocker {
	counts := map[string][2]int{}
	for _, step := range in.Plan.Steps {
		n := counts[step.Kind]
		n[0]++
		counts[step.Kind] = n
	}
	for _, step := range in.Result.Steps {
		n := counts[step.Kind]
		n[1]++
		counts[step.Kind] = n
	}
	var blockers []Blocker
	for kind, n := range counts {
		if n[0] > 1 || n[1] > 1 {
			blockers = append(blockers, Blocker{Code: CodeDuplicateStep, Subject: kind})
		}
	}
	return sortedBlockers(blockers)
}

func stepBlockers(kinds []string, results []plan.StepResult) []Blocker {
	var blockers []Blocker
	for _, kind := range kinds {
		found, failed := false, false
		for _, step := range results {
			if step.Kind != kind {
				continue
			}
			found = true
			failed = failed || !stepOK(kind, step)
			if kind != plan.KindLatency {
				continue
			}
			if n := latencyN(step); n < minLatencyN {
				blockers = append(blockers, Blocker{Code: CodeLatencyN, Subject: strconv.Itoa(n)})
			}
			for _, row := range step.Latency {
				if replay.Shifted(row) {
					blockers = append(blockers, Blocker{Code: CodeLatencyShift, Subject: row.Method + " " + row.Path})
				}
			}
		}
		if !found {
			blockers = append(blockers, Blocker{Code: CodeStepMissing, Subject: kind})
		} else if failed {
			blockers = append(blockers, Blocker{Code: CodeStepFailed, Subject: kind})
		}
	}
	return sortedBlockers(blockers)
}

func requiredStepBlockers(in Input) []Blocker {
	var kinds []string
	for _, step := range in.Plan.Steps {
		if step.Required && !step.Operator && !slices.Contains(coreKinds, step.Kind) && !slices.Contains(kinds, step.Kind) {
			kinds = append(kinds, step.Kind)
		}
	}
	return stepBlockers(kinds, in.Result.Steps)
}

func changeBlockers(in Input) []Blocker {
	var blockers []Blocker
	for _, path := range in.Changes.Unanalyzed {
		blockers = append(blockers, Blocker{Code: CodeUnanalyzedChange, Subject: path})
	}
	if len(in.Changes.Bound) == 0 {
		blockers = append(blockers, Blocker{Code: CodeNoFunctionBinding})
	} else if len(in.Impacted) == 0 {
		blockers = append(blockers, Blocker{Code: CodeNoRuntimeRoute})
	}
	return sortedBlockers(blockers)
}

func coverageBlockers(in Input) []Blocker {
	var blockers []Blocker
	for _, route := range in.Impacted {
		if !Covered([]string{route}, in.Workload, in.Result) {
			blockers = append(blockers, Blocker{Code: CodeRouteMissed, Subject: route})
		}
	}
	return sortedBlockers(blockers)
}

func sortedBlockers(blockers []Blocker) []Blocker {
	sort.Slice(blockers, func(i, j int) bool {
		if blockers[i].Subject == blockers[j].Subject {
			return blockers[i].Code < blockers[j].Code
		}
		return blockers[i].Subject < blockers[j].Subject
	})
	return blockers
}

func normURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.TrimRight(raw, "/")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	switch host {
	case "localhost", "127.0.0.1", "::1":
		host = "127.0.0.1"
	}
	port := u.Port()
	if u.Scheme == "http" && port == "80" || u.Scheme == "https" && port == "443" {
		port = ""
	}
	switch {
	case port != "":
		u.Host = net.JoinHostPort(host, port)
	case strings.Contains(host, ":"):
		u.Host = "[" + host + "]"
	default:
		u.Host = host
	}
	return strings.TrimRight(u.String(), "/")
}

// Covered reports whether some workload step hit an impacted route and both
// revisions answered it from a handler: a recorded status on each side that
// is not an auth refusal. A planned request that never got a reply is not
// coverage.
func Covered(impacted []string, work []replay.Step, res plan.Evidence) bool {
	answered := map[string]struct{}{}
	for _, s := range res.Steps {
		if s.Kind != plan.KindBehavior {
			continue
		}
		for _, d := range s.Steps {
			if d.BaselineStatus == 0 || d.PatchStatus == 0 || d.AuthRejected() {
				continue
			}
			answered[d.Method+" "+d.Path] = struct{}{}
		}
	}
	for _, st := range work {
		if _, ok := answered[st.Method+" "+st.Path]; !ok {
			continue
		}
		for _, route := range impacted {
			if impact.RouteMatches(route, st.Method, st.Path) {
				return true
			}
		}
	}
	return false
}

func stepOK(kind string, got plan.StepResult) bool {
	v := strings.ToLower(strings.TrimSpace(got.Verdict))
	switch kind {
	case plan.KindEnv, plan.KindTests:
		return v == plan.VerdictPrepared
	case plan.KindBehavior, plan.KindConcurrency:
		return v == replay.VerdictMatch
	case plan.KindLatency:
		if v != plan.VerdictSamples || latencyN(got) < minLatencyN {
			return false
		}
		for _, l := range got.Latency {
			if replay.Shifted(l) {
				return false
			}
		}
		return true
	default:
		return v == replay.VerdictMatch || v == plan.VerdictSamples || v == plan.VerdictPrepared
	}
}

func latencyN(got plan.StepResult) int {
	min := 0
	for i, lat := range got.Latency {
		n := lat.Baseline.N
		if lat.Patch.N < n {
			n = lat.Patch.N
		}
		if i == 0 || n < min {
			min = n
		}
	}
	return min
}

// CoverageNote describes the provenance of executed route coverage.
func (d Decision) CoverageNote() string {
	switch d.CoverageBasis {
	case CoverageInferred:
		return " (coverage inferred via caller)"
	case CoverageObserved:
		return " (coverage observed)"
	default:
		return ""
	}
}

func coverageBasis(in Input) string {
	basis := ""
	for _, route := range in.Impacted {
		if !Covered([]string{route}, in.Workload, in.Result) {
			continue
		}
		observed, inferred := false, false
		for _, a := range in.Attributions {
			if a.Route != route {
				continue
			}
			switch a.Provenance {
			case graph.ProvenanceTraceEntry, locate.ProvenanceCodeAttrs:
				observed = true
			case impact.ProvenanceTraceEntryCaller:
				inferred = true
			}
		}
		if inferred && !observed {
			return CoverageInferred
		}
		if observed {
			basis = CoverageObserved
		}
	}
	return basis
}
