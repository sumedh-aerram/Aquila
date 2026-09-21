package source

import (
	"sort"
)

// Snapshot vocabulary for node kinds, edge kinds, provenance, and traversal direction.
const (
	KindPackage  = "package"
	KindFile     = "file"
	KindFunction = "function"

	EdgeImports  = "imports"
	EdgeContains = "contains"
	EdgeCalls    = "calls"

	ProvenanceTypes  = "types"
	ProvenanceSyntax = "syntax"

	DirOut = "out"
	DirIn  = "in"

	maxNodes = 20_000
	maxEdges = 80_000
)

// Node is a package, file, or function in a loaded Go module.
type Node struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	Pkg  string `json:"pkg,omitempty"`
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
}

// Edge is a directed relation between two nodes.
type Edge struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Kind       string `json:"kind"`
	Provenance string `json:"provenance"`
}

// Neighbor is one hop from a node, used to traverse the source graph.
type Neighbor struct {
	Node      Node   `json:"node"`
	Kind      string `json:"kind"`
	Direction string `json:"direction"`
}

// Snapshot is the JSON form of a source graph. It does not include file bodies.
type Snapshot struct {
	Module string `json:"module"`
	Nodes  []Node `json:"nodes"`
	Edges  []Edge `json:"edges"`
}

// Graph is a traversable static model of one Go module.
type Graph struct {
	module string
	nodes  []Node
	edges  []Edge
	byID   map[string]int
	out    map[string][]int
	in     map[string][]int
}

// Module returns the Go module path that was indexed.
func (g *Graph) Module() string {
	if g == nil {
		return ""
	}
	return g.module
}

// NodeCount returns the number of indexed nodes.
func (g *Graph) NodeCount() int {
	if g == nil {
		return 0
	}
	return len(g.nodes)
}

// Snapshot returns a JSON-serializable copy of the graph.
func (g *Graph) Snapshot() Snapshot {
	if g == nil {
		return Snapshot{Nodes: []Node{}, Edges: []Edge{}}
	}
	return Snapshot{
		Module: g.module,
		Nodes:  append([]Node(nil), g.nodes...),
		Edges:  append([]Edge(nil), g.edges...),
	}
}

// Node returns the node with the given id.
func (g *Graph) Node(id string) (Node, bool) {
	if g == nil {
		return Node{}, false
	}
	i, ok := g.byID[id]
	if !ok {
		return Node{}, false
	}
	return g.nodes[i], true
}

// Neighbors returns inbound and outbound edges for id.
func (g *Graph) Neighbors(id string) ([]Neighbor, bool) {
	if g == nil {
		return nil, false
	}
	if _, ok := g.byID[id]; !ok {
		return nil, false
	}
	out := make([]Neighbor, 0, len(g.out[id])+len(g.in[id]))
	for _, ei := range g.out[id] {
		e := g.edges[ei]
		n, ok := g.Node(e.To)
		if !ok {
			continue
		}
		out = append(out, Neighbor{Node: n, Kind: e.Kind, Direction: DirOut})
	}
	for _, ei := range g.in[id] {
		e := g.edges[ei]
		n, ok := g.Node(e.From)
		if !ok {
			continue
		}
		out = append(out, Neighbor{Node: n, Kind: e.Kind, Direction: DirIn})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Direction != out[j].Direction {
			return out[i].Direction < out[j].Direction
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Node.ID < out[j].Node.ID
	})
	return out, true
}

func (g *Graph) index() {
	g.byID = make(map[string]int, len(g.nodes))
	g.out = make(map[string][]int, len(g.nodes))
	g.in = make(map[string][]int, len(g.nodes))
	for i, n := range g.nodes {
		g.byID[n.ID] = i
	}
	for i, e := range g.edges {
		g.out[e.From] = append(g.out[e.From], i)
		g.in[e.To] = append(g.in[e.To], i)
	}
}
