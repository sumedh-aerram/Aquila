package impact

import (
	"strings"
	"testing"

	"github.com/sumedhaerram/aquila/internal/diff"
	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/locate"
)

func TestFromDiffUnknownFileUnobserved(t *testing.T) {
	t.Parallel()
	d, err := diff.Parse([]byte(`--- a/backend/server.py
+++ b/backend/server.py
@@ -1,1 +1,1 @@
-a
+b
`))
	if err != nil {
		t.Fatal(err)
	}
	rep := FromDiff(d, nil)
	if len(rep.Files) != 1 || rep.Files[0] != "backend/server.py" {
		t.Fatalf("files=%v", rep.Files)
	}
	if len(rep.Direct) != 0 || len(rep.Likely) != 0 {
		t.Fatalf("typed=%+v %+v", rep.Direct, rep.Likely)
	}
	if len(rep.Unobserved) != 1 || rep.Unobserved[0].Reason != "unknown_file" {
		t.Fatalf("unobserved=%+v", rep.Unobserved)
	}
}

func TestFromDiffRuntimeOnlyOnCodeFile(t *testing.T) {
	t.Parallel()
	d, err := diff.Parse([]byte(`--- a/backend/server.py
+++ b/backend/server.py
@@ -1,1 +1,1 @@
-a
+b
`))
	if err != nil {
		t.Fatal(err)
	}
	rep := FromDiff(d, []ingest.Span{
		{ServiceName: "reroute", HTTPMethod: "GET", HTTPRoute: "/api/health", CodeFile: "/opt/app/backend/server.py"},
		{ServiceName: "shop", HTTPMethod: "GET", HTTPRoute: "/checkout", CodeFile: "internal/payment/handler.go"},
		{ServiceName: "reroute", HTTPMethod: "GET", HTTPRoute: "/api/jobs"},
	})
	if len(rep.Runtime) != 1 {
		t.Fatalf("runtime=%+v", rep.Runtime)
	}
	r := rep.Runtime[0]
	if r.Service != "reroute" || r.Path != "/api/health" || r.Route != "GET /api/health" || r.Reason != "code_file" || r.Provenance != locate.ProvenanceCodeAttrs {
		t.Fatalf("%+v", r)
	}
	if strings.Contains(r.Name, "checkout") {
		t.Fatal("must not join on route guess")
	}
}

func TestFromDiffBareFilenameDoesNotSuffixMatch(t *testing.T) {
	t.Parallel()
	d, err := diff.Parse([]byte(`--- a/handler.go
+++ b/handler.go
@@ -1,1 +1,1 @@
-a
+b
`))
	if err != nil {
		t.Fatal(err)
	}
	rep := FromDiff(d, []ingest.Span{{
		ServiceName: "payment",
		HTTPMethod:  "POST",
		HTTPRoute:   "/authorize",
		CodeFile:    "internal/payment/handler.go",
	}})
	if len(rep.Runtime) != 0 {
		t.Fatalf("guessed runtime=%+v", rep.Runtime)
	}
}

func TestClassify(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, path, reason string
		bound, unanalyzed  bool
	}{
		{"doc only", "README.md", "", false, false},
		{"requirements", "requirements.txt", "", false, true},
		{"cmake", "CMakeLists.txt", "", false, true},
		{"constraints", "constraints.txt", "", false, true},
		{"test only", "a_test.go", "changed_lines", false, false},
		{"dockerfile", "Dockerfile", "", false, true},
		{"changed lines", "a.go", "changed_lines", true, false},
		{"changed file", "a.go", "changed_file", false, true},
		{"no findings", "a.go", "", false, true},
		{"mixed findings", "a.go", "mixed", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := Report{Files: []string{tc.path}}
			if tc.reason != "" {
				rep.Direct = []Finding{{File: tc.path, SourceID: "f", Reason: tc.reason}}
			}
			if tc.reason == "mixed" {
				rep.Direct = append(rep.Direct, Finding{File: tc.path, SourceID: "g", Reason: "changed_lines"})
			}
			got := Classify(rep)
			if (len(got.Bound) > 0) != tc.bound || (len(got.Unanalyzed) > 0) != tc.unanalyzed {
				t.Fatalf("%+v", got)
			}
			if tc.bound && got.Bound[0] != "f" {
				t.Fatalf("%+v", got)
			}
			if tc.unanalyzed && got.Unanalyzed[0] != tc.path {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestInert(t *testing.T) {
	t.Parallel()
	for _, p := range []string{"README.md", "guide.rst", "LICENSE", "LICENSE-MIT",
		"NOTICE", "NOTICE.txt", "AUTHORS", "AUTHORS.txt", ".github/CODEOWNERS",
		"CHANGELOG", "CHANGELOG.txt", "a_test.go"} {
		t.Run(strings.ToLower(p), func(t *testing.T) {
			if !Inert(p) {
				t.Fatal(p)
			}
		})
	}
	for _, p := range []string{"Dockerfile", "compose.yaml", "go.mod", "a.go", "schema.sql",
		"notes.txt", "requirements.txt", "constraints.txt", "CMakeLists.txt"} {
		t.Run(strings.ToLower(p), func(t *testing.T) {
			if Inert(p) {
				t.Fatal(p)
			}
		})
	}
}
