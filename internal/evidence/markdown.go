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
	b.WriteString("validated: false\n")
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
	b.WriteString("\n## Workload\n\n")
	if len(a.Workload) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, r := range a.Workload {
			b.WriteString("- " + r.Method + " " + r.Path)
			if r.Provenance != "" {
				b.WriteString("  " + r.Provenance)
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
				b.WriteString("  - " + lat.Method + " " + lat.Path + "  " + latencyLine(lat) + "\n")
			}
		}
	}
	if len(a.Result.Notes) > 0 {
		b.WriteString("\n## Notes\n\n")
		for _, n := range a.Result.Notes {
			b.WriteString("- " + n + "\n")
		}
	}
	b.WriteString("\nnot validated. match is not a pass. this file is evidence, not a policy decision.\n")
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
