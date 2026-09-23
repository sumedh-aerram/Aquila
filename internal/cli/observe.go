package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/sumedhaerram/aquila/internal/diff"
	"github.com/sumedhaerram/aquila/internal/gitrev"
	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/ingest"
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
	dir := fs.String("dir", ".", "module under change (default cwd)")
	service := fs.String("service", "", "OTEL service.name; scopes the trace window")
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
	q := windowQuery(n, *service)

	var rt graph.Snapshot
	if err := c.getJSON(ctx, "/v1/graph"+q, &rt); err != nil {
		return err
	}

	src, loc, origin, spans, srcErr, locErr := observeJoin(ctx, c, *api, *dir, n, *service, q)
	if srcErr != nil && !unavailable(srcErr) {
		return srcErr
	}
	if locErr != nil && !unavailable(locErr) {
		return locErr
	}
	if ingest.ClipService(*service) != "" {
		rt = graph.Build(spans)
	}

	writef(stdout, "api  %s  traces=%d\n\n", c.base, n)
	writeAttach(stdout, *service, spans)
	writeWorktree(stdout, ctx, *dir)
	writeLocalEdit(stdout, ctx, *dir, origin, loc, spans)
	writeRuntime(stdout, rt)
	writeRoutes(stdout, spans)
	writef(stdout, "\n")
	if origin == originNone {
		writef(stdout, "source  not a Go module  origin=%s\n", origin)
		writef(stdout, "  typed locate and callers need go.mod in -dir\n")
	} else if srcErr != nil {
		writef(stdout, "source  unavailable\n")
		writef(stderr, "observe: source: %v\n", srcErr)
	} else {
		writeSource(stdout, src, origin)
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

func observeJoin(ctx context.Context, c *Client, api, dir string, traces int, service, q string) (src source.Snapshot, loc locate.Snapshot, origin string, spans []ingest.Span, srcErr, locErr error) {
	if g := loadTargetSource(ctx, dir); g != nil {
		origin = originCwd
		src = g.Snapshot()
		spans, locErr = fetchSpans(ctx, api, traces, service)
		if locErr != nil {
			return src, loc, origin, spans, srcErr, locErr
		}
		loc = locate.Bind(g, spans)
		return src, loc, origin, spans, nil, nil
	}
	if controlPlaneDir(dir) {
		origin = originAPI
		srcErr = c.getJSON(ctx, "/v1/source", &src)
		if srcErr != nil && !unavailable(srcErr) {
			return src, loc, origin, spans, srcErr, nil
		}
		locErr = c.getJSON(ctx, "/v1/locate"+q, &loc)
		if locErr != nil && !unavailable(locErr) {
			return src, loc, origin, spans, srcErr, locErr
		}
		spans, _ = fetchSpans(ctx, api, traces, service)
		return src, loc, origin, spans, srcErr, locErr
	}
	origin = originNone
	spans, locErr = fetchSpans(ctx, api, traces, service)
	if locErr != nil {
		return src, loc, origin, spans, srcErr, locErr
	}
	loc = locate.Bind(nil, spans)
	return src, loc, origin, spans, nil, nil
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

func writeSource(w io.Writer, src source.Snapshot, origin string) {
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
	writef(w, "source  module=%s  origin=%s\n", emptyDash(src.Module), origin)
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

func writeAttach(w io.Writer, service string, spans []ingest.Span) {
	svc := ingest.ClipService(service)
	names := ingest.UniqueServices(spans)
	switch {
	case svc != "":
		writef(w, "attach  service=%s\n", svc)
	case len(names) > 1:
		writef(w, "attach  mixed  services=%s\n", strings.Join(names, ", "))
		writef(w, "  pass -service to keep one OTEL resource\n")
	case len(names) == 1:
		writef(w, "attach  service=%s\n", names[0])
	}
}

func writeRoutes(w io.Writer, spans []ingest.Span) {
	routes := uniqueServerRoutes(spans)
	if len(routes) == 0 {
		return
	}
	writef(w, "  routes\n")
	for _, r := range routes {
		writef(w, "    %s\n", r)
	}
}

func writeLocalEdit(w io.Writer, ctx context.Context, dir, origin string, loc locate.Snapshot, spans []ingest.Span) {
	if origin == originAPI {
		return
	}
	raw, _, err := resolveDiff(ctx, strings.NewReader(""), "", dir)
	if err != nil || len(raw) == 0 {
		return
	}
	parsed, err := diff.Parse(raw)
	if err != nil {
		return
	}
	var rep impact.Report
	if g := loadTargetSource(ctx, dir); g != nil {
		rep = impact.Analyze(g, parsed, loc, graph.Build(spans))
	} else {
		rep = impact.FromDiff(parsed, spans)
	}
	if len(rep.Direct) == 0 && len(rep.Runtime) == 0 && len(rep.Unobserved) == 0 {
		return
	}
	writef(w, "edit     origin=%s  files=%d  direct=%d  runtime=%d\n", origin, len(rep.Files), len(rep.Direct), len(rep.Runtime))
	n := 0
	for _, f := range rep.Direct {
		writeFinding(w, f)
		n++
		if n >= maxPrintBindings {
			break
		}
	}
	for _, f := range rep.Runtime {
		if n >= maxPrintBindings {
			break
		}
		writeFinding(w, f)
		n++
	}
	writef(w, "\n")
}

func writeWorktree(w io.Writer, ctx context.Context, dir string) {
	sha, dirty, err := gitrev.State(ctx, dir)
	if err != nil || sha == "" {
		return
	}
	state := "clean"
	if dirty {
		state = "dirty"
	}
	writef(w, "worktree  sha=%s  %s\n", sha, state)
	if !dirty {
		writef(w, "\n")
		return
	}
	paths, err := gitrev.ChangedPaths(ctx, dir)
	if err != nil {
		writef(w, "\n")
		return
	}
	limit := len(paths)
	if limit > 10 {
		limit = 10
	}
	for i := 0; i < limit; i++ {
		writef(w, "  %s\n", paths[i])
	}
	if len(paths) > limit {
		writef(w, "  (%d more)\n", len(paths)-limit)
	}
	writef(w, "\n")
}
