package cli

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sumedhaerram/aquila/internal/diff"
	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/locate"
	"github.com/sumedhaerram/aquila/internal/source"
)

const (
	controlPlaneModule = "github.com/sumedhaerram/aquila"

	originCwd   = "cwd"
	originAPI   = "api"
	originFiles = "files"
	originNone  = "none"
)

func analyzeDiff(ctx context.Context, api, dir string, traces int, service string, raw []byte) (impact.Report, string, error) {
	parsed, err := diff.Parse(raw)
	if err != nil {
		return impact.Report{}, "", fmt.Errorf("cli: impact: %w", err)
	}
	if g := loadTargetSource(ctx, dir); g != nil {
		rep, err := impactFromWindow(ctx, api, traces, service, g, parsed)
		return rep, originCwd, err
	}
	if controlPlaneDir(dir) {
		rep, err := fetchImpact(ctx, api, traces, service, raw)
		return rep, originAPI, err
	}
	spans, err := fetchSpans(ctx, api, traces, service)
	if err != nil {
		return impact.Report{}, "", err
	}
	rep := impact.FromDiff(parsed, spans)
	return rep, originFiles, nil
}

func loadTargetSource(ctx context.Context, dir string) *source.Graph {
	if dir == "" {
		dir = "."
	}
	g, err := source.Load(ctx, dir)
	if err != nil || g == nil {
		return nil
	}
	// Aquila's own module is the control plane. Using it would map a shop
	// diff onto aquila source instead of the server snapshot.
	if strings.TrimSuffix(g.Module(), "/") == controlPlaneModule {
		return nil
	}
	return g
}

func controlPlaneDir(dir string) bool {
	root := findGoMod(dir)
	if root == "" {
		return false
	}
	return readModulePath(root) == controlPlaneModule
}

func shopTree(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for _, p := range []string{
		filepath.Join(abs, "internal", "payment", "handler.go"),
		filepath.Join(abs, "examples", "shop", "internal", "payment", "handler.go"),
	} {
		st, err := os.Stat(p)
		if err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

// allowsShopPair is true when omitting -base/-patch may start the shop Compose pair.
// -shop is the copy source and must not license a foreign -dir.
func allowsShopPair(dir, _ string) bool {
	return shopTree(dir) || controlPlaneDir(dir)
}

func findGoMod(dir string) string {
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil {
			return abs
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return ""
		}
		abs = parent
	}
}

func readModulePath(dir string) string {
	f, err := os.Open(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

func impactFromWindow(ctx context.Context, api string, traces int, service string, g *source.Graph, parsed diff.Diff) (impact.Report, error) {
	spans, err := fetchSpans(ctx, api, traces, service)
	if err != nil {
		return impact.Report{}, err
	}
	return impact.Analyze(g, parsed, locate.Bind(g, spans), graph.Build(spans)), nil
}

func windowQuery(traces int, service string) string {
	q := url.Values{}
	q.Set("traces", strconv.Itoa(clipTraces(traces)))
	if s := ingest.ClipService(service); s != "" {
		q.Set("service", s)
	}
	return "?" + q.Encode()
}

func fetchSpans(ctx context.Context, api string, traces int, service string) ([]ingest.Span, error) {
	c, err := newClient(api)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Spans []ingest.Span `json:"spans"`
	}
	if err := c.getJSON(ctx, "/v1/spans"+windowQuery(traces, service), &payload); err != nil {
		return nil, err
	}
	if payload.Spans == nil {
		payload.Spans = []ingest.Span{}
	}
	if s := ingest.ClipService(service); s != "" {
		payload.Spans = ingest.SelectWindow(payload.Spans, traces, s)
	}
	return payload.Spans, nil
}

func reportFromLocal(g *source.Graph, spans []ingest.Span, raw []byte) (impact.Report, error) {
	parsed, err := diff.Parse(raw)
	if err != nil {
		return impact.Report{}, fmt.Errorf("cli: impact: %w", err)
	}
	return impact.Analyze(g, parsed, locate.Bind(g, spans), graph.Build(spans)), nil
}

func uniqueServerRoutes(spans []ingest.Span) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range spans {
		if strings.EqualFold(strings.TrimSpace(s.Kind), "internal") ||
			strings.EqualFold(strings.TrimSpace(s.Kind), "client") ||
			strings.EqualFold(strings.TrimSpace(s.Kind), "producer") ||
			strings.EqualFold(strings.TrimSpace(s.Kind), "consumer") {
			continue
		}
		method := strings.ToUpper(strings.TrimSpace(s.HTTPMethod))
		route := strings.TrimSpace(s.HTTPRoute)
		if method == "" || route == "" {
			continue
		}
		svc := strings.TrimSpace(s.ServiceName)
		key := svc + " " + method + " " + route
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		line := method + " " + route
		if svc != "" {
			line = svc + "  " + line
		}
		out = append(out, line)
		if len(out) >= maxPrintBindings {
			break
		}
	}
	return out
}
