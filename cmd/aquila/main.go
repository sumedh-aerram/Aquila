package main

import (
	"context"
	"fmt"
	"io"
	"os"
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
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout(args))
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
  status     Control-plane health, ready, version
  observe    Runtime hops, source summary, span-to-source binds
  impact     Blast radius of a unified diff (direct, likely, runtime, unobserved)
  env        Isolated baseline and patch shop trees from a diff (does not start)
  replay     Same workload against two gateways; match/differ/incomplete
  version    Print the Aquila version
  help       Show this help

Flags:
  -api string     control-plane URL (default http://127.0.0.1:8080, or AQUILA_API_URL)
  -traces int     observe/impact trace window (default 20, max 200)
  -f path         impact/env: diff file (default stdin)
  -shop path      env: shop module (default examples/shop)
  -out path       env: pair parent directory (default out/env)
  -base url       replay: baseline gateway
  -patch url      replay: patch gateway
  -fixture        replay: shop smoke requests (not span-derived)

impact reads git diff on stdin. It does not apply the patch. env copies the shop
and applies the diff only to patch. replay hits -base and -patch; match is not
a pass. Durations are observations, not a regression claim.
`
}

func commandTimeout(args []string) time.Duration {
	if len(args) > 0 && args[0] == "replay" {
		return 45 * time.Second
	}
	return 15 * time.Second
}
