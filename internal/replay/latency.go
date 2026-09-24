package replay

import (
	"math"
	"sort"
)

const minPercentileN = 20

// A step's latency has shifted when the patch median is more than
// ShiftRatio times the baseline median and at least ShiftMinNS slower. Both
// bounds must hold so sub-millisecond noise on fast routes does not trip it.
const (
	ShiftRatio = 2
	ShiftMinNS = int64(5_000_000)
)

// Shifted reports whether l crosses the documented shift bounds. Steps
// without successful samples on both sides are not judged here.
func Shifted(l StepLatency) bool {
	if l.Baseline.N == 0 || l.Patch.N == 0 {
		return false
	}
	b, p := l.Baseline.MedNS, l.Patch.MedNS
	return p > ShiftRatio*b && p-b >= ShiftMinNS
}

// Summary is nearest-rank latency of successful samples only. Missing
// percentiles are withheld rather than invented.
type Summary struct {
	N      int   `json:"n"`
	MinNS  int64 `json:"min_ns,omitempty"`
	MedNS  int64 `json:"median_ns,omitempty"`
	P95NS  int64 `json:"p95_ns,omitempty"`
	P99NS  int64 `json:"p99_ns,omitempty"`
	HasP95 bool  `json:"has_p95"`
	HasP99 bool  `json:"has_p99"`
}

// StepLatency is baseline vs patch latency for one workload step.
type StepLatency struct {
	Method   string  `json:"method"`
	Path     string  `json:"path"`
	Baseline Summary `json:"baseline"`
	Patch    Summary `json:"patch"`
}

// Summarize computes min/median and, when n is large enough, p95/p99.
func Summarize(samples []int64) Summary {
	out := Summary{N: len(samples)}
	if out.N == 0 {
		return out
	}
	cp := append([]int64(nil), samples...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	out.MinNS = cp[0]
	out.MedNS = rank(cp, 0.50)
	if out.N >= minPercentileN {
		out.P95NS = rank(cp, 0.95)
		out.P99NS = rank(cp, 0.99)
		out.HasP95 = true
		out.HasP99 = true
	}
	return out
}

func rank(sorted []int64, p float64) int64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(n))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= n {
		idx = n - 1
	}
	return sorted[idx]
}

func successfulDurations(runs []Result, step int) []int64 {
	var out []int64
	for _, r := range runs {
		if step < 0 || step >= len(r.Steps) {
			continue
		}
		s := r.Steps[step]
		if s.Err != "" || s.Status <= 0 || s.DurationNS <= 0 {
			continue
		}
		out = append(out, s.DurationNS)
	}
	return out
}

// Latency compares successful sample durations per step. It does not vote on verdict.
func Latency(base, patch []Result) []StepLatency {
	n := 0
	if len(base) > 0 {
		n = len(base[0].Steps)
	}
	if len(patch) > 0 && len(patch[0].Steps) < n {
		n = len(patch[0].Steps)
	}
	out := make([]StepLatency, 0, n)
	for i := 0; i < n; i++ {
		st := StepLatency{}
		if len(base) > 0 && i < len(base[0].Steps) {
			st.Method = base[0].Steps[i].Method
			st.Path = base[0].Steps[i].Path
		}
		st.Baseline = Summarize(successfulDurations(base, i))
		st.Patch = Summarize(successfulDurations(patch, i))
		out = append(out, st)
	}
	return out
}
