package source

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

const modulePattern = "./..."

// Load type-checks a Go module at dir and returns a traversable source graph.
// Only packages in that module are included. Call edges require a typed
// callee in-module; interface dispatch and missing types are omitted.
func Load(ctx context.Context, dir string) (*Graph, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("source: abs %s: %w", dir, err)
	}
	st, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("source: %s is not a directory", root)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return nil, fmt.Errorf("source: %s: go.mod: %w", root, err)
	}

	fset := token.NewFileSet()
	cfg := &packages.Config{
		Context: ctx,
		Dir:     root,
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedCompiledGoFiles |
			packages.NeedImports |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedSyntax |
			packages.NeedModule,
		Tests: false,
		Fset:  fset,
		Env:   loadEnv(),
	}
	pkgs, err := packages.Load(cfg, modulePattern)
	if err != nil {
		return nil, fmt.Errorf("source: load: %w", err)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("source: no packages in %s", root)
	}

	var loadErrs []error
	modulePath := ""
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			loadErrs = append(loadErrs, errors.New(e.Error()))
		}
		if modulePath == "" && pkg.Module != nil && pkg.Module.Path != "" {
			modulePath = pkg.Module.Path
		}
	}
	if len(loadErrs) > 0 {
		return nil, fmt.Errorf("source: load: %w", errors.Join(loadErrs...))
	}
	if modulePath == "" {
		return nil, fmt.Errorf("source: module path missing in %s", root)
	}

	b := newBuilder(modulePath, root, fset)
	for _, pkg := range pkgs {
		if pkg == nil || !inModule(pkg.PkgPath, modulePath) {
			continue
		}
		if err := b.addPackage(pkg); err != nil {
			return nil, err
		}
	}
	if b.funcs == 0 {
		return nil, fmt.Errorf("source: no functions in %s", modulePath)
	}
	return b.graph()
}

func loadEnv() []string {
	out := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GOTOOLCHAIN=") || strings.HasPrefix(kv, "CGO_ENABLED=") {
			continue
		}
		out = append(out, kv)
	}
	// Inherited GOTOOLCHAIN can be older than the target module's go line (shop is 1.25.6).
	return append(out, "CGO_ENABLED=0", "GOTOOLCHAIN=auto")
}

func inModule(pkgPath, modulePath string) bool {
	return pkgPath == modulePath || strings.HasPrefix(pkgPath, modulePath+"/")
}

type builder struct {
	module string
	root   string
	fset   *token.FileSet
	nodes  []Node
	edges  []Edge
	seenN  map[string]struct{}
	seenE  map[string]struct{}
	funcs  int
}

func newBuilder(module, root string, fset *token.FileSet) *builder {
	return &builder{
		module: module,
		root:   root,
		fset:   fset,
		seenN:  make(map[string]struct{}),
		seenE:  make(map[string]struct{}),
	}
}

func (b *builder) addPackage(pkg *packages.Package) error {
	if pkg.Types == nil || pkg.TypesInfo == nil {
		return fmt.Errorf("source: package %s missing types", pkg.PkgPath)
	}
	name := pkg.Name
	if name == "" {
		name = pkg.PkgPath
	}
	if err := b.node(Node{
		ID:   pkgID(pkg.PkgPath),
		Kind: KindPackage,
		Name: name,
		Pkg:  pkg.PkgPath,
	}); err != nil {
		return err
	}
	for importPath, ip := range pkg.Imports {
		if ip == nil || !inModule(importPath, b.module) {
			continue
		}
		iname := ip.Name
		if iname == "" {
			iname = importPath
		}
		if err := b.node(Node{ID: pkgID(importPath), Kind: KindPackage, Name: iname, Pkg: importPath}); err != nil {
			return err
		}
		if err := b.edge(pkgID(pkg.PkgPath), pkgID(importPath), EdgeImports, ProvenanceTypes); err != nil {
			return err
		}
	}

	for i, file := range pkg.Syntax {
		abs := ""
		if i < len(pkg.CompiledGoFiles) {
			abs = pkg.CompiledGoFiles[i]
		}
		rel, err := relFile(b.root, abs)
		if err != nil {
			continue
		}
		fid := fileID(rel)
		if err := b.node(Node{
			ID:   fid,
			Kind: KindFile,
			Name: path.Base(rel),
			Pkg:  pkg.PkgPath,
			File: rel,
		}); err != nil {
			return err
		}
		if err := b.edge(pkgID(pkg.PkgPath), fid, EdgeContains, ProvenanceSyntax); err != nil {
			return err
		}
		if err := b.walkFile(pkg, file, fid); err != nil {
			return err
		}
	}
	return nil
}

func (b *builder) walkFile(pkg *packages.Package, file *ast.File, fid string) error {
	var walkErr error
	ast.Inspect(file, func(n ast.Node) bool {
		if walkErr != nil {
			return false
		}
		fn, ok := n.(*ast.FuncDecl)
		if !ok {
			return true
		}
		if fn.Name == nil {
			return false
		}
		obj := pkg.TypesInfo.Defs[fn.Name]
		tf, ok := obj.(*types.Func)
		if !ok {
			return false
		}
		fnNode, err := b.funcNode(tf)
		if err != nil {
			walkErr = err
			return false
		}
		if err := b.node(fnNode); err != nil {
			walkErr = err
			return false
		}
		b.funcs++
		if err := b.edge(fid, fnNode.ID, EdgeContains, ProvenanceSyntax); err != nil {
			walkErr = err
			return false
		}
		if fn.Body == nil {
			return false
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if walkErr != nil {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee, ok := typeutil.Callee(pkg.TypesInfo, call).(*types.Func)
			if !ok || callee.Pkg() == nil || !inModule(callee.Pkg().Path(), b.module) {
				return true
			}
			cnode, err := b.funcNode(callee)
			if err != nil {
				walkErr = err
				return false
			}
			if err := b.node(cnode); err != nil {
				walkErr = err
				return false
			}
			if err := b.edge(fnNode.ID, cnode.ID, EdgeCalls, ProvenanceTypes); err != nil {
				walkErr = err
				return false
			}
			return true
		})
		return false
	})
	return walkErr
}

func (b *builder) funcNode(fn *types.Func) (Node, error) {
	if fn.Pkg() == nil {
		return Node{}, fmt.Errorf("source: function %s has no package", fn.FullName())
	}
	pos := b.fset.Position(fn.Pos())
	rel, err := relFile(b.root, pos.Filename)
	if err != nil {
		rel = ""
	}
	return Node{
		ID:   funcID(fn.FullName()),
		Kind: KindFunction,
		Name: fn.Name(),
		Pkg:  fn.Pkg().Path(),
		File: rel,
		Line: pos.Line,
	}, nil
}

func (b *builder) node(n Node) error {
	if n.ID == "" {
		return fmt.Errorf("source: empty node id")
	}
	if _, ok := b.seenN[n.ID]; ok {
		return nil
	}
	if len(b.nodes)+1 > maxNodes {
		return fmt.Errorf("source: node cap %d exceeded", maxNodes)
	}
	b.seenN[n.ID] = struct{}{}
	b.nodes = append(b.nodes, n)
	return nil
}

func (b *builder) edge(from, to, kind, provenance string) error {
	if from == "" || to == "" || from == to {
		return nil
	}
	key := from + "\x00" + to + "\x00" + kind
	if _, ok := b.seenE[key]; ok {
		return nil
	}
	if len(b.edges)+1 > maxEdges {
		return fmt.Errorf("source: edge cap %d exceeded", maxEdges)
	}
	b.seenE[key] = struct{}{}
	b.edges = append(b.edges, Edge{From: from, To: to, Kind: kind, Provenance: provenance})
	return nil
}

func (b *builder) graph() (*Graph, error) {
	sort.Slice(b.nodes, func(i, j int) bool { return b.nodes[i].ID < b.nodes[j].ID })
	sort.Slice(b.edges, func(i, j int) bool {
		if b.edges[i].From != b.edges[j].From {
			return b.edges[i].From < b.edges[j].From
		}
		if b.edges[i].To != b.edges[j].To {
			return b.edges[i].To < b.edges[j].To
		}
		return b.edges[i].Kind < b.edges[j].Kind
	})
	g := &Graph{module: b.module, nodes: b.nodes, edges: b.edges}
	g.index()
	return g, nil
}

func pkgID(pkgPath string) string {
	return "pkg:" + pkgPath
}

func fileID(rel string) string {
	return "file:" + rel
}

func funcID(fullName string) string {
	return "func:" + fullName
}

func relFile(root, abs string) (string, error) {
	if abs == "" {
		return "", fmt.Errorf("empty path")
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("outside module")
	}
	return rel, nil
}
