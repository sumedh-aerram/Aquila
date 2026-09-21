package source

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sumedhaerram/aquila/internal/config"
)

const maxSnapshotBytes = 20 << 20

// Open loads a source graph from a live module directory or a snapshot file.
// Dir takes precedence. Both empty returns (nil, nil).
func Open(ctx context.Context, cfg config.SourceConfig) (*Graph, error) {
	dir := strings.TrimSpace(cfg.Dir)
	snap := strings.TrimSpace(cfg.Snapshot)
	switch {
	case dir != "":
		return Load(ctx, dir)
	case snap != "":
		return ReadFile(snap)
	default:
		return nil, nil
	}
}

// ReadFile loads a source graph snapshot from path.
func ReadFile(path string) (*Graph, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("source: read snapshot %s: %w", path, err)
	}
	if len(raw) > maxSnapshotBytes {
		return nil, fmt.Errorf("source: snapshot %s exceeds %d bytes", path, maxSnapshotBytes)
	}
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, fmt.Errorf("source: parse snapshot %s: %w", path, err)
	}
	return FromSnapshot(snap)
}

// WriteFile writes a source graph snapshot to path.
func (g *Graph) WriteFile(path string) error {
	if g == nil {
		return fmt.Errorf("source: write snapshot: nil graph")
	}
	raw, err := json.MarshalIndent(g.Snapshot(), "", "  ")
	if err != nil {
		return fmt.Errorf("source: encode snapshot: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return fmt.Errorf("source: write snapshot %s: %w", path, err)
	}
	return nil
}

// FromSnapshot rebuilds indexes after decoding JSON.
func FromSnapshot(snap Snapshot) (*Graph, error) {
	if strings.TrimSpace(snap.Module) == "" {
		return nil, fmt.Errorf("source: snapshot missing module")
	}
	if snap.Nodes == nil {
		snap.Nodes = []Node{}
	}
	if snap.Edges == nil {
		snap.Edges = []Edge{}
	}
	if len(snap.Nodes) > maxNodes {
		return nil, fmt.Errorf("source: snapshot node cap %d exceeded", maxNodes)
	}
	if len(snap.Edges) > maxEdges {
		return nil, fmt.Errorf("source: snapshot edge cap %d exceeded", maxEdges)
	}
	seen := make(map[string]struct{}, len(snap.Nodes))
	for _, n := range snap.Nodes {
		if err := validateNode(n); err != nil {
			return nil, err
		}
		if _, ok := seen[n.ID]; ok {
			return nil, fmt.Errorf("source: duplicate node %s", n.ID)
		}
		seen[n.ID] = struct{}{}
	}
	for _, e := range snap.Edges {
		if err := validateEdge(e, seen); err != nil {
			return nil, err
		}
	}
	g := &Graph{module: snap.Module, nodes: snap.Nodes, edges: snap.Edges}
	g.index()
	return g, nil
}

func validateNode(n Node) error {
	if n.ID == "" {
		return fmt.Errorf("source: node missing id")
	}
	switch n.Kind {
	case KindPackage, KindFile, KindFunction:
	default:
		return fmt.Errorf("source: unknown node kind %q", n.Kind)
	}
	return nil
}

func validateEdge(e Edge, nodes map[string]struct{}) error {
	if _, ok := nodes[e.From]; !ok {
		return fmt.Errorf("source: dangling edge from %s", e.From)
	}
	if _, ok := nodes[e.To]; !ok {
		return fmt.Errorf("source: dangling edge to %s", e.To)
	}
	switch e.Kind {
	case EdgeImports, EdgeContains, EdgeCalls:
	default:
		return fmt.Errorf("source: unknown edge kind %q", e.Kind)
	}
	switch e.Provenance {
	case ProvenanceTypes, ProvenanceSyntax:
	default:
		return fmt.Errorf("source: unknown provenance %q", e.Provenance)
	}
	return nil
}
