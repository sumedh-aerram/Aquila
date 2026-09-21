package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/sumedhaerram/aquila/internal/source"
)

func main() {
	dir := flag.String("dir", "", "Go module directory to index")
	out := flag.String("out", "", "snapshot JSON path")
	timeout := flag.Duration("timeout", 2*time.Minute, "load timeout")
	flag.Parse()
	if *dir == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "sourceindex: -dir and -out are required")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	g, err := source.Load(ctx, *dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sourceindex: load: %v\n", err)
		os.Exit(1)
	}
	if err := g.WriteFile(*out); err != nil {
		fmt.Fprintf(os.Stderr, "sourceindex: write: %v\n", err)
		os.Exit(1)
	}
}
