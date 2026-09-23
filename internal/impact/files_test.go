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
