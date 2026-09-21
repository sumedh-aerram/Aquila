package locate

import (
	"path"
	"sort"
	"strings"

	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/source"
)

const (
	// ProvenanceCodeAttrs marks a binding from span code.function.name and code.file.path.
	ProvenanceCodeAttrs = "code_attrs"

	ReasonMissingAttrs = "missing_code_attrs"
	ReasonInvalidFile  = "invalid_code_file"
	ReasonNoFile       = "no_file"
	ReasonNoFunction   = "no_function"
	ReasonAmbiguous    = "ambiguous"

	maxBindings         = 2000
	maxUnmappedReturned = 200
)

// Binding is a span attached to one source node.
type Binding struct {
	TraceID      string `json:"trace_id"`
	SpanID       string `json:"span_id"`
	ServiceName  string `json:"service_name"`
	SpanName     string `json:"span_name"`
	CodeFunction string `json:"code_function"`
	CodeFile     string `json:"code_file"`
	SourceID     string `json:"source_id"`
	SourceName   string `json:"source_name"`
	File         string `json:"file"`
	Line         int    `json:"line,omitempty"`
	Provenance   string `json:"provenance"`
}

// Unmapped is a span that could not be bound without guessing.
type Unmapped struct {
	TraceID      string `json:"trace_id"`
	SpanID       string `json:"span_id"`
	ServiceName  string `json:"service_name"`
	SpanName     string `json:"span_name"`
	CodeFunction string `json:"code_function,omitempty"`
	CodeFile     string `json:"code_file,omitempty"`
	Reason       string `json:"reason"`
}

// Snapshot is a derived join of a span window onto a source graph.
type Snapshot struct {
	Bindings      []Binding  `json:"bindings"`
	Unmapped      []Unmapped `json:"unmapped"`
	Bound         int        `json:"bound"`
	UnmappedCount int        `json:"unmapped_count"`
	SpanCount     int        `json:"span_count"`
}

// Bind maps spans onto source functions using explicit code attributes only.
// Span names, routes, and service names are never used as a fallback.
func Bind(g *source.Graph, spans []ingest.Span) Snapshot {
	out := Snapshot{
		Bindings: []Binding{},
		Unmapped: []Unmapped{},
	}
	if g == nil {
		return out
	}
	idx := indexSource(g)
	for i := range spans {
		sp := spans[i]
		if sp.TraceID == "" || sp.SpanID == "" {
			continue
		}
		out.SpanCount++
		b, u, ok := bindOne(idx, sp)
		if ok {
			out.Bound++
			if len(out.Bindings) < maxBindings {
				out.Bindings = append(out.Bindings, b)
			}
			continue
		}
		out.UnmappedCount++
		if len(out.Unmapped) < maxUnmappedReturned {
			out.Unmapped = append(out.Unmapped, u)
		}
	}
	sort.Slice(out.Bindings, func(i, j int) bool {
		if out.Bindings[i].TraceID != out.Bindings[j].TraceID {
			return out.Bindings[i].TraceID < out.Bindings[j].TraceID
		}
		return out.Bindings[i].SpanID < out.Bindings[j].SpanID
	})
	return out
}

type sourceIndex struct {
	files map[string]struct{}
	funcs map[string][]source.Node
}

func indexSource(g *source.Graph) sourceIndex {
	idx := sourceIndex{
		files: make(map[string]struct{}),
		funcs: make(map[string][]source.Node),
	}
	for _, n := range g.Snapshot().Nodes {
		switch n.Kind {
		case source.KindFile:
			if n.File != "" {
				idx.files[n.File] = struct{}{}
			}
		case source.KindFunction:
			if n.File != "" {
				idx.funcs[n.File] = append(idx.funcs[n.File], n)
			}
		}
	}
	return idx
}

func bindOne(idx sourceIndex, sp ingest.Span) (Binding, Unmapped, bool) {
	base := Unmapped{
		TraceID:      sp.TraceID,
		SpanID:       sp.SpanID,
		ServiceName:  sp.ServiceName,
		SpanName:     sp.Name,
		CodeFunction: sp.CodeFunction,
		CodeFile:     sp.CodeFile,
	}
	if strings.TrimSpace(sp.CodeFunction) == "" || strings.TrimSpace(sp.CodeFile) == "" {
		base.Reason = ReasonMissingAttrs
		return Binding{}, base, false
	}
	file, ok := normalizeFile(sp.CodeFile)
	if !ok {
		base.Reason = ReasonInvalidFile
		return Binding{}, base, false
	}
	if _, exists := idx.files[file]; !exists {
		if _, hasFuncs := idx.funcs[file]; !hasFuncs {
			base.Reason = ReasonNoFile
			return Binding{}, base, false
		}
	}
	want := functionName(sp.CodeFunction)
	var hits []source.Node
	for _, fn := range idx.funcs[file] {
		if fn.Name == want {
			hits = append(hits, fn)
		}
	}
	switch len(hits) {
	case 0:
		base.Reason = ReasonNoFunction
		return Binding{}, base, false
	case 1:
		fn := hits[0]
		return Binding{
			TraceID:      sp.TraceID,
			SpanID:       sp.SpanID,
			ServiceName:  sp.ServiceName,
			SpanName:     sp.Name,
			CodeFunction: sp.CodeFunction,
			CodeFile:     sp.CodeFile,
			SourceID:     fn.ID,
			SourceName:   fn.Name,
			File:         fn.File,
			Line:         fn.Line,
			Provenance:   ProvenanceCodeAttrs,
		}, Unmapped{}, true
	default:
		base.Reason = ReasonAmbiguous
		return Binding{}, base, false
	}
}

func normalizeFile(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, "\\", "/")
	s = path.Clean(s)
	if s == "." || s == "/" || strings.HasPrefix(s, "../") || strings.Contains(s, "/../") {
		return "", false
	}
	s = strings.TrimPrefix(s, "/")
	s = strings.TrimPrefix(s, "examples/shop/")
	if s == "" || strings.Contains(s, "..") {
		return "", false
	}
	return s, true
}

func functionName(codeFunction string) string {
	s := strings.TrimSpace(codeFunction)
	if i := strings.LastIndex(s, "."); i >= 0 && i+1 < len(s) {
		return s[i+1:]
	}
	return s
}
