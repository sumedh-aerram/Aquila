package main

import (
	"fmt"
	"io"
	"os"

	"github.com/sumedhaerram/aquila/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage(os.Stdout)
		return nil
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Printf("aquila %s\n", version.Version)
		return nil
	case "help", "--help", "-h":
		printUsage(os.Stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage())
	}
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, usage())
}

func usage() string {
	return `Aquila — production-aware agentic software engineer

Usage:
  aquila <command>

Commands:
  version    Print the Aquila version
  help       Show this help

Runtime investigation, impact analysis, and experiment commands land in later phases.
See README.md for local setup.
`
}
