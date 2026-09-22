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
	limit := fs.Int("limit", 200, "span list limit when not using -fixture")
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
		c, err := newClient(*api)
		if err != nil {
			return err
		}
		var payload struct {
			Spans []ingest.Span `json:"spans"`
		}
		if err := c.getJSON(ctx, "/v1/spans?limit="+strconv.Itoa(clipList(*limit)), &payload); err != nil {
			return err
		}
		w = replay.FromSpans(payload.Spans)
	}
	if len(w.Steps) == 0 {
		return fmt.Errorf("cli: replay: empty workload (no gateway routes in spans; try -fixture)")
	}

	baseRes, err := replay.Run(ctx, *base, w)
	if err != nil {
		return err
	}
	patchRes, err := replay.Run(ctx, *patch, w)
	if err != nil {
		return err
	}
	rep := replay.Compare(baseRes, patchRes)
	writeReplay(stdout, w, baseRes, patchRes, rep)
	if rep.Verdict == replay.VerdictIncomplete {
		return fmt.Errorf("cli: replay: incomplete")
	}
	return nil
}

func writeReplay(w io.Writer, load replay.Workload, base, patch replay.Result, rep replay.Report) {
	writef(w, "replay   steps=%d  verdict=%s\n", len(load.Steps), rep.Verdict)
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
	}
	writef(w, "not validated. match is not a pass. durations are observations, not a regression claim.\n")
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

func joinNotes(notes []string) string {
	return strings.Join(notes, ",")
}

func clipList(n int) int {
	if n <= 0 {
		return 50
	}
	if n > 200 {
		return 200
	}
	return n
}
