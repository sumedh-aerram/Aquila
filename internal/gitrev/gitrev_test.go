package gitrev

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStateMissingDir(t *testing.T) {
	t.Parallel()
	_, _, err := State(t.Context(), filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStateNotGit(t *testing.T) {
	t.Parallel()
	sha, dirty, err := State(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if sha != "" || dirty {
		t.Fatalf("sha=%q dirty=%v", sha, dirty)
	}
}

func TestStateHEADAndDirty(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, true)
	sha, dirty, err := State(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if sha == "" {
		t.Fatal("expected HEAD")
	}
	if dirty {
		t.Fatal("empty commit must be clean")
	}
	if err := os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha2, dirty, err := State(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if sha2 != sha {
		t.Fatalf("HEAD changed: %s vs %s", sha, sha2)
	}
	if !dirty {
		t.Fatal("untracked file must be dirty")
	}

	raw, err := UnifiedDiff(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("wip.txt")) {
		t.Fatalf("untracked missing from diff:\n%s", raw)
	}
	paths, err := ChangedPaths(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "wip.txt" {
		t.Fatalf("paths=%v", paths)
	}
}

func TestUnifiedDiffSkipsUntrackedBinary(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, true)
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ledger"), []byte{0x7f, 'E', 'L', 'F', 0}, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := UnifiedDiff(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("main.go")) {
		t.Fatalf("source missing:\n%s", raw)
	}
	if bytes.Contains(raw, []byte("ledger")) {
		t.Fatalf("binary leaked:\n%s", raw)
	}
	paths, err := ChangedPaths(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "ledger" || paths[1] != "main.go" {
		t.Fatalf("paths=%v", paths)
	}
}

func TestUnifiedDiffTrackedHunk(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, false)
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.go")
	runGit(t, dir, "-c", "user.email=aquila@test", "-c", "user.name=aquila", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "add")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\nfunc F() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := UnifiedDiff(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("func F()")) {
		t.Fatalf("%s", raw)
	}
}

func TestUnifiedDiffNotGit(t *testing.T) {
	t.Parallel()
	_, err := UnifiedDiff(t.Context(), t.TempDir())
	if err != ErrNotGit {
		t.Fatalf("err=%v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=aquila",
		"GIT_AUTHOR_EMAIL=aquila@test",
		"GIT_COMMITTER_NAME=aquila",
		"GIT_COMMITTER_EMAIL=aquila@test",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func stateRepo(t *testing.T, committed bool) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	if committed {
		runGit(t, dir, "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-q", "-m", "init")
	}
	return dir
}

func TestStateStatusError(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, true)
	if err := os.WriteFile(filepath.Join(dir, ".git", "index"), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := State(t.Context(), dir); err == nil {
		t.Fatal("corrupt index must fail")
	}
}

func TestStateDirtyBinary(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, true)
	p := filepath.Join(dir, "a.png")
	if err := os.WriteFile(p, []byte{0, 1}, 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.png")
	runGit(t, dir, "-c", "user.email=aquila@test", "-c", "user.name=aquila", "-c", "commit.gpgsign=false", "commit", "-qm", "binary")
	if err := os.WriteFile(p, []byte{0, 2}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, dirty, err := State(t.Context(), dir); err != nil || !dirty {
		t.Fatalf("dirty=%t err=%v", dirty, err)
	}
}

func TestUnifiedDiffUntrackedCap(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, true)
	for i := 0; i < 51; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%d.go", i)), []byte("package p"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := UnifiedDiff(t.Context(), dir); err == nil || !strings.Contains(err.Error(), "51 untracked files exceed cap 50") {
		t.Fatalf("%v", err)
	}
}

func TestStateUnbornHEAD(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		dirty, corrupt bool
	}{
		{name: "clean"},
		{name: "untracked", dirty: true},
		{name: "corrupt index", corrupt: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := stateRepo(t, false)
			if tc.dirty {
				if err := os.WriteFile(filepath.Join(dir, "new.go"), []byte("package p"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.corrupt {
				if err := os.WriteFile(filepath.Join(dir, ".git", "index"), []byte("garbage"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			sha, dirty, err := State(t.Context(), dir)
			if tc.corrupt {
				if err == nil {
					t.Fatal("corrupt unborn index accepted")
				}
				return
			}
			if err != nil || sha != "" || dirty != tc.dirty {
				t.Fatalf("sha=%q dirty=%t err=%v", sha, dirty, err)
			}
		})
	}
}

func TestStateRevisionError(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, true)
	raw, err := os.ReadFile(filepath.Join(dir, ".git", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	ref := strings.TrimSpace(strings.TrimPrefix(string(raw), "ref: "))
	if err := os.WriteFile(filepath.Join(dir, ".git", ref), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := State(t.Context(), dir); err == nil || !strings.Contains(err.Error(), "rev-parse") {
		t.Fatalf("corrupt revision must fail: %v", err)
	}
}
