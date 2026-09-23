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
	"sync"
	"time"

	"github.com/sumedhaerram/aquila/internal/evidence"
	"github.com/sumedhaerram/aquila/internal/gitrev"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/pair"
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
	service := fs.String("service", "", "OTEL service.name; scopes the trace window")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: plan: %w", err)
	}
	path, err := diffPath(*file, fs.Args())
	if err != nil {
		return fmt.Errorf("cli: plan: %w", err)
	}
	raw, src, err := resolveDiff(ctx, stdin, path, *dir)
	if err != nil {
		return fmt.Errorf("cli: plan: %w", err)
	}
	rep, origin, err := analyzeDiff(ctx, *api, *dir, *traces, *service, raw)
	if err != nil {
		return err
	}
	dag, err := plan.FromImpact(rep)
	if err != nil {
		return err
	}
	dag = plan.WithTests(dag, patchModule(ctx, *dir), rep.Files)
	writef(stdout, "origin   %s  diff=%s\n", origin, src)
	writeEditPlan(stdout, rep)
	writePlan(stdout, dag)
	return nil
}

func writeEditPlan(w io.Writer, rep impact.Report) {
	if len(rep.Direct) == 0 && len(rep.Runtime) == 0 {
		return
	}
	writef(w, "edit\n")
	n := 0
	for _, f := range rep.Direct {
		if n >= 10 {
			break
		}
		writeFinding(w, f)
		n++
	}
	for _, label := range impact.ReplayableRoutes(rep) {
		writef(w, "  replay  %s  changed_lines\n", label)
	}
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
		case s.Kind == plan.KindLatency || s.Kind == plan.KindConcurrency:
			if s.N > 0 {
				extra = "  n=" + strconv.Itoa(s.N)
			}
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

// RunExperiment plans from a diff and executes replay/latency. When -base and
// -patch are omitted it prepares and starts a shop pair, then tears it down.
func RunExperiment(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("experiment", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	api := fs.String("api", envAPI(), "control-plane base URL")
	traces := fs.Int("traces", defaultTraces, "trace window (max 200)")
	file := fs.String("f", "", "diff file (default stdin)")
	base := fs.String("base", "", "baseline gateway URL")
	patch := fs.String("patch", "", "patch gateway URL")
	fixture := fs.Bool("fixture", false, "use shop smoke fixture instead of span routes")
	workload := fs.String("workload", "", "operator workload JSON (not derived from traces)")
	n := fs.Int("n", 0, "latency repeats (0 uses the plan default)")
	smoke := fs.Bool("smoke", false, "latency n=1; cannot validate")
	outPath := fs.String("out", "", "write evidence JSON (does not imply a pass)")
	dir := fs.String("dir", ".", "module under change (default cwd)")
	service := fs.String("service", "", "OTEL service.name; scopes span-derived replay")
	shop := fs.String("shop", defaultShop(), "shop module when starting a local pair")
	pairDir := fs.String("pair", filepath.Join("out", "env"), "parent directory for a local pair")
	basePort := fs.Int("base-port", 0, "baseline gateway host port (default 18180)")
	patchPort := fs.Int("patch-port", 0, "patch gateway host port (default 18280)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: experiment: %w", err)
	}
	hasBase := *base != ""
	hasPatch := *patch != ""
	if hasBase != hasPatch {
		return fmt.Errorf("cli: experiment: -base and -patch are required together; omit both to start a shop pair")
	}
	if *fixture && !allowsShopPair(*dir, *shop) {
		return fmt.Errorf("cli: experiment: -fixture is shop checkout smoke; pass -workload for another app")
	}
	if !hasBase && !allowsShopPair(*dir, *shop) {
		return fmt.Errorf("cli: experiment: -base and -patch are required; omitting them only starts the shop pair")
	}
	path, err := diffPath(*file, fs.Args())
	if err != nil {
		return fmt.Errorf("cli: experiment: %w", err)
	}
	raw, _, err := resolveDiff(ctx, stdin, path, *dir)
	if err != nil {
		return fmt.Errorf("cli: experiment: %w", err)
	}
	rep, _, err := analyzeDiff(ctx, *api, *dir, *traces, *service, raw)
	if err != nil {
		return err
	}
	dag, err := plan.FromImpact(rep)
	if err != nil {
		return err
	}
	if *n > 0 {
		dag = plan.WithLatencyN(dag, *n)
	} else if *smoke {
		dag = plan.WithLatencyN(dag, 1)
	}
	dag = plan.WithTests(dag, patchModule(ctx, *dir), rep.Files)

	w, err := resolveWorkload(ctx, *api, *traces, *service, *fixture, *workload, *dir)
	if err != nil {
		return err
	}
	if len(w.Steps) == 0 {
		return fmt.Errorf("cli: experiment: empty workload (no GET/HEAD/OPTIONS server routes in the trace window; pass -workload for mutating requests)")
	}

	baseURL, patchURL := *base, *patch
	if !hasBase {
		env, err := preparePair(ctx, pair.PrepareOpts{
			ShopDir:   *shop,
			Parent:    *pairDir,
			Diff:      raw,
			BasePort:  *basePort,
			PatchPort: *patchPort,
			Replace:   true,
		})
		if err != nil {
			return err
		}
		defer func() {
			if err := stopPair(context.WithoutCancel(ctx), env); err != nil {
				writef(stdout, "env       down failed: %v\n", err)
				return
			}
			writef(stdout, "env       stopped\n")
		}()
		if err := startPair(ctx, env); err != nil {
			return err
		}
		baseURL = gatewayURL(env.Baseline.Gateway)
		patchURL = gatewayURL(env.Patch.Gateway)
		dag = plan.WithLocalEnv(dag)
		writef(stdout, "env       id=%s  status=up\n", env.ID)
		writef(stdout, "baseline  %s\n", baseURL)
		writef(stdout, "patch     %s\n", patchURL)
	}

	ev, err := plan.Execute(ctx, dag, baseURL, patchURL, w)
	if err != nil {
		return err
	}
	sha, dirty, err := gitrev.State(ctx, *dir)
	if err != nil {
		return fmt.Errorf("cli: experiment: %w", err)
	}
	art := evidence.Build(evidence.Input{
		Now:         time.Now().UTC(),
		Baseline:    baseURL,
		Patch:       patchURL,
		BaselineSHA: sha,
		Dirty:       dirty,
		Service:     ingest.ClipService(*service),
		Workload:    w,
		Impact:      rep,
		Plan:        dag,
		Result:      ev,
	})
	writeEvidence(stdout, ev)
	writeValidated(stdout, art.Validated)
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
}

func writeValidated(w io.Writer, ok bool) {
	writef(w, "validated  %t\n", ok)
	if ok {
		writef(w, "required experiments ran on a clean recorded revision. match is not a ship decision.\n")
		return
	}
	writef(w, "not validated. match is not a pass. skipped required steps are not a pass.\n")
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

var (
	pairHookMu  sync.Mutex
	preparePair = pair.Prepare
	startPair   = pair.Up
	stopPair    = pair.Down
)

func gatewayURL(gateway string) string {
	if strings.HasPrefix(gateway, "http://") || strings.HasPrefix(gateway, "https://") {
		return gateway
	}
	return "http://" + gateway
}
