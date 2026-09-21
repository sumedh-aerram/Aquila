package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/locate"
	"github.com/sumedhaerram/aquila/internal/source"
)

const (
	maxPrintEdges    = 20
	maxPrintPaths    = 10
	maxPrintBindings = 15
	maxPrintServices = 20
)

// RunObserve prints runtime topology, source summary, and span-to-source bindings.
func RunObserve(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("observe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	api := fs.String("api", envAPI(), "control-plane base URL")
	traces := fs.Int("traces", defaultTraces, "trace window (max 200)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: observe: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("cli: observe: unexpected argument %q", fs.Arg(0))
	}
	c, err := newClient(*api)
	if err != nil {
		return err
	}
	n := clipTraces(*traces)
	q := "?traces=" + strconv.Itoa(n)

	var rt graph.Snapshot
	if err := c.getJSON(ctx, "/v1/graph"+q, &rt); err != nil {
		return err
	}

	var src source.Snapshot
	srcErr := c.getJSON(ctx, "/v1/source", &src)
	if srcErr != nil && !unavailable(srcErr) {
		return srcErr
	}

	var loc locate.Snapshot
	locErr := c.getJSON(ctx, "/v1/locate"+q, &loc)
	if locErr != nil && !unavailable(locErr) {
		return locErr
	}

	writef(stdout, "api  %s  traces=%d\n\n", c.base, n)
	writeRuntime(stdout, rt)
	writef(stdout, "\n")
	if srcErr != nil {
		writef(stdout, "source  unavailable\n")
		writef(stderr, "observe: source: %v\n", srcErr)
	} else {
		writeSource(stdout, src)
	}
	writef(stdout, "\n")
	if locErr != nil {
		writef(stdout, "locate  unavailable\n")
		writef(stderr, "observe: locate: %v\n", locErr)
	} else {
		writeLocate(stdout, loc)
	}
	return nil
}

func writeRuntime(w io.Writer, rt graph.Snapshot) {
	writef(w, "runtime  provenance=observed_parent\n")
	writef(w, "  traces %d  spans %d  services %d  edges %d  paths %d\n",
		rt.TraceCount, rt.SpanCount, len(rt.Services), len(rt.Edges), len(rt.Paths))
	if len(rt.Services) == 0 {
		writef(w, "  (no spans in window)\n")
		return
	}
	names := make([]string, 0, len(rt.Services))
	for i, s := range rt.Services {
		if i >= maxPrintServices {
			names = append(names, "...")
			break
		}
		names = append(names, s.Name)
	}
	writef(w, "  services  %s\n", strings.Join(names, ", "))
	limit := len(rt.Edges)
	if limit > maxPrintEdges {
		limit = maxPrintEdges
	}
	for i := 0; i < limit; i++ {
		e := rt.Edges[i]
		writef(w, "  hop  %s -> %s  n=%d  %s\n", e.From, e.To, e.Count, e.Provenance)
	}
	if len(rt.Edges) > maxPrintEdges {
		writef(w, "  hops truncated (%d more)\n", len(rt.Edges)-maxPrintEdges)
	}
	plimit := len(rt.Paths)
	if plimit > maxPrintPaths {
		plimit = maxPrintPaths
	}
	for i := 0; i < plimit; i++ {
		p := rt.Paths[i]
		writef(w, "  path  %s  traces=%d  %s\n", strings.Join(p.Services, " -> "), p.Traces, p.Provenance)
	}
}

func writeSource(w io.Writer, src source.Snapshot) {
	var pkgs, files, funcs int
	for _, n := range src.Nodes {
		switch n.Kind {
		case source.KindPackage:
			pkgs++
		case source.KindFile:
			files++
		case source.KindFunction:
			funcs++
		}
	}
	writef(w, "source  module=%s\n", emptyDash(src.Module))
	writef(w, "  packages %d  files %d  functions %d  edges %d\n", pkgs, files, funcs, len(src.Edges))
}

func writeLocate(w io.Writer, loc locate.Snapshot) {
	writef(w, "locate  provenance=code_attrs\n")
	writef(w, "  spans %d  bound %d  unmapped %d\n", loc.SpanCount, loc.Bound, loc.UnmappedCount)
	limit := len(loc.Bindings)
	if limit > maxPrintBindings {
		limit = maxPrintBindings
	}
	for i := 0; i < limit; i++ {
		b := loc.Bindings[i]
		writef(w, "  bind  %s  %s  %s:%d\n", b.ServiceName, b.SourceName, b.File, b.Line)
	}
	if len(loc.Bindings) > maxPrintBindings {
		writef(w, "  binds truncated (%d more)\n", len(loc.Bindings)-maxPrintBindings)
	}
	if loc.UnmappedCount == 0 {
		return
	}
	counts := map[string]int{}
	for _, u := range loc.Unmapped {
		counts[u.Reason]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	writef(w, "  unmapped reasons")
	if loc.UnmappedCount > len(loc.Unmapped) {
		writef(w, " (sample of %d)", len(loc.Unmapped))
	}
	writef(w, "\n")
	for _, k := range keys {
		writef(w, "    %s  %d\n", k, counts[k])
	}
}
