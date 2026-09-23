package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/sumedhaerram/aquila/internal/diff"
	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/locate"
	"github.com/sumedhaerram/aquila/internal/source"
)

const controlPlaneModule = "github.com/sumedhaerram/aquila"

func analyzeDiff(ctx context.Context, api, dir string, traces int, raw []byte) (impact.Report, error) {
	if g := loadTargetSource(ctx, dir); g != nil {
		return impactFromWindow(ctx, api, traces, g, raw)
	}
	return fetchImpact(ctx, api, traces, raw)
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

func impactFromWindow(ctx context.Context, api string, traces int, g *source.Graph, raw []byte) (impact.Report, error) {
	c, err := newClient(api)
	if err != nil {
		return impact.Report{}, err
	}
	var payload struct {
		Spans []ingest.Span `json:"spans"`
	}
	path := "/v1/spans?traces=" + strconv.Itoa(clipTraces(traces))
	if err := c.getJSON(ctx, path, &payload); err != nil {
		return impact.Report{}, err
	}
	return reportFromLocal(g, payload.Spans, raw)
}

func reportFromLocal(g *source.Graph, spans []ingest.Span, raw []byte) (impact.Report, error) {
	parsed, err := diff.Parse(raw)
	if err != nil {
		return impact.Report{}, fmt.Errorf("cli: impact: %w", err)
	}
	return impact.Analyze(g, parsed, locate.Bind(g, spans), graph.Build(spans)), nil
}
