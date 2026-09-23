package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sumedhaerram/aquila/internal/evidence"
	"github.com/sumedhaerram/aquila/internal/gitrev"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

// RunPlan prints the minimum useful experiment DAG for a unified diff.
func RunPlan(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	api := fs.String("api", envAPI(), "control-plane base URL")
	traces := fs.Int("traces", defaultTraces, "trace window (max 200)")
	file := fs.String("f", "", "diff file (default stdin)")
	dir := fs.String("dir", ".", "module under change (default cwd)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: plan: %w", err)
	}
	path, err := diffPath(*file, fs.Args())
	if err != nil {
		return fmt.Errorf("cli: plan: %w", err)
	}
	raw, err := slurpDiff(stdin, path)
	if err != nil {
		return fmt.Errorf("cli: plan: %w", err)
	}
	rep, err := analyzeDiff(ctx, *api, *dir, *traces, raw)
	if err != nil {
		return err
	}
	dag, err := plan.FromImpact(rep)
	if err != nil {
		return err
	}
	writePlan(stdout, dag)
	return nil
}

func writePlan(w io.Writer, dag plan.DAG) {
	execN, opN := 0, 0
	for _, s := range dag.Steps {
		if s.Operator {
			opN++
		} else {
			execN++
		}
	}
	writef(w, "plan     steps=%d  execute=%d  operator=%d\n", len(dag.Steps), execN, opN)
	for _, s := range dag.Steps {
		role := "execute"
		if s.Operator {
			role = "operator"
		}
		extra := ""
		switch {
		case s.Kind == plan.KindLatency:
			extra = "  n=" + strconv.Itoa(s.N)
		case s.Kind == plan.KindFaultStatus && s.Status > 0:
			extra = "  status=" + strconv.Itoa(s.Status)
		}
		writef(w, "  %-14s %s%s  %s\n", s.Kind, role, extra, s.Reason)
	}
	for _, n := range dag.Notes {
		writef(w, "note     %s\n", n)
	}
	writef(w, "not validated. this is a plan, not evidence.\n")
}

// RunExperiment plans from a diff and executes replay/latency against two gateways.
func RunExperiment(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("experiment", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	api := fs.String("api", envAPI(), "control-plane base URL")
	traces := fs.Int("traces", defaultTraces, "trace window (max 200)")
	file := fs.String("f", "", "diff file (default stdin)")
	base := fs.String("base", "", "baseline gateway URL")
	patch := fs.String("patch", "", "patch gateway URL")
	fixture := fs.Bool("fixture", false, "use shop smoke fixture instead of span routes")
	n := fs.Int("n", 0, "latency repeats (0 uses the plan default)")
	outPath := fs.String("out", "", "write evidence JSON (does not imply a pass)")
	dir := fs.String("dir", ".", "module under change (default cwd)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: experiment: %w", err)
	}
	if *base == "" || *patch == "" {
		return fmt.Errorf("cli: experiment: -base and -patch are required")
	}
	path, err := diffPath(*file, fs.Args())
	if err != nil {
		return fmt.Errorf("cli: experiment: %w", err)
	}
	raw, err := slurpDiff(stdin, path)
	if err != nil {
		return fmt.Errorf("cli: experiment: %w", err)
	}
	rep, err := analyzeDiff(ctx, *api, *dir, *traces, raw)
	if err != nil {
		return err
	}
	dag, err := plan.FromImpact(rep)
	if err != nil {
		return err
	}
	if *n > 0 {
		dag = plan.WithLatencyN(dag, *n)
	}

	var w replay.Workload
	if *fixture {
		w = replay.ShopFixture()
	} else {
		w, err = loadWorkload(ctx, *api, *traces)
		if err != nil {
			return err
		}
	}
	if len(w.Steps) == 0 {
		return fmt.Errorf("cli: experiment: empty workload (no replayable GET routes in server spans)")
	}

	ev, err := plan.Execute(ctx, dag, *base, *patch, w)
	if err != nil {
		return err
	}
	sha, dirty, err := gitrev.State(ctx, *dir)
	if err != nil {
		return fmt.Errorf("cli: experiment: %w", err)
	}
	art := evidence.Build(evidence.Input{
		Now:         time.Now().UTC(),
		Baseline:    *base,
		Patch:       *patch,
		BaselineSHA: sha,
		Dirty:       dirty,
		Workload:    w,
		Impact:      rep,
		Plan:        dag,
		Result:      ev,
	})
	writeEvidence(stdout, ev)
	if sha != "" {
		state := "clean"
		if dirty {
			state = "dirty"
		}
		writef(stdout, "git       %s  %s\n", sha, state)
	}
	if *outPath != "" {
		if err := writeEvidenceFile(*outPath, art); err != nil {
			return err
		}
		writef(stdout, "wrote     %s\n", *outPath)
	}
	recordRun(ctx, *api, stdout, art)
	if ev.Overall == replay.VerdictIncomplete {
		return fmt.Errorf("cli: experiment: incomplete")
	}
	return nil
}

func writeEvidence(w io.Writer, ev plan.Evidence) {
	skipped := 0
	for _, s := range ev.Steps {
		if s.Verdict == plan.VerdictSkipped {
			skipped++
		}
	}
	writef(w, "experiment  overall=%s  steps=%d  skipped=%d\n", ev.Overall, len(ev.Steps), skipped)
	for _, s := range ev.Steps {
		writef(w, "  %-14s %s\n", s.Kind, s.Verdict)
		if s.Kind == plan.KindLatency {
			for _, lat := range s.Latency {
				writef(w, "    latency    %s\n", formatLatency(lat))
			}
		}
		if len(s.Notes) > 0 && s.Verdict != plan.VerdictSkipped {
			writef(w, "    notes      %s\n", joinNotes(s.Notes))
		}
	}
	for _, n := range ev.Notes {
		writef(w, "note         %s\n", n)
	}
	writef(w, "not validated. match is not a pass. skipped operator steps are not a pass.\n")
}

func writeEvidenceFile(path string, a evidence.Artifact) error {
	if strings.Contains(path, "://") {
		return fmt.Errorf("cli: experiment: -out must be a local file")
	}
	raw, err := evidence.Marshal(a)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("cli: experiment: %w", err)
		}
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("cli: experiment: %w", err)
	}
	return nil
}
