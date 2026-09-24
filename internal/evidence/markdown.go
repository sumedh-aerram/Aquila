package evidence

import (
	"strconv"
	"strings"

	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

// Markdown renders a human projection of an artifact. The JSON is the source of truth.
func Markdown(a Artifact) string {
	var b strings.Builder
	b.WriteString("# Aquila evidence\n\n")
	b.WriteString("schema: " + a.Schema + "\n")
	if !a.Recorded.IsZero() {
		b.WriteString("recorded: " + a.Recorded.UTC().Format("2006-01-02T15:04:05Z") + "\n")
	}
	b.WriteString("overall: " + a.Result.Overall + "\n")
	if a.Validated {
		b.WriteString("validated: true\n")
	} else {
		b.WriteString("validated: false\n")
	}
	if a.BaselineSHA != "" {
		dirty := "clean"
		if a.Dirty {
			dirty = "dirty"
		}
		b.WriteString("git: " + a.BaselineSHA + "  " + dirty + "\n")
	}
	if a.WorkloadDigest != "" {
		b.WriteString("workload: " + a.WorkloadDigest + "\n")
	}
	if a.ArtifactDigest != "" {
		b.WriteString("artifact: " + a.ArtifactDigest + "\n")
	}
	b.WriteString("\n## Gateways\n\n")
	b.WriteString("- baseline: " + a.Baseline + "\n")
	b.WriteString("- patch: " + a.Patch + "\n\n")
	b.WriteString("## Impact\n\n")
	b.WriteString("files=" + strconv.Itoa(a.Impact.Files) +
		"  direct=" + strconv.Itoa(a.Impact.Direct) +
		"  likely=" + strconv.Itoa(a.Impact.Likely) +
		"  runtime=" + strconv.Itoa(a.Impact.Runtime) +
		"  unobserved=" + strconv.Itoa(a.Impact.Unobserved) + "\n")
	if len(a.Impact.DirectName) > 0 {
		b.WriteString("direct: " + strings.Join(a.Impact.DirectName, ", ") + "\n")
	}
	if c := a.Coverage; c != nil {
		b.WriteString("\n## Coverage\n\n")
		b.WriteString("impacted=" + strconv.Itoa(len(c.Impacted)) +
			"  exercised=" + strconv.Itoa(len(c.Exercised)) +
			"  missed=" + strconv.Itoa(len(c.Missed)) + "\n")
		for _, r := range c.Exercised {
			b.WriteString("- exercised " + r + "\n")
		}
		for _, r := range c.Missed {
			b.WriteString("- missed " + r + "\n")
		}
	}
	b.WriteString("\n## Workload\n\n")
	if len(a.Workload) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, r := range a.Workload {
			b.WriteString("- " + r.Method + " " + clipQuery(r.Path))
			if r.Provenance != "" {
				b.WriteString("  " + r.Provenance)
			}
			if len(r.Headers) > 0 {
				b.WriteString("  headers=" + strings.Join(r.Headers, ","))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n## Steps\n\n")
	for _, s := range a.Result.Steps {
		b.WriteString("- " + s.Kind + "  " + s.Verdict)
		if len(s.Notes) > 0 && s.Verdict != plan.VerdictSkipped {
			b.WriteString("  " + strings.Join(s.Notes, ","))
		}
		b.WriteString("\n")
		if s.Kind == plan.KindLatency {
			for _, lat := range s.Latency {
				b.WriteString("  - " + lat.Method + " " + clipQuery(lat.Path) + "  " + latencyLine(lat) + "\n")
			}
		}
	}
	if len(a.Result.Notes) > 0 {
		b.WriteString("\n## Notes\n\n")
		for _, n := range a.Result.Notes {
			b.WriteString("- " + n + "\n")
		}
	}
	if a.Validated {
		b.WriteString("\nvalidated. required experiments ran on a clean recorded revision. match is not a ship decision.\n")
	} else {
		b.WriteString("\nnot validated. match is not a pass. this file is evidence, not a policy decision.\n")
	}
	return b.String()
}

func latencyLine(s replay.StepLatency) string {
	return "base " + summaryLine(s.Baseline) + "  patch " + summaryLine(s.Patch)
}

func summaryLine(s replay.Summary) string {
	if s.N == 0 {
		return "n=0"
	}
	out := "n=" + strconv.Itoa(s.N) + "  median=" + formatDur(s.MedNS)
	if s.HasP95 {
		out += "  p95=" + formatDur(s.P95NS)
	} else {
		out += "  p95=withheld"
	}
	return out
}

func formatDur(ns int64) string {
	if ns <= 0 {
		return "-"
	}
	return strconv.FormatFloat(float64(ns)/1e6, 'f', 1, 64) + "ms"
}

// clipQuery hides query values in shareable Markdown; they can carry
// credentials. The JSON artifact keeps the operator's path for replay.
func clipQuery(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		return p[:i] + "?…"
	}
	return p
}
