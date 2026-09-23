package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/replay"
)

// RunReplay executes the same workload against baseline and patch gateways.
func RunReplay(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	api := fs.String("api", envAPI(), "control-plane base URL")
	base := fs.String("base", "", "baseline gateway URL")
	patch := fs.String("patch", "", "patch gateway URL")
	fixture := fs.Bool("fixture", false, "use shop smoke fixture instead of span routes")
	traces := fs.Int("traces", defaultTraces, "trace window for span-derived routes (max 200)")
	n := fs.Int("n", 1, "replay repeats for latency samples (p95 withheld below 20)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: replay: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("cli: replay: unexpected argument %q", fs.Arg(0))
	}
	if *base == "" || *patch == "" {
		return fmt.Errorf("cli: replay: -base and -patch are required")
	}

	var w replay.Workload
	if *fixture {
		w = replay.ShopFixture()
	} else {
		var err error
		w, err = loadWorkload(ctx, *api, *traces)
		if err != nil {
			return err
		}
	}
	if len(w.Steps) == 0 {
		return fmt.Errorf("cli: replay: empty workload (no replayable GET routes in server spans)")
	}

	baseRuns, err := replay.Repeat(ctx, *base, w, *n)
	if err != nil {
		return err
	}
	patchRuns, err := replay.Repeat(ctx, *patch, w, *n)
	if err != nil {
		return err
	}
	rep := replay.Compare(baseRuns[0], patchRuns[0])
	lat := replay.Latency(baseRuns, patchRuns)
	writeReplay(stdout, w, baseRuns[0], patchRuns[0], rep, lat, len(baseRuns))
	if rep.Verdict == replay.VerdictIncomplete {
		return fmt.Errorf("cli: replay: incomplete")
	}
	return nil
}

func writeReplay(w io.Writer, load replay.Workload, base, patch replay.Result, rep replay.Report, lat []replay.StepLatency, n int) {
	writef(w, "replay   steps=%d  verdict=%s  n=%d\n", len(load.Steps), rep.Verdict, n)
	writef(w, "baseline %s\n", base.Target)
	writef(w, "patch    %s\n", patch.Target)
	for i, st := range load.Steps {
		note := ""
		if i < len(rep.Steps) && len(rep.Steps[i].Notes) > 0 {
			note = "  " + joinNotes(rep.Steps[i].Notes)
		}
		bs, bd := stepStatus(base, i)
		ps, pd := stepStatus(patch, i)
		ver := ""
		if i < len(rep.Steps) {
			ver = rep.Steps[i].Status
		}
		writef(w, "  %s %s  %s  %s/%s  %s/%s%s\n",
			st.Method, st.Path, ver, bs, ps, formatDur(bd), formatDur(pd), note)
		writef(w, "    provenance %s\n", st.Provenance)
		if i < len(lat) {
			writef(w, "    latency    %s\n", formatLatency(lat[i]))
		}
	}
	writef(w, "not validated. match is not a pass. p95 is withheld unless n>=20. no regression threshold.\n")
}

func stepStatus(res replay.Result, i int) (status string, dur int64) {
	if i >= len(res.Steps) {
		return "-", 0
	}
	s := res.Steps[i]
	if s.Err != "" {
		return "err", s.DurationNS
	}
	return strconv.Itoa(s.Status), s.DurationNS
}

func formatDur(ns int64) string {
	if ns <= 0 {
		return "-"
	}
	ms := float64(ns) / 1e6
	return strconv.FormatFloat(ms, 'f', 1, 64) + "ms"
}

func formatLatency(s replay.StepLatency) string {
	return "base " + formatSummary(s.Baseline) + "  patch " + formatSummary(s.Patch)
}

func formatSummary(s replay.Summary) string {
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

func joinNotes(notes []string) string {
	return strings.Join(notes, ",")
}

func loadWorkload(ctx context.Context, api string, traces int) (replay.Workload, error) {
	c, err := newClient(api)
	if err != nil {
		return replay.Workload{}, err
	}
	var payload struct {
		Spans []ingest.Span `json:"spans"`
	}
	path := "/v1/spans?traces=" + strconv.Itoa(clipTraces(traces))
	if err := c.getJSON(ctx, path, &payload); err != nil {
		return replay.Workload{}, err
	}
	return replay.FromSpans(payload.Spans), nil
}
