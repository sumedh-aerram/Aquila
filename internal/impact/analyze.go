package impact

import (
	"math"
	"sort"
	"strings"

	"github.com/sumedhaerram/aquila/internal/diff"
	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/locate"
	"github.com/sumedhaerram/aquila/internal/source"
)

const maxFindings = 500

// ProvenanceTraceEntryCaller marks entry-route attribution through an immediate static caller.
const ProvenanceTraceEntryCaller = "trace_entry_caller"

// Finding is one inspectable impact item. There is no numeric risk score.
type Finding struct {
	Via         string `json:"via,omitempty"`
	TraceID     string `json:"trace_id,omitempty"`
	SpanID      string `json:"span_id,omitempty"`
	EntrySpanID string `json:"entry_span_id,omitempty"`
	Name        string `json:"name"`
	File        string `json:"file,omitempty"`
	SourceID    string `json:"source_id,omitempty"`
	Service     string `json:"service,omitempty"`
	Path        string `json:"path,omitempty"`
	Route       string `json:"route,omitempty"`
	Line        int    `json:"line,omitempty"`
	Reason      string `json:"reason"`
	Provenance  string `json:"provenance"`
}

// Report is the blast radius of a parsed diff against source and runtime.
type Report struct {
	RouteTraceCounts []RouteTraceCount `json:"route_trace_counts,omitempty"`
	Files            []string          `json:"files"`
	Direct           []Finding         `json:"direct"`
	Likely           []Finding         `json:"likely"`
	Runtime          []Finding         `json:"runtime"`
	Unobserved       []Finding         `json:"unobserved"`
}

// Analyze maps a diff onto the source graph, then overlays locate and runtime
// topology when those snapshots are present. Callers are likely; callees are not.
// Routes target complete entry ancestry rather than nested service endpoints.
func Analyze(g *source.Graph, d diff.Diff, loc locate.Snapshot, rt graph.Snapshot) Report {
	out := Report{
		Files:      []string{},
		Direct:     []Finding{},
		Likely:     []Finding{},
		Runtime:    []Finding{},
		Unobserved: []Finding{},
	}
	if g == nil {
		return out
	}
	idx := index(g)
	seenFile := map[string]struct{}{}
	directID := map[string]struct{}{}

	for _, f := range d.Files {
		path := f.Path
		if _, dup := seenFile[path]; !dup {
			seenFile[path] = struct{}{}
			out.Files = append(out.Files, path)
		}
		fileNode, ok := idx.files[path]
		if !ok {
			out.Unobserved = appendFinding(out.Unobserved, Finding{
				Name: path, File: path, Reason: "unknown_file", Provenance: "diff",
			})
			continue
		}
		if f.Binary {
			out.Direct = appendFinding(out.Direct, Finding{
				Name: fileNode.Name, File: path, SourceID: fileNode.ID, Reason: "binary", Provenance: "diff",
			})
			continue
		}
		hits, outside := overlappingFuncs(idx.funcs[path], f.Lines)
		if outside {
			out.Direct = appendFinding(out.Direct, Finding{Name: fileNode.Name, File: path, SourceID: fileNode.ID, Reason: "changed_outside_func", Provenance: "diff"})
		}
		if len(hits) == 0 {
			out.Direct = appendFinding(out.Direct, Finding{
				Name: fileNode.Name, File: path, SourceID: fileNode.ID, Reason: "changed_file", Provenance: "diff",
			})
			continue
		}
		for _, h := range hits {
			directID[h.fn.ID] = struct{}{}
			out.Direct = appendFinding(out.Direct, Finding{
				Name: h.fn.Name, File: h.fn.File, SourceID: h.fn.ID, Line: h.line, Reason: "changed_lines", Provenance: "diff",
			})
		}
	}

	likelyID := map[string]struct{}{}
	callerTargets := make(map[string]map[string]struct{})
	for id := range directID {
		ns, ok := g.Neighbors(id)
		if !ok {
			continue
		}
		for _, n := range ns {
			if n.Direction != source.DirIn || n.Kind != source.EdgeCalls {
				continue
			}
			targets := callerTargets[n.Node.ID]
			if targets == nil {
				targets = make(map[string]struct{})
				callerTargets[n.Node.ID] = targets
			}
			if changed, ok := g.Node(id); ok {
				targets[changed.ID] = struct{}{}
			}
			if _, already := directID[n.Node.ID]; already {
				continue
			}
			if _, dup := likelyID[n.Node.ID]; dup {
				continue
			}
			likelyID[n.Node.ID] = struct{}{}
			out.Likely = appendFinding(out.Likely, Finding{
				Name: n.Node.Name, File: n.Node.File, SourceID: n.Node.ID, Line: n.Node.Line, Reason: "caller", Provenance: source.ProvenanceTypes,
			})
		}
	}

	watch := map[string]struct{}{}
	for id := range directID {
		watch[id] = struct{}{}
	}
	for id := range likelyID {
		watch[id] = struct{}{}
	}
	services := map[string]struct{}{}
	seenBind := map[string]struct{}{}
	routeTraces := make(map[string]map[string]struct{})
	for _, b := range loc.Bindings {
		if _, ok := watch[b.SourceID]; !ok {
			continue
		}
		if b.ServiceName != "" {
			services[b.ServiceName] = struct{}{}
		}
		entry, _ := rt.EntryForSpan(b.TraceID, b.SpanID)
		_, direct := directID[b.SourceID]
		for _, f := range boundFindings(b, entry, direct, callerTargets[b.SourceID]) {
			if f.Service == "" && f.Route == "" {
				continue
			}
			if f.Route != "" && b.TraceID != "" {
				if routeTraces[f.Route] == nil {
					routeTraces[f.Route] = make(map[string]struct{})
				}
				routeTraces[f.Route][b.TraceID] = struct{}{}
			}
			key := f.SourceID + "\x00" + f.Route + "\x00" + f.Provenance
			if _, dup := seenBind[key]; dup {
				continue
			}
			seenBind[key] = struct{}{}
			out.Runtime = appendFinding(out.Runtime, f)
		}
	}
	for _, p := range rt.Paths {
		if !pathTouches(p.Services, services) {
			continue
		}
		out.Runtime = appendFinding(out.Runtime, Finding{
			Path: strings.Join(p.Services, " -> "), Reason: "observed_path", Provenance: graph.ProvenanceObservedParent,
		})
	}

	for _, f := range out.Direct {
		if f.SourceID == "" {
			continue
		}
		if locateHit(loc, f.SourceID) {
			continue
		}
		out.Unobserved = appendFinding(out.Unobserved, Finding{
			Name: f.Name, File: f.File, SourceID: f.SourceID, Reason: "no_runtime_bind", Provenance: "locate",
		})
	}

	for _, route := range RuntimeRoutes(out) {
		if n := len(routeTraces[route]); n > 0 {
			out.RouteTraceCounts = append(out.RouteTraceCounts, RouteTraceCount{
				Route:  route,
				Traces: n,
			})
		}
	}
	sortFindings(out.Direct)
	sortFindings(out.Likely)
	sortFindings(out.Runtime)
	sortFindings(out.Unobserved)
	sort.Strings(out.Files)
	return out
}

type srcIndex struct {
	files map[string]source.Node
	funcs map[string][]source.Node
}

func index(g *source.Graph) srcIndex {
	idx := srcIndex{files: map[string]source.Node{}, funcs: map[string][]source.Node{}}
	for _, n := range g.Snapshot().Nodes {
		switch n.Kind {
		case source.KindFile:
			if n.File != "" {
				idx.files[n.File] = n
			}
		case source.KindFunction:
			if n.File != "" {
				idx.funcs[n.File] = append(idx.funcs[n.File], n)
			}
		}
	}
	for k := range idx.funcs {
		fns := idx.funcs[k]
		sort.Slice(fns, func(i, j int) bool {
			if fns[i].Line != fns[j].Line {
				return fns[i].Line < fns[j].Line
			}
			return fns[i].ID < fns[j].ID
		})
		idx.funcs[k] = fns
	}
	return idx
}

type funcHit struct {
	fn   source.Node
	line int
}

func overlappingFuncs(funcs []source.Node, lines []int) ([]funcHit, bool) {
	if len(funcs) == 0 || len(lines) == 0 {
		return nil, len(lines) > 0
	}
	type span struct {
		n   source.Node
		end int
	}
	spans := make([]span, 0, len(funcs))
	for i, fn := range funcs {
		end := math.MaxInt
		switch {
		case fn.EndLine >= fn.Line && fn.EndLine > 0:
			end = fn.EndLine + 1
		case i+1 < len(funcs):
			end = funcs[i+1].Line
		}
		spans = append(spans, span{n: fn, end: end})
	}
	seen := map[string]struct{}{}
	var out []funcHit
	outside := false
	for _, ln := range lines {
		matched := false
		for _, s := range spans {
			if ln < s.n.Line || ln >= s.end {
				continue
			}
			matched = true
			if _, ok := seen[s.n.ID]; ok {
				break
			}
			seen[s.n.ID] = struct{}{}
			out = append(out, funcHit{fn: s.n, line: ln})
			break
		}
		if !matched {
			outside = true
		}
	}
	return out, outside
}

func routeLabel(method, path string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if method == "" || path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return method + " " + path
}

func appendFinding(dst []Finding, f Finding) []Finding {
	if len(dst) >= maxFindings {
		return dst
	}
	return append(dst, f)
}

func locateHit(loc locate.Snapshot, id string) bool {
	for _, b := range loc.Bindings {
		if b.SourceID == id {
			return true
		}
	}
	return false
}

func pathTouches(path []string, services map[string]struct{}) bool {
	for _, s := range path {
		if _, ok := services[s]; ok {
			return true
		}
	}
	return false
}

func sortFindings(fs []Finding) {
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].File != fs[j].File {
			return fs[i].File < fs[j].File
		}
		if fs[i].Name != fs[j].Name {
			return fs[i].Name < fs[j].Name
		}
		if fs[i].Path != fs[j].Path {
			return fs[i].Path < fs[j].Path
		}
		if fs[i].Route != fs[j].Route {
			return fs[i].Route < fs[j].Route
		}
		if fs[i].TraceID != fs[j].TraceID {
			return fs[i].TraceID < fs[j].TraceID
		}
		return fs[i].SpanID < fs[j].SpanID
	})
}

func boundFindings(b locate.Binding, entry graph.EntryRoute, direct bool, targets map[string]struct{}) []Finding {
	f := Finding{
		TraceID:     b.TraceID,
		SpanID:      b.SpanID,
		EntrySpanID: entry.SpanID,
		Name:        b.SourceName,
		File:        b.File,
		SourceID:    b.SourceID,
		Service:     b.ServiceName,
		Line:        b.Line,
		Route:       routeLabel(entry.Method, entry.Route),
		Reason:      "bound_span",
		Provenance:  locate.ProvenanceCodeAttrs,
	}
	if f.Route == "" {
		return []Finding{f}
	}
	var findings []Finding
	if direct {
		f.Provenance = graph.ProvenanceTraceEntry
		findings = append(findings, f)
	}
	if len(targets) > 0 {
		f.Provenance, f.Reason = ProvenanceTraceEntryCaller, "bound_span_caller"
		var links []string
		for target := range targets {
			links = append(links, b.SourceID+" -> "+target)
		}
		sort.Strings(links)
		f.Via = strings.Join(links, "; ")
		findings = append(findings, f)
	}
	return findings
}
