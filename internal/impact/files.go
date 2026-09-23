package impact

import (
	"path"
	"sort"
	"strings"

	"github.com/sumedhaerram/aquila/internal/diff"
	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/locate"
)

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
