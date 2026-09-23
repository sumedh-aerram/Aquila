package investigate

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/sumedhaerram/aquila/internal/impact"
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

// Input is the observed window an investigation may cite.
type Input struct {
	Question string
	Origin   string
	Services []string
	Hops     []string
	Paths    []string
	Routes   []string
	Binds    []string
	Impact   *impact.Report
}

// Search returns facts whose text contains a question token.
// Unmatched questions still fail closed: hits stay empty rather than guessed.
func Search(in Input) (Report, error) {
	q := strings.TrimSpace(in.Question)
	if q == "" {
		return Report{}, fmt.Errorf("investigate: empty question")
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
	for _, fact := range facts(in) {
		if match(fact, tokens) {
			out.Hits = append(out.Hits, fact)
		}
	}
	if len(out.Hits) == 0 {
		out.Notes = append(out.Notes, "no token matched observed facts")
	}
	return out, nil
}

func facts(in Input) []string {
	var out []string
	for _, s := range in.Services {
		if s != "" {
			out = append(out, "service "+s)
		}
	}
	for _, h := range in.Hops {
		if h != "" {
			out = append(out, "hop "+h)
		}
	}
	for _, p := range in.Paths {
		if p != "" {
			out = append(out, "path "+p)
		}
	}
	for _, r := range in.Routes {
		if r != "" {
			out = append(out, r)
		}
	}
	for _, b := range in.Binds {
		if b != "" {
			out = append(out, "bind "+b)
		}
	}
	if in.Impact == nil {
		return out
	}
	for _, f := range in.Impact.Files {
		out = append(out, "file "+f)
	}
	for _, f := range in.Impact.Direct {
		out = append(out, "direct "+strings.TrimSpace(f.Name+" "+f.File))
	}
	for _, f := range in.Impact.Likely {
		out = append(out, "likely "+strings.TrimSpace(f.Name+" "+f.File))
	}
	for _, f := range in.Impact.Runtime {
		line := strings.TrimSpace(f.Path + " " + f.Name)
		out = append(out, "runtime "+line)
	}
	for _, f := range in.Impact.Unobserved {
		out = append(out, "unobserved "+strings.TrimSpace(f.Name+" "+f.File))
	}
	return out
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
