package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/sumedhaerram/aquila/internal/diff"
	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/investigate"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/rewrite"
)

// RunWorkflow investigates, plans, prints a candidate, and optionally executes
// the experiment. It does not call an LLM.
func RunWorkflow(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	api := fs.String("api", envAPI(), "control-plane base URL")
	traces := fs.Int("traces", defaultTraces, "trace window (max 200)")
	dir := fs.String("dir", ".", "module under change (default cwd)")
	service := fs.String("service", "", "OTEL service.name; scopes the trace window")
	file := fs.String("f", "", "diff file (default stdin or git)")
	base := fs.String("base", "", "baseline gateway URL")
	patch := fs.String("patch", "", "patch gateway URL")
	fixture := fs.Bool("fixture", false, "use shop smoke fixture instead of span routes")
	workload := fs.String("workload", "", "operator workload JSON")
	n := fs.Int("n", 0, "latency repeats (0 uses the plan default)")
	smoke := fs.Bool("smoke", false, "latency n=1; cannot validate")
	outPath := fs.String("out", "", "write evidence JSON")
	shop := fs.String("shop", defaultShop(), "shop module when starting a local pair")
	pairDir := fs.String("pair", filepath.Join("out", "env"), "parent directory for a local pair")
	apply := fs.Bool("apply", false, "write the candidate onto the module tree (not a pass)")
	planOnly := fs.Bool("plan-only", false, "investigate, plan, and candidate only")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: run: %w", err)
	}
	q := strings.TrimSpace(strings.Join(fs.Args(), " "))
	path, err := diffPath(*file, nil)
	if err != nil {
		return fmt.Errorf("cli: run: %w", err)
	}
	raw, _, diffErr := resolveDiff(ctx, stdin, path, *dir)
	if diffErr != nil {
		raw = nil
	}
	if q == "" && len(raw) == 0 {
		return fmt.Errorf("cli: run: question or diff required")
	}

	c, err := newClient(*api)
	if err != nil {
		return err
	}
	tn := clipTraces(*traces)
	query := windowQuery(tn, *service)
	var rt graph.Snapshot
	if err := c.getJSON(ctx, "/v1/graph"+query, &rt); err != nil {
		return err
	}
	_, loc, origin, spans, _, locErr := observeJoin(ctx, c, *api, *dir, tn, *service, query)
	if locErr != nil && !unavailable(locErr) {
		return locErr
	}
	if ingest.ClipService(*service) != "" {
		rt = graph.Build(spans)
	}
	in := investigate.Input{
		Question: q,
		Origin:   origin,
		Services: serviceNames(rt),
		Hops:     hopsFrom(rt),
		Paths:    pathsFrom(rt),
		Binds:    bindsFrom(loc),
		Routes:   routeFacts(spans),
	}
	if len(raw) > 0 {
		rep, _, err := analyzeDiff(ctx, *api, *dir, tn, *service, raw)
		if err != nil {
			if q == "" {
				return err
			}
		} else {
			in.Impact = &rep
		}
	}
	if q == "" && in.Impact == nil {
		return fmt.Errorf("cli: run: question or diff required")
	}
	askRep, err := investigate.Search(in)
	if err != nil {
		return err
	}
	writeAsk(stdout, askRep)
	if len(raw) == 0 {
		return nil
	}

	rep, _, err := analyzeDiff(ctx, *api, *dir, tn, *service, raw)
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
	mod := patchModule(ctx, *dir)
	dag = plan.WithTests(dag, mod, rep.Files)
	writePlan(stdout, dag)

	cand, err := rewrite.Candidates(mod, rep.Files)
	if err != nil {
		writef(stdout, "patch    no candidate\n")
	} else {
		_, _ = stdout.Write(cand)
		if *apply {
			if err := diff.Apply(mod, cand); err != nil {
				return fmt.Errorf("cli: run: %w", err)
			}
			writef(stdout, "applied. not validated.\n")
		} else {
			writef(stdout, "not applied. not validated. candidate only.\n")
		}
	}
	if *planOnly {
		return nil
	}

	expArgs := []string{
		"-api", *api,
		"-dir", *dir,
		"-traces", fmt.Sprintf("%d", tn),
		"-shop", *shop,
		"-pair", *pairDir,
	}
	if *service != "" {
		expArgs = append(expArgs, "-service", *service)
	}
	if *fixture {
		expArgs = append(expArgs, "-fixture")
	}
	if *workload != "" {
		expArgs = append(expArgs, "-workload", *workload)
	}
	if *n > 0 {
		expArgs = append(expArgs, "-n", fmt.Sprintf("%d", *n))
	}
	if *smoke {
		expArgs = append(expArgs, "-smoke")
	}
	if *outPath != "" {
		expArgs = append(expArgs, "-out", *outPath)
	}
	if *base != "" || *patch != "" {
		expArgs = append(expArgs, "-base", *base, "-patch", *patch)
	}
	return RunExperiment(ctx, expArgs, bytes.NewReader(raw), stdout)
}
