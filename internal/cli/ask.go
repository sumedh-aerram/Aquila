package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/investigate"
	"github.com/sumedhaerram/aquila/internal/locate"
)

// RunAsk prints observed facts matching a question. It does not patch or validate.
func RunAsk(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	api := fs.String("api", envAPI(), "control-plane base URL")
	traces := fs.Int("traces", defaultTraces, "trace window (max 200)")
	dir := fs.String("dir", ".", "module under change (default cwd)")
	service := fs.String("service", "", "OTEL service.name; scopes the trace window")
	file := fs.String("f", "", "optional unified diff to include impact facts")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: ask: %w", err)
	}
	q := strings.TrimSpace(strings.Join(fs.Args(), " "))
	var diffRaw []byte
	if *file != "" {
		raw, err := slurpDiff(stdin, *file)
		if err != nil {
			return fmt.Errorf("cli: ask: %w", err)
		}
		diffRaw = raw
	} else {
		if raw, _, err := resolveDiff(ctx, strings.NewReader(""), "", *dir); err == nil {
			diffRaw = raw
		}
	}
	if q == "" && len(diffRaw) == 0 {
		return fmt.Errorf("cli: ask: question required")
	}

	c, err := newClient(*api)
	if err != nil {
		return err
	}
	n := clipTraces(*traces)
	query := windowQuery(n, *service)

	var rt graph.Snapshot
	if err := c.getJSON(ctx, "/v1/graph"+query, &rt); err != nil {
		return err
	}
	_, loc, origin, spans, _, locErr := observeJoin(ctx, c, *api, *dir, n, *service, query)
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
	if len(diffRaw) > 0 {
		rep, _, err := analyzeDiff(ctx, *api, *dir, n, *service, diffRaw)
		if err != nil {
			if q == "" {
				return err
			}
		} else {
			in.Impact = &rep
		}
	}
	if q == "" && in.Impact == nil {
		return fmt.Errorf("cli: ask: question required")
	}

	rep, err := investigate.Search(in)
	if err != nil {
		return err
	}
	writeAsk(stdout, rep)
	return nil
}

func writeAsk(w io.Writer, rep investigate.Report) {
	writef(w, "ask      %s\n", rep.Question)
	if rep.Origin != "" {
		writef(w, "origin   %s\n", rep.Origin)
	}
	writef(w, "hits     %d\n", len(rep.Hits))
	for _, h := range rep.Hits {
		writef(w, "  %s\n", h)
	}
	for _, n := range rep.Notes {
		writef(w, "note     %s\n", n)
	}
}

func serviceNames(rt graph.Snapshot) []string {
	out := make([]string, 0, len(rt.Services))
	for _, s := range rt.Services {
		out = append(out, s.Name)
	}
	return out
}

func hopsFrom(rt graph.Snapshot) []investigate.Hop {
	out := make([]investigate.Hop, 0, len(rt.Edges))
	for _, e := range rt.Edges {
		out = append(out, investigate.Hop{From: e.From, To: e.To, Provenance: e.Provenance})
	}
	return out
}

func pathsFrom(rt graph.Snapshot) []investigate.Path {
	out := make([]investigate.Path, 0, len(rt.Paths))
	for _, p := range rt.Paths {
		out = append(out, investigate.Path{Services: p.Services, Provenance: p.Provenance})
	}
	return out
}

func bindsFrom(loc locate.Snapshot) []investigate.Bind {
	out := make([]investigate.Bind, 0, len(loc.Bindings))
	for _, b := range loc.Bindings {
		route := ""
		if r := strings.TrimSpace(b.HTTPMethod + " " + b.HTTPRoute); strings.Contains(r, "/") {
			route = strings.ToUpper(strings.TrimSpace(b.HTTPMethod)) + " " + strings.TrimSpace(b.HTTPRoute)
		}
		out = append(out, investigate.Bind{
			Service:    b.ServiceName,
			Name:       b.SourceName,
			File:       b.File,
			Route:      route,
			Provenance: b.Provenance,
			Line:       b.Line,
		})
	}
	return out
}

func routeFacts(spans []ingest.Span) []string {
	out := make([]string, 0, len(spans))
	for _, r := range uniqueServerRoutes(spans) {
		out = append(out, "route "+r)
	}
	return out
}
