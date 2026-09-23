package investigate

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/sumedhaerram/aquila/internal/graph"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/locate"
)

const maxQuestion = 2 << 10

// Report is observed facts that match an investigation question.
// It is not a patch, a pass, or model confidence.
type Report struct {
	Question string   `json:"question"`
	Origin   string   `json:"origin,omitempty"`
	Hits     []string `json:"hits"`
	Notes    []string `json:"notes,omitempty"`
}

// Hop is a labeled runtime edge.
type Hop struct {
	From       string
	To         string
	Provenance string
}

// Path is a labeled runtime path.
type Path struct {
	Services   []string
	Provenance string
}

// Bind is a labeled span-to-source attachment.
type Bind struct {
	Service    string
	Name       string
	File       string
	Route      string
	Provenance string
	Line       int
}

// Input is the observed window an investigation may cite.
type Input struct {
	Question string
	Origin   string
	Services []string
	Hops     []Hop
	Paths    []Path
	Routes   []string
	Binds    []Bind
	Impact   *impact.Report
}

// Search joins question tokens onto services, hops, binds, and impact.
// Unmatched questions fail closed: hits stay empty rather than guessed.
func Search(in Input) (Report, error) {
	q := strings.TrimSpace(in.Question)
	if q == "" {
		if in.Impact == nil {
			return Report{}, fmt.Errorf("investigate: empty question")
		}
		out := Report{
			Question: "local edit",
			Origin:   in.Origin,
			Hits:     []string{},
			Notes: []string{
				"facts only. not a patch. not validated.",
				"question omitted: citing the local worktree",
			},
		}
		out.Hits = append(out.Hits, impactFacts(*in.Impact)...)
		if len(out.Hits) == 0 {
			out.Notes = append(out.Notes, "no local-edit facts")
		}
		return out, nil
	}
	if len(q) > maxQuestion {
		return Report{}, fmt.Errorf("investigate: question too large")
	}
	out := Report{
		Question: q,
		Origin:   in.Origin,
		Hits:     []string{},
		Notes: []string{
			"facts only. not a patch. not validated.",
		},
	}
	tokens := tokens(q)
	if len(tokens) == 0 {
		out.Notes = append(out.Notes, "no searchable token")
		return out, nil
	}
	out.Hits = join(in, tokens)
	if len(out.Hits) == 0 {
		out.Notes = append(out.Notes, "no token matched observed facts")
	}
	return out, nil
}

func join(in Input, tokens []string) []string {
	seed := map[string]struct{}{}
	for _, s := range in.Services {
		if match(s, tokens) {
			seed[s] = struct{}{}
		}
	}
	for _, h := range in.Hops {
		if match(h.From, tokens) {
			seed[h.From] = struct{}{}
		}
		if match(h.To, tokens) {
			seed[h.To] = struct{}{}
		}
	}
	for _, p := range in.Paths {
		for _, s := range p.Services {
			if match(s, tokens) {
				seed[s] = struct{}{}
			}
		}
	}
	for _, b := range in.Binds {
		if match(b.Service, tokens) || match(b.Name, tokens) || match(b.File, tokens) || match(b.Route, tokens) {
			if b.Service != "" {
				seed[b.Service] = struct{}{}
			}
		}
	}
	if in.Impact != nil {
		for _, f := range in.Impact.Direct {
			if match(f.Name, tokens) || match(f.File, tokens) || match(f.Service, tokens) || match(f.Route, tokens) {
				if f.Service != "" {
					seed[f.Service] = struct{}{}
				}
			}
		}
		for _, f := range in.Impact.Runtime {
			if match(f.Name, tokens) || match(f.Path, tokens) || match(f.Service, tokens) || match(f.Route, tokens) {
				if f.Service != "" {
					seed[f.Service] = struct{}{}
				}
			}
		}
	}
	for _, h := range in.Hops {
		if _, ok := seed[h.From]; ok {
			seed[h.To] = struct{}{}
		}
		if _, ok := seed[h.To]; ok {
			seed[h.From] = struct{}{}
		}
	}

	var out []string
	seen := map[string]struct{}{}
	add := func(line string) {
		if line == "" {
			return
		}
		if _, ok := seen[line]; ok {
			return
		}
		seen[line] = struct{}{}
		out = append(out, line)
	}

	for _, s := range in.Services {
		if _, ok := seed[s]; ok {
			add(label("service", s, ""))
		}
	}
	for _, h := range in.Hops {
		if _, ok := seed[h.From]; ok {
			add(formatHop(h))
			continue
		}
		if _, ok := seed[h.To]; ok {
			add(formatHop(h))
		}
	}
	for _, p := range in.Paths {
		if pathTouches(p.Services, seed) {
			add(formatPath(p))
		}
	}
	for _, r := range in.Routes {
		if match(r, tokens) {
			add(r)
		}
	}
	for _, b := range in.Binds {
		if _, ok := seed[b.Service]; ok {
			add(formatBind(b))
			continue
		}
		if match(b.Name, tokens) || match(b.File, tokens) || match(b.Route, tokens) {
			add(formatBind(b))
		}
	}
	if in.Impact != nil {
		if impactTouches(*in.Impact, tokens, seed) {
			for _, line := range impactFacts(*in.Impact) {
				add(line)
			}
		}
	}
	return out
}

func formatHop(h Hop) string {
	prov := h.Provenance
	if prov == "" {
		prov = graph.ProvenanceObservedParent
	}
	return label("hop", h.From+" -> "+h.To, prov)
}

func formatPath(p Path) string {
	prov := p.Provenance
	if prov == "" {
		prov = graph.ProvenanceObservedParent
	}
	return label("path", strings.Join(p.Services, " -> "), prov)
}

func formatBind(b Bind) string {
	text := strings.TrimSpace(b.Service + " " + b.Name + " " + b.File)
	if b.Line > 0 {
		text += fmt.Sprintf(":%d", b.Line)
	}
	if r := strings.TrimSpace(b.Route); r != "" {
		text += " " + r
	}
	prov := b.Provenance
	if prov == "" {
		prov = locate.ProvenanceCodeAttrs
	}
	return label("bind", text, prov)
}

func label(kind, text, prov string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if prov == "" {
		return kind + "  " + text
	}
	return kind + "  " + text + "  " + prov
}

func pathTouches(services []string, seed map[string]struct{}) bool {
	for _, s := range services {
		if _, ok := seed[s]; ok {
			return true
		}
	}
	return false
}

func impactTouches(rep impact.Report, tokens []string, seed map[string]struct{}) bool {
	for _, f := range rep.Files {
		if match(f, tokens) {
			return true
		}
	}
	for _, f := range append(append([]impact.Finding{}, rep.Direct...), append(rep.Likely, append(rep.Runtime, rep.Unobserved...)...)...) {
		if _, ok := seed[f.Service]; ok && f.Service != "" {
			return true
		}
		if match(f.Name, tokens) || match(f.File, tokens) || match(f.Route, tokens) || match(f.Path, tokens) {
			return true
		}
	}
	return false
}

func impactFacts(rep impact.Report) []string {
	var out []string
	for _, f := range rep.Files {
		out = append(out, label("file", f, "changed_lines"))
	}
	for _, f := range rep.Direct {
		out = append(out, label("direct", findingText(f), provenanceOr(f.Reason, "changed_lines")))
	}
	for _, f := range rep.Likely {
		out = append(out, label("likely", findingText(f), provenanceOr(f.Provenance, "typed_call")))
	}
	for _, f := range rep.Runtime {
		line := strings.TrimSpace(f.Route + " " + f.Path + " " + f.Name)
		out = append(out, label("runtime", line, provenanceOr(f.Provenance, graph.ProvenanceObservedParent)))
	}
	for _, f := range rep.Unobserved {
		out = append(out, label("unobserved", findingText(f), provenanceOr(f.Reason, "unobserved")))
	}
	return out
}

func findingText(f impact.Finding) string {
	loc := strings.TrimSpace(f.Name + " " + f.File)
	if f.Line > 0 {
		loc += fmt.Sprintf(":%d", f.Line)
	}
	return loc
}

func provenanceOr(got, fallback string) string {
	got = strings.TrimSpace(got)
	if got == "" {
		return fallback
	}
	return got
}

func tokens(q string) []string {
	fields := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	seen := map[string]struct{}{}
	var out []string
	for _, t := range fields {
		if len(t) < 3 || stop(t) {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func match(fact string, tokens []string) bool {
	low := strings.ToLower(fact)
	for _, t := range tokens {
		if strings.Contains(low, t) {
			return true
		}
	}
	return false
}

func stop(t string) bool {
	switch t {
	case "the", "and", "for", "how", "what", "why", "can", "this", "that",
		"with", "from", "are", "was", "does", "did", "not", "our", "any":
		return true
	default:
		return false
	}
}
