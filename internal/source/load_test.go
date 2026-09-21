package source

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sumedhaerram/aquila/internal/config"
)

var shopOnce = sync.OnceValues(func() (*Graph, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return Load(ctx, shopDir())
})

func shopDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "examples", "shop"))
}

func loadedShop(t *testing.T) *Graph {
	t.Helper()
	g, err := shopOnce()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestLoadShopContainsPaymentSymbols(t *testing.T) {
	t.Parallel()
	g := loadedShop(t)
	if g.Module() != "github.com/sumedhaerram/aquila/examples/shop" {
		t.Fatalf("module = %q", g.Module())
	}

	payPkg, ok := g.Node(pkgID("github.com/sumedhaerram/aquila/examples/shop/internal/payment"))
	if !ok || payPkg.Kind != KindPackage {
		t.Fatal("missing payment package")
	}
	authorize := findFunc(t, g, "/internal/payment", "authorize")
	charge := findFunc(t, g, "/internal/payment", "chargeProcessor")
	if authorize.File != "internal/payment/handler.go" {
		t.Fatalf("authorize file = %q", authorize.File)
	}

	if !hasEdge(g, authorize.ID, charge.ID, EdgeCalls) {
		t.Fatal("authorize should call chargeProcessor")
	}
	if !hasEdge(g, pkgID("github.com/sumedhaerram/aquila/examples/shop/internal/payment"), fileID("internal/payment/handler.go"), EdgeContains) {
		t.Fatal("payment package should contain handler.go")
	}
	if !reachableOut(g, payPkg.ID, charge.ID) {
		t.Fatal("payment package should reach chargeProcessor")
	}

	ephemeral := findFunc(t, g, "/internal/svcclient", "NewEphemeral")
	if !hasEdge(g, charge.ID, ephemeral.ID, EdgeCalls) {
		t.Fatal("chargeProcessor should call NewEphemeral")
	}
}

func TestLoadShopDoesNotInventHTTPAsCalls(t *testing.T) {
	t.Parallel()
	g := loadedShop(t)
	authorize := findFunc(t, g, "/internal/payment", "authorize")
	for _, n := range g.nodes {
		if n.Kind != KindFunction || n.Pkg == "" || !strings.Contains(n.Pkg, "/internal/checkout") {
			continue
		}
		if hasEdge(g, n.ID, authorize.ID, EdgeCalls) {
			t.Fatalf("checkout %s must not typed-call payment.authorize (that hop is HTTP)", n.ID)
		}
	}
}

func TestLoadShopImportsAreInModule(t *testing.T) {
	t.Parallel()
	g := loadedShop(t)
	from := pkgID("github.com/sumedhaerram/aquila/examples/shop/internal/checkout")
	to := pkgID("github.com/sumedhaerram/aquila/examples/shop/internal/svcclient")
	if !hasEdge(g, from, to, EdgeImports) {
		t.Fatal("checkout should import svcclient")
	}
	for _, e := range g.edges {
		if e.Kind != EdgeImports {
			continue
		}
		if !strings.HasPrefix(e.To, "pkg:"+g.Module()) {
			t.Fatalf("import edge escaped module: %+v", e)
		}
	}
}

func TestLoadRejectsMissingGoMod(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := Load(t.Context(), dir)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSnapshotRoundTripPreservesCalls(t *testing.T) {
	t.Parallel()
	g := loadedShop(t)
	path := filepath.Join(t.TempDir(), "source.json")
	if err := g.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.NodeCount() != g.NodeCount() {
		t.Fatalf("nodes %d != %d", got.NodeCount(), g.NodeCount())
	}
	authorize := findFunc(t, got, "/internal/payment", "authorize")
	charge := findFunc(t, got, "/internal/payment", "chargeProcessor")
	if !hasEdge(got, authorize.ID, charge.ID, EdgeCalls) {
		t.Fatal("round trip dropped authorize -> chargeProcessor")
	}
}

func TestFromSnapshotRejectsDanglingEdge(t *testing.T) {
	t.Parallel()
	_, err := FromSnapshot(Snapshot{
		Module: "example.com/demo",
		Nodes:  []Node{{ID: "pkg:example.com/demo", Kind: KindPackage, Name: "demo"}},
		Edges:  []Edge{{From: "pkg:example.com/demo", To: "pkg:missing", Kind: EdgeImports, Provenance: ProvenanceTypes}},
	})
	if err == nil {
		t.Fatal("expected dangling edge error")
	}
}

func TestOpenEmptyConfig(t *testing.T) {
	t.Parallel()
	g, err := Open(t.Context(), config.SourceConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if g != nil {
		t.Fatal("expected nil graph")
	}
}

func findFunc(t *testing.T, g *Graph, pkgSubstr, name string) Node {
	t.Helper()
	var found []Node
	for _, n := range g.nodes {
		if n.Kind == KindFunction && n.Name == name && strings.Contains(n.Pkg, pkgSubstr) {
			found = append(found, n)
		}
	}
	if len(found) != 1 {
		t.Fatalf("func %s in %s: got %d", name, pkgSubstr, len(found))
	}
	return found[0]
}

func hasEdge(g *Graph, from, to, kind string) bool {
	for _, e := range g.edges {
		if e.From == from && e.To == to && e.Kind == kind {
			return true
		}
	}
	return false
}

func reachableOut(g *Graph, start, want string) bool {
	seen := map[string]struct{}{start: {}}
	q := []string{start}
	for len(q) > 0 {
		id := q[0]
		q = q[1:]
		ns, ok := g.Neighbors(id)
		if !ok {
			continue
		}
		for _, n := range ns {
			if n.Direction != DirOut {
				continue
			}
			if n.Node.ID == want {
				return true
			}
			if _, dup := seen[n.Node.ID]; dup {
				continue
			}
			seen[n.Node.ID] = struct{}{}
			q = append(q, n.Node.ID)
		}
	}
	return false
}

func TestOpenReadsSnapshot(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "snap.json")
	g, err := FromSnapshot(Snapshot{
		Module: "example.com/demo",
		Nodes: []Node{
			{ID: "pkg:example.com/demo", Kind: KindPackage, Name: "demo", Pkg: "example.com/demo"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	got, err := Open(t.Context(), config.SourceConfig{Snapshot: path})
	if err != nil {
		t.Fatal(err)
	}
	if got.Module() != "example.com/demo" {
		t.Fatalf("module = %q", got.Module())
	}
}

func TestOpenMissingSnapshot(t *testing.T) {
	t.Parallel()
	_, err := Open(t.Context(), config.SourceConfig{Snapshot: filepath.Join(t.TempDir(), "missing.json")})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenPrefersDir(t *testing.T) {
	t.Parallel()
	g, err := Open(t.Context(), config.SourceConfig{Dir: shopDir(), Snapshot: filepath.Join(t.TempDir(), "ignored.json")})
	if err != nil {
		t.Fatal(err)
	}
	if g.Module() != "github.com/sumedhaerram/aquila/examples/shop" {
		t.Fatalf("module = %q", g.Module())
	}
}

func TestReadFileRejectsOversizedSnapshot(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "big.json")
	if err := os.WriteFile(path, make([]byte, maxSnapshotBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(path); err == nil {
		t.Fatal("expected error")
	}
}
