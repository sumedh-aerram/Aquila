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
  impact      Blast radius of a unified diff (direct, likely, runtime, unobserved)
  env         Isolated baseline and patch shop trees from a diff (does not start)
  replay      Same workload against two gateways; match/differ/incomplete
  fault       Loopback reverse proxy that delays or injects a status
  plan        Minimum useful experiment DAG from impact (does not run it)
  experiment  Plan plus executed replay/latency against two gateways
  version     Print the Aquila version
  help        Show this help

Flags:
  -api string     control-plane URL (default http://127.0.0.1:8080, or AQUILA_API_URL)
  -traces int     observe/impact/plan/experiment trace window (default 20, max 200)
  -f path         impact/env/plan/experiment: diff file (default stdin)
  -shop path      env: shop module (default examples/shop)
  -out path       env: pair parent directory (default out/env)
  -base url       replay/experiment: baseline gateway
  -patch url      replay/experiment: patch gateway
  -fixture        replay/experiment: shop smoke requests (not span-derived)
  -n int          replay: repeats (default 1). experiment: latency repeats (0 = plan default 20)
  -target url     fault: upstream gateway
  -listen addr    fault: loopback listen (default 127.0.0.1:19080)
  -delay dur      fault: injected delay before proxy or status
  -status int     fault: if set, return this status and do not proxy

impact reads git diff on stdin. It does not apply the patch. env copies the shop
and applies the diff only to patch. replay hits -base and -patch; match is not
a pass. p95 is withheld unless n>=20. No regression threshold. fault listens on
loopback only; an injected 502 is a probe, not a pass. plan names env, behavior,
latency, and (when runtime paths exist) an operator fault. experiment executes
behavior and latency only; skipped operator steps are not a pass. There is no
ask command.
`
}

const (
	defaultCommandTimeout = 15 * time.Second
	replayStepBudget      = 45 * time.Second
	maxReplayTimeout      = 8 * time.Minute
)

func commandTimeout(args []string) time.Duration {
	if len(args) == 0 {
		return defaultCommandTimeout
	}
	switch args[0] {
	case "fault":
		return 0
	case "replay":
		n := flagN(args[1:], 1)
		return capReplayTimeout(n)
	case "experiment":
		n := flagN(args[1:], 20)
		return capReplayTimeout(n)
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
