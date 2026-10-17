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
	"github.com/sumedhaerram/aquila/internal/validate"
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
	health := fs.String("health", replay.DefaultHealthPath, "GET route the env step probes on both gateways")
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
	var snap buildSnapshot
	var raw []byte
	analysisDir := *dir
	snapshotStart := time.Now()
	if hasBase {
		raw, _, err = resolveDiff(ctx, stdin, path, *dir)
	} else {
		snap, err = captureBuild(ctx, stdin, path, *shop)
		raw = snap.Diff
		analysisDir = *shop
	}
	if err != nil {
		return fmt.Errorf("cli: experiment: %w", err)
	}
	snapshotDuration := time.Since(snapshotStart)
	rep, _, err := analyzeDiff(ctx, *api, analysisDir, *traces, *service, raw)
	if err != nil {
		return fmt.Errorf("cli: experiment: %w", err)
	}
	dag, err := plan.FromImpact(rep)
	if err != nil {
		return fmt.Errorf("cli: experiment: %w", err)
	}
	if *n > 0 {
		dag = plan.WithLatencyN(dag, *n)
	} else if *smoke {
		dag = plan.WithLatencyN(dag, 1)
	}
	dag = plan.WithTests(dag, patchModule(ctx, analysisDir), rep.Files)
	dag = plan.WithHealthPath(dag, *health)

	w, err := resolveWorkload(ctx, *api, *traces, *service, *fixture, *workload, analysisDir)
	if err != nil {
		return fmt.Errorf("cli: experiment: %w", err)
	}
	if len(w.Steps) == 0 {
		return fmt.Errorf("cli: experiment: empty workload (no GET/HEAD/OPTIONS server routes in the trace window; pass -workload for mutating requests)")
	}

	var baselineRev, patchRev gitrev.Revision

	baseURL, patchURL := *base, *patch
	if !hasBase {
		baselineRev, patchRev = snap.Baseline, snap.Patch
		env, err := preparePair(ctx, pair.PrepareOpts{
			Repo:      snap.Repo,
			Baseline:  snap.Baseline,
			Patch:     snap.Patch,
			Parent:    *pairDir,
			BasePort:  *basePort,
			PatchPort: *patchPort,
			Replace:   true,
		})
		if err != nil {
			return fmt.Errorf("cli: experiment: %w", err)
		}
		defer func() {
			if err := stopPair(context.WithoutCancel(ctx), env); err != nil {
				writef(stdout, "env       down failed: %v\n", err)
				return
			}
			writef(stdout, "env       stopped\n")
		}()
		if err := startPair(ctx, env); err != nil {
			return fmt.Errorf("cli: experiment: %w", err)
		}
		baselineRev.Source = gitrev.SourceBuilt
		patchRev.Source = gitrev.SourceBuilt
		baseURL = gatewayURL(env.Baseline.Gateway)
		patchURL = gatewayURL(env.Patch.Gateway)
		dag = plan.WithLocalEnv(dag)
		writef(stdout, "env       id=%s  status=up snapshot=%s\n", env.ID, snapshotDuration)
		writef(stdout, "baseline  %s\n", baseURL)
		writef(stdout, "patch     %s\n", patchURL)
	} else {
		var err error
		baselineRev, patchRev, err = declaredRevisions(ctx, *dir)
		if err != nil {
			return fmt.Errorf("cli: experiment: %w", err)
		}
	}
	writeRevisions(stdout, baselineRev, patchRev)

	ev, err := plan.Execute(ctx, dag, baseURL, patchURL, w)
	if err != nil {
		return fmt.Errorf("cli: experiment: %w", err)
	}
	art := evidence.Build(evidence.Input{
		Now:         time.Now().UTC(),
		Baseline:    baseURL,
		Patch:       patchURL,
		BaselineRev: baselineRev,
		PatchRev:    patchRev,
		Service:     ingest.ClipService(*service),
		Workload:    w,
		Impact:      rep,
		Plan:        dag,
		Result:      ev,
	})
	writeEvidence(stdout, ev)
	writeDecision(stdout, art.Coverage, art.Decision())
	if *outPath != "" {
		if err := writeEvidenceFile(*outPath, art); err != nil {
			return fmt.Errorf("cli: experiment: %w", err)
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
	refused := authRefused(ev.Steps)
	for _, s := range ev.Steps {
		writef(w, "  %-14s %s\n", s.Kind, s.Verdict)
		if s.Kind == plan.KindLatency {
			for _, lat := range s.Latency {
				writef(w, "    %-6s %-22s %s%s\n", lat.Method, clipRoute(lat.Path), formatLatency(lat), refused[lat.Method+" "+lat.Path])
			}
		}
		if s.Kind == plan.KindBehavior && len(s.Steps) > 0 {
			for _, d := range s.Steps {
				writef(w, "    %-6s %-22s %d/%d  %s%s\n", d.Method, clipRoute(d.Path), d.BaselineStatus, d.PatchStatus, d.Status, deltaNotes(d))
			}
			continue
		}
		if len(s.Notes) > 0 && s.Verdict != plan.VerdictSkipped {
			writef(w, "    notes      %s\n", joinNotes(s.Notes))
		}
	}
	for _, n := range ev.Notes {
		writef(w, "note         %s\n", n)
	}
}

// authRefused marks latency rows whose request was 401/403 on both sides:
// those samples time the auth check, not the handler.
func authRefused(results []plan.StepResult) map[string]string {
	out := map[string]string{}
	for _, r := range results {
		for _, d := range r.Steps {
			if d.AuthRejected() {
				out[d.Method+" "+d.Path] = "  auth_rejected"
			}
		}
	}
	return out
}

func clipRoute(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		return p[:i] + "?…"
	}
	return p
}

func deltaNotes(d replay.Delta) string {
	if len(d.Notes) == 0 {
		return ""
	}
	if d.AuthRejected() {
		return "  auth_rejected: handler did not run; check workload headers"
	}
	return "  " + strings.Join(d.Notes, ",")
}

func writeCoverage(w io.Writer, c *evidence.Coverage) {
	writef(w, "coverage   impacted=%d  exercised=%d  missed=%d\n", len(c.Impacted), len(c.Exercised), len(c.Missed))
	for _, r := range c.Exercised {
		writef(w, "  hit      %s\n", r)
	}
	rejected := map[string]struct{}{}
	for _, r := range c.Rejected {
		rejected[r] = struct{}{}
	}
	for _, r := range c.Missed {
		if _, ok := rejected[r]; ok {
			writef(w, "  missed   %s  auth_rejected (401/403 on both sides; handler did not run)\n", r)
			continue
		}
		writef(w, "  missed   %s\n", r)
	}
	if len(c.Exercised) == 0 {
		writef(w, "  no impacted route got a request; validated cannot be earned\n")
	}
}

func writeDecision(w io.Writer, c *evidence.Coverage, d validate.Decision) {
	if c != nil {
		writeCoverage(w, c)
	}
	writef(w, "validated  %t\n", d.Validated)
	for _, b := range d.Blockers {
		if b.Subject == "" {
			writef(w, "blocker  %s\n", b.Code)
		} else {
			writef(w, "blocker  %s  %s\n", b.Code, b.Subject)
		}
	}
	if d.Validated {
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
