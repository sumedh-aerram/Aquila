package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sumedhaerram/aquila/internal/cli"
	"github.com/sumedhaerram/aquila/internal/version"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stdout)
		return nil
	}
	ctx := context.Background()
	cancel := func() {}
	if d := commandTimeout(args); d > 0 {
		ctx, cancel = context.WithTimeout(ctx, d)
	}
	defer cancel()
	switch args[0] {
	case "version", "--version", "-v":
		_, _ = fmt.Fprintf(stdout, "aquila %s\n", version.Version)
		return nil
	case "help", "--help", "-h":
		printUsage(stdout)
		return nil
	case "status":
		return cli.RunStatus(ctx, args[1:], stdout)
	case "observe":
		return cli.RunObserve(ctx, args[1:], stdout, stderr)
	case "impact":
		return cli.RunImpact(ctx, args[1:], os.Stdin, stdout)
	case "env":
		return cli.RunEnv(ctx, args[1:], os.Stdin, stdout)
	case "replay":
		return cli.RunReplay(ctx, args[1:], stdout)
	case "fault":
		return cli.RunFault(ctx, args[1:], stdout)
	case "plan":
		return cli.RunPlan(ctx, args[1:], os.Stdin, stdout)
	case "experiment":
		return cli.RunExperiment(ctx, args[1:], os.Stdin, stdout)
	case "report":
		return cli.RunReport(ctx, args[1:], stdout)
	case "runs":
		return cli.RunRuns(ctx, args[1:], stdout)
	case "ask":
		return cli.RunAsk(ctx, args[1:], os.Stdin, stdout, stderr)
	case "run":
		return cli.RunWorkflow(ctx, args[1:], os.Stdin, stdout)
	case "patch":
		return cli.RunPatch(ctx, args[1:], os.Stdin, stdout)
	case "job":
		return cli.RunJob(ctx, args[1:], os.Stdin, stdout)
	case "jobs":
		return cli.RunJobs(ctx, args[1:], stdout)
	case "attaches":
		return cli.RunAttaches(ctx, args[1:], stdout)
	case "worker":
		return cli.RunWorker(ctx, args[1:], stdout)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage())
	}
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, usage())
}

func usage() string {
	return `Aquila — control plane for changing distributed backends safely

Usage:
  aquila <command> [flags]

Commands:
  status      Control-plane health, ready, version
  observe     Runtime hops, source summary, span-to-source binds
  impact      Blast radius of local git changes (or a piped unified diff)
  env         Isolated baseline and patch shop trees from a diff (does not start)
  replay      Same workload against two gateways; match/differ/incomplete
  fault       Loopback reverse proxy that delays or injects a status
  plan        Minimum useful experiment DAG from impact (does not run it)
  experiment  Plan plus executed replay/latency; shop pair only on shop/control-plane -dir
  report      Markdown from a saved evidence JSON file (does not re-run)
  runs        List, show, or import persisted experiment evidence
  ask         Observed facts matching a question (not a patch, not validated)
  run         Investigate, plan, candidate, and experiment (no LLM)
  patch       Candidate shop rewrite from impact (optional -apply; not a pass)
  job         Enqueue a plan as a durable DAG for a worker (does not start compose)
  jobs        List, show, or cancel persisted jobs
  attaches    OTEL service.name values in the span store (not tenants)
  worker      Single gRPC worker: lease one executable task and commit
  version     Print the Aquila version
  help        Show this help

Flags:
  -api string     control-plane URL (default http://127.0.0.1:8080, or AQUILA_API_URL)
  -traces int     observe/impact/plan/experiment/replay trace window (default 20, max 200)
  -f path         impact/env/plan/experiment: diff file (default: git diff HEAD in -dir, else stdin)
  -shop path      env/experiment: shop module (default examples/shop)
  -out path       env: pair parent (default out/env); experiment: evidence JSON; report: optional markdown
  -pair path      experiment: pair parent when omitting -base/-patch (default out/env)
  -dir path       observe/impact/plan/experiment/replay: module (and git tree) under change (default .)
  -service name   observe/impact/ask/replay/experiment/job/jobs/runs: OTEL service.name
  -job id         worker: lease only READY tasks for this job
  -base url       replay/experiment: baseline gateway
  -patch url      replay/experiment: patch gateway
  -fixture        replay/experiment: shop smoke requests (not span-derived)
  -workload path  replay/experiment: operator JSON steps (bodies never from traces)
  -smoke          experiment/job: latency n=1; cannot validate
  -apply          patch/run: write the candidate onto the module tree (not a pass)
  -plan-only      run: investigate, plan, and candidate only
  -n int          replay: repeats (default 1). experiment/run: latency repeats (0 = plan default 20)
  -target url     fault: upstream gateway
  -listen addr    fault: loopback listen (default 127.0.0.1:19080)
  -delay dur      fault: injected delay before proxy or status
  -status int     fault: if set, return this status and do not proxy

impact reads git diff HEAD in -dir when stdin is empty (local IDE changes).
Pipe a unified diff or pass -f to override. It does not apply the patch.
Standing in this repo falls back to POST /v1/impact so a shop snapshot still
maps shop diffs. A directory without go.mod is file-level impact (origin=files),
not the shop graph. Other Go modules are loaded from -dir. observe prints an
edit section for those dirty lines when they join a function or bound route.
ask with no question cites that same worktree. env copies the shop
and applies the diff only to patch. replay hits -base and -patch using
GET/HEAD/OPTIONS from server spans (no bodies). When -dir has a local edit
that joins a replayable route, those steps are preferred (provenance
changed_lines); otherwise the window is used. A window with more than one
service refuses span-derived replay unless -service is set. -workload is an operator JSON
file for POST and friends. -fixture is shop smoke including POST /checkout.
match is not a pass. p95 is withheld unless n>=20. No regression threshold.
fault listens on loopback only; an injected 502 is a probe, not a pass. plan
names env, behavior, latency, and (when runtime paths exist) concurrency plus
a one-sided 502 probe. experiment executes those steps and go test of impact
packages when -dir is a Go module. omitting -base and -patch prepares the shop
pair only when -dir is the shop or this control-plane repo. a foreign or python
checkout must pass both gateway URLs; it will not start shop compose. -fixture
is shop smoke and is rejected off the shop. -smoke sets latency n=1 and cannot
validate. experiment -out writes evidence JSON. validated is earned only when
required executable steps succeed on a clean recorded revision (env, behavior,
latency n>=20, concurrency when planned, tests when planned). match is not a
pass. experiment also POSTs that artifact to /v1/runs when the API is up; a
missing store is unrecorded, not a pass. report renders a file as Markdown
without hitting gateways. runs lists stored evidence, shows one id, or imports
-f. attaches lists service.name values in the store. ask joins question tokens
onto hops, binds, and impact and labels provenance. run is ask plus plan,
candidate, and experiment. patch emits the first matching shop rewrite (D1-D6)
and does not write the tree unless -apply. job records env READY first;
overlapping jobs on the same gateway host are refused. jobs cancel <id> stops
remaining READY tasks. a 10m deadline fails unfinished tasks. worker leases one
READY task over gRPC, runs it, and commits with an attempt id. worker -job pins
the lease to that DAG. a stale attempt cannot commit. AQUILA_API_TOKEN, when
set, is sent as Authorization: Bearer. There is no LLM.
`
}

const (
	defaultCommandTimeout = 15 * time.Second
	replayStepBudget      = 45 * time.Second
	maxReplayTimeout      = 8 * time.Minute
	localEnvBudget        = 15 * time.Minute
	maxLocalTimeout       = 25 * time.Minute
)

func commandTimeout(args []string) time.Duration {
	if len(args) == 0 {
		return defaultCommandTimeout
	}
	switch args[0] {
	case "fault":
		return 0
	case "worker":
		return 0
	case "job":
		return time.Minute
	case "replay":
		n := flagN(args[1:], 1)
		return capReplayTimeout(n)
	case "experiment":
		n := flagN(args[1:], 20)
		d := capReplayTimeout(n)
		if !hasFlag(args[1:], "-base") && !hasFlag(args[1:], "-patch") {
			d += localEnvBudget
			if d > maxLocalTimeout {
				return maxLocalTimeout
			}
		}
		return d
	case "run":
		n := flagN(args[1:], 20)
		d := capReplayTimeout(n)
		if hasFlag(args[1:], "-plan-only") {
			return defaultCommandTimeout
		}
		if !hasFlag(args[1:], "-base") && !hasFlag(args[1:], "-patch") {
			d += localEnvBudget
			if d > maxLocalTimeout {
				return maxLocalTimeout
			}
		}
		return d
	default:
		return defaultCommandTimeout
	}
}

func capReplayTimeout(n int) time.Duration {
	d := time.Duration(n) * replayStepBudget
	if d > maxReplayTimeout {
		return maxReplayTimeout
	}
	return d
}

func flagN(args []string, def int) int {
	n := def
	for i := 0; i < len(args); i++ {
		a := args[i]
		var raw string
		switch {
		case a == "-n" && i+1 < len(args):
			raw = args[i+1]
			i++
		case strings.HasPrefix(a, "-n="):
			raw = strings.TrimPrefix(a, "-n=")
		default:
			continue
		}
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			continue
		}
		n = v
	}
	if n > 100 {
		n = 100
	}
	return n
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name || strings.HasPrefix(a, name+"=") {
			return true
		}
	}
	return false
}
