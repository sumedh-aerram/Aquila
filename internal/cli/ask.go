package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
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
	file := fs.String("f", "", "optional unified diff to include impact facts")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: ask: %w", err)
	}
	q := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if q == "" {
		return fmt.Errorf("cli: ask: question required")
	}

	c, err := newClient(*api)
	if err != nil {
		return err
	}
	n := clipTraces(*traces)
	query := "?traces=" + strconv.Itoa(n)

	var rt graph.Snapshot
	if err := c.getJSON(ctx, "/v1/graph"+query, &rt); err != nil {
		return err
	}
	_, loc, origin, spans, _, locErr := observeJoin(ctx, c, *api, *dir, n, query)
	if locErr != nil && !unavailable(locErr) {
		return locErr
	}

	in := investigate.Input{
		Question: q,
		Origin:   origin,
		Services: serviceNames(rt),
		Hops:     hopLines(rt),
		Paths:    pathLines(rt),
		Binds:    bindLines(loc),
		Routes:   routeFacts(spans),
	}
	if *file != "" {
		raw, err := slurpDiff(stdin, *file)
		if err != nil {
			return fmt.Errorf("cli: ask: %w", err)
		}
		rep, _, err := analyzeDiff(ctx, *api, *dir, n, raw)
		if err != nil {
			return err
		}
		in.Impact = &rep
	} else if origin != originAPI {
		raw, _, err := resolveDiff(ctx, strings.NewReader(""), "", *dir)
		if err == nil && len(raw) > 0 {
			rep, _, err := analyzeDiff(ctx, *api, *dir, n, raw)
			if err != nil {
				return err
			}
			in.Impact = &rep
		}
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

func hopLines(rt graph.Snapshot) []string {
	out := make([]string, 0, len(rt.Edges))
	for _, e := range rt.Edges {
		out = append(out, e.From+" -> "+e.To)
	}
	return out
}

func pathLines(rt graph.Snapshot) []string {
	out := make([]string, 0, len(rt.Paths))
	for _, p := range rt.Paths {
		out = append(out, strings.Join(p.Services, " -> "))
	}
	return out
}

func bindLines(loc locate.Snapshot) []string {
	out := make([]string, 0, len(loc.Bindings))
	for _, b := range loc.Bindings {
		out = append(out, strings.TrimSpace(b.ServiceName+" "+b.SourceName+" "+b.File))
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
