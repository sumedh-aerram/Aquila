package gitrev

import (
	"os"
	"os/exec"
	"path/filepath"
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
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "-c", "user.email=aquila@test", "-c", "user.name=aquila", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-q", "-m", "init")
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
