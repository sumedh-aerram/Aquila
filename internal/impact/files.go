package impact

import (
	"path"
	"sort"
	"strings"

	"github.com/sumedhaerram/aquila/internal/diff"
	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/locate"
)

// Changes separates function bindings from changes the analyzer cannot establish.
type Changes struct {
	Bound      []string `json:"bound,omitempty"`
	Unanalyzed []string `json:"unanalyzed,omitempty"`
}

// Inert reports explicitly allowed files that do not change the built application.
func Inert(p string) bool {
	switch path.Ext(p) {
	case ".md", ".rst":
		return true
	}
	b := path.Base(p)
	return strings.HasPrefix(b, "LICENSE") ||
		strings.HasPrefix(b, "NOTICE") ||
		strings.HasPrefix(b, "AUTHORS") ||
		strings.HasPrefix(b, "CHANGELOG") ||
		b == "CODEOWNERS" || strings.HasSuffix(b, "_test.go")
}

// Classify requires every direct finding for a path to bind changed function lines.
func Classify(rep Report) Changes {
	var c Changes
	bound := map[string]bool{}
	unknown := map[string]bool{}
	for _, p := range rep.Files {
		if Inert(p) {
			continue
		}
		var ids []string
		analyzed := true
		for _, f := range rep.Direct {
			if f.File != p {
				continue
			}
			if f.Reason != "changed_lines" || f.SourceID == "" {
				analyzed = false
			}
			ids = append(ids, f.SourceID)
		}
		if !analyzed || len(ids) == 0 {
			unknown[p] = true
			continue
		}
		for _, id := range ids {
			bound[id] = true
		}
	}
	for id := range bound {
		c.Bound = append(c.Bound, id)
	}
	sort.Strings(c.Bound)
	for p := range unknown {
		c.Unanalyzed = append(c.Unanalyzed, p)
	}
	sort.Strings(c.Unanalyzed)
	return c
}

// FromDiff reports file-level blast radius when there is no typed source graph.
// Runtime findings require span code.file.path to match a changed path. Routes
// and filenames are not guessed.
func FromDiff(d diff.Diff, spans []ingest.Span) Report {
	out := Report{
		Files:      []string{},
		Direct:     []Finding{},
		Likely:     []Finding{},
		Runtime:    []Finding{},
		Unobserved: []Finding{},
	}
	seenFile := map[string]struct{}{}
	seenRuntime := map[string]struct{}{}
	for _, f := range d.Files {
		p := f.Path
		if p == "" {
			continue
		}
		if _, dup := seenFile[p]; !dup {
			seenFile[p] = struct{}{}
			out.Files = append(out.Files, p)
		}
		reason := "unknown_file"
		if f.Status == "add" {
			reason = "new_file"
		}
		out.Unobserved = appendFinding(out.Unobserved, Finding{
			Name: p, File: p, Path: p, Reason: reason, Provenance: "diff",
		})
		for _, sp := range spans {
			if !codeFileMatch(p, sp.CodeFile) {
				continue
			}
			name := strings.TrimSpace(sp.HTTPMethod + " " + sp.HTTPRoute)
			if name == "" {
				name = strings.TrimSpace(sp.Name)
			}
			if name == "" {
				name = sp.ServiceName
			}
			key := sp.ServiceName + "\x00" + name + "\x00" + p
			if _, dup := seenRuntime[key]; dup {
				continue
			}
			seenRuntime[key] = struct{}{}
			out.Runtime = appendFinding(out.Runtime, Finding{
				Name:       name,
				File:       p,
				Service:    sp.ServiceName,
				Path:       strings.TrimSpace(sp.HTTPRoute),
				Route:      routeLabel(sp.HTTPMethod, sp.HTTPRoute),
				Reason:     "code_file",
				Provenance: locate.ProvenanceCodeAttrs,
			})
		}
	}
	sort.Strings(out.Files)
	sortFindings(out.Runtime)
	sortFindings(out.Unobserved)
	return out
}

func codeFileMatch(diffPath, codeFile string) bool {
	dp, ok := diff.CanonicalPath(diffPath)
	if !ok {
		dp = path.Clean(strings.ReplaceAll(diffPath, "\\", "/"))
	}
	cp, ok := diff.CanonicalPath(codeFile)
	if !ok {
		cp = path.Clean(strings.ReplaceAll(strings.TrimSpace(codeFile), "\\", "/"))
		cp = strings.TrimPrefix(cp, "/")
	}
	if dp == "" || cp == "" || dp == "." || cp == "." {
		return false
	}
	if dp == cp {
		return true
	}
	if !strings.Contains(dp, "/") {
		return false
	}
	return strings.HasSuffix(cp, "/"+dp)
}
