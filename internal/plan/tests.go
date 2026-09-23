package plan

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sumedhaerram/aquila/internal/replay"
)

// WithTests adds a required tests step when dir is a Go module that contains
// packages from files. It does not invent tests for a Python checkout.
func WithTests(dag DAG, dir string, files []string) DAG {
	root, err := filepath.Abs(dir)
	if err != nil {
		return dag
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		dag.Notes = append(append([]string(nil), dag.Notes...), "tests omitted: no go.mod")
		return dag
	}
	pkgs := testPackages(root, files)
	if len(pkgs) == 0 {
		dag.Notes = append(append([]string(nil), dag.Notes...), "tests omitted: no go packages in impact")
		return dag
	}
	if hasKind(dag, KindTests) {
		dag.Module = root
		dag.TestPackages = pkgs
		return dag
	}
	steps := append([]Step(nil), dag.Steps...)
	steps = append(steps, Step{
		ID:       "tests",
		Kind:     KindTests,
		Required: true,
		Reason:   "go test of impact packages in the current module; not a two-revision compare",
	})
	notes := append([]string(nil), dag.Notes...)
	notes = append(notes, "impacted tests are of the current module, not baseline vs patch")
	dag.Steps = steps
	dag.Notes = notes
	dag.Module = root
	dag.TestPackages = pkgs
	return dag
}

func testPackages(root string, files []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, f := range files {
		p := filepath.ToSlash(f)
		if !strings.HasSuffix(p, ".go") || strings.Contains(p, "/testdata/") {
			continue
		}
		dir := pathDir(p)
		if !safeRel(dir) {
			continue
		}
		pkg := "."
		if dir != "." && dir != "" {
			pkg = "./" + dir
		}
		abs := root
		if dir != "." && dir != "" {
			abs = filepath.Join(root, filepath.FromSlash(dir))
		}
		if !hasGoFiles(abs) {
			continue
		}
		if _, ok := seen[pkg]; ok {
			continue
		}
		seen[pkg] = struct{}{}
		out = append(out, pkg)
	}
	return out
}

func pathDir(p string) string {
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return "."
	}
	return p[:i]
}

func safeRel(dir string) bool {
	if dir == "." || dir == "" {
		return true
	}
	if strings.HasPrefix(dir, "/") || strings.Contains(dir, "..") || strings.Contains(dir, "\\") {
		return false
	}
	return true
}

func hasGoFiles(dir string) bool {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			return true
		}
	}
	return false
}

func runTests(ctx context.Context, dag DAG) StepResult {
	out := StepResult{ID: "tests", Kind: KindTests}
	if strings.TrimSpace(dag.Module) == "" || len(dag.TestPackages) == 0 {
		out.Verdict = replay.VerdictIncomplete
		out.Notes = []string{"no test packages"}
		return out
	}
	args := []string{"test", "-count=1", "-timeout", "60s"}
	for _, p := range dag.TestPackages {
		if p != "." && !strings.HasPrefix(p, "./") {
			out.Verdict = replay.VerdictIncomplete
			out.Notes = []string{"invalid package"}
			return out
		}
		if strings.Contains(p, "..") {
			out.Verdict = replay.VerdictIncomplete
			out.Notes = []string{"invalid package"}
			return out
		}
		args = append(args, p)
	}
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dag.Module
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	note := clipBytes(buf.Bytes(), 512)
	if err != nil {
		if ctx.Err() != nil {
			out.Verdict = replay.VerdictIncomplete
			out.Notes = []string{"tests canceled"}
			return out
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() > 0 {
			out.Verdict = replay.VerdictDiffer
			out.Notes = []string{note}
			return out
		}
		out.Verdict = replay.VerdictIncomplete
		if note != "" {
			out.Notes = []string{note}
		} else {
			out.Notes = []string{err.Error()}
		}
		return out
	}
	out.Verdict = VerdictPrepared
	out.Notes = []string{"go test"}
	return out
}

func clipBytes(raw []byte, n int) string {
	s := strings.TrimSpace(string(raw))
	if len(s) <= n {
		return s
	}
	return s[:n]
}
