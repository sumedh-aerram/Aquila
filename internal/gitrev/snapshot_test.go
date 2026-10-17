package gitrev

import (
	"bytes"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func openRepo(t *testing.T, dir string) Repo {
	t.Helper()
	r, err := Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func writeFile(t *testing.T, dir, name string, raw []byte) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func snapshot(t *testing.T, r Repo) Revision {
	t.Helper()
	rev, err := r.SnapshotWorktree(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return rev
}

func TestSnapshotUntrackedRealContent(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, true)
	raw := []byte("package p\nfunc F() {}\nfunc G() {}\n")
	writeFile(t, dir, "x.go", raw)
	r := openRepo(t, dir)
	rev := snapshot(t, r)
	got, err := r.ReadBlob(t.Context(), rev.Tree, "x.go")
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("blob=%q err=%v", got, err)
	}
	if !rev.Dirty {
		t.Fatal("untracked snapshot must differ from HEAD")
	}
}

func TestSnapshotRespectsGitignore(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, true)
	writeFile(t, dir, ".gitignore", []byte(".env\n"))
	writeFile(t, dir, ".env", []byte("private"))
	r := openRepo(t, dir)
	rev := snapshot(t, r)
	if _, err := r.ReadBlob(t.Context(), rev.Tree, ".env"); err == nil {
		t.Fatal("ignored file retained")
	}
}

func TestSnapshotSubdir(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, true)
	writeFile(t, dir, "sub/a.go", []byte("package p\n"))
	writeFile(t, dir, "outside", []byte("head"))
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "-c", "commit.gpgsign=false", "commit", "-qm", "files")
	writeFile(t, dir, "sub/a.go", []byte("package p\nfunc F() {}\n"))
	writeFile(t, dir, "outside", []byte("dirty"))
	r := openRepo(t, filepath.Join(dir, "sub"))
	rev := snapshot(t, r)
	got, err := r.ReadBlob(t.Context(), rev.Tree, "a.go")
	if err != nil || !bytes.Contains(got, []byte("func F")) {
		t.Fatalf("%q %v", got, err)
	}
	cmd := exec.CommandContext(t.Context(), "git", "-C", dir, "show", rev.Commit+":outside")
	raw, err := cmd.Output()
	if err != nil || string(raw) != "head" {
		t.Fatalf("outside=%q err=%v", raw, err)
	}
}

func TestSnapshotDeletion(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, true)
	writeFile(t, dir, "x.go", []byte("package p\n"))
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "-c", "commit.gpgsign=false", "commit", "-qm", "file")
	if err := os.Remove(filepath.Join(dir, "x.go")); err != nil {
		t.Fatal(err)
	}
	r := openRepo(t, dir)
	rev := snapshot(t, r)
	if _, err := r.ReadBlob(t.Context(), rev.Tree, "x.go"); err == nil {
		t.Fatal("deleted file retained")
	}
}

func TestSnapshotRefusesSubmoduleSymlink(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"symlink", "submodule"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			dir := stateRepo(t, true)
			if kind == "symlink" {
				if err := os.Symlink("target", filepath.Join(dir, "link")); err != nil {
					t.Fatal(err)
				}
			} else {
				cmd := exec.CommandContext(t.Context(), "git", "-C", dir, "rev-parse", "HEAD")
				raw, err := cmd.Output()
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(dir, "module"), 0o755); err != nil {
					t.Fatal(err)
				}
				runGit(t, filepath.Join(dir, "module"), "init", "-q")
				runGit(t, filepath.Join(dir, "module"), "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "nested")
				runGit(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+strings.TrimSpace(string(raw))+",module")
				runGit(t, dir, "-c", "commit.gpgsign=false", "commit", "-qm", "submodule")
			}
			_, err := openRepo(t, dir).SnapshotWorktree(t.Context())
			if err == nil || !strings.Contains(err.Error(), "unsupported entry") {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestSnapshotUntrackedCap(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, true)
	file, err := os.Create(filepath.Join(dir, "huge"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxCopyBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := openRepo(t, dir).SnapshotWorktree(t.Context()); err == nil || !strings.Contains(err.Error(), "exceed cap") {
		t.Fatalf("%v", err)
	}
}

func TestSnapshotNoCommits(t *testing.T) {
	t.Parallel()
	if _, err := openRepo(t, stateRepo(t, false)).SnapshotWorktree(t.Context()); err == nil || !strings.Contains(err.Error(), "no commits") {
		t.Fatalf("%v", err)
	}
}

func TestSnapshotDoesNotTouchIndex(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, true)
	writeFile(t, dir, "a.go", []byte("package p\n"))
	runGit(t, dir, "add", "a.go")
	before, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot(t, openRepo(t, dir))
	after, err := os.ReadFile(filepath.Join(dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("real index changed")
	}
}

func TestSnapshotDiffApplies(t *testing.T) {
	t.Parallel()
	r := openRepo(t, filepath.Join("..", "..", "examples", "shop"))
	raw, err := os.ReadFile("../pair/testdata/d1.diff")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		raw  []byte
	}{
		{
			name: "repository relative",
			raw:  raw,
		},
		{
			name: "module relative",
			raw:  bytes.ReplaceAll(raw, []byte("examples/shop/"), nil),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rev, err := r.SnapshotDiff(t.Context(), tc.raw)
			if err != nil {
				t.Fatal(err)
			}
			body, err := r.ReadBlob(t.Context(), rev.Tree, "internal/payment/handler.go")
			if err != nil || !bytes.Contains(body, []byte("client := svcclient.Shared()")) {
				t.Fatalf("patch missing: %v", err)
			}
		})
	}
}

func TestSnapshotDiffMismatch(t *testing.T) {
	t.Parallel()
	r := openRepo(t, stateRepo(t, true))
	raw := []byte("--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-missing\n+new\n")
	if _, err := r.SnapshotDiff(t.Context(), raw); err == nil {
		t.Fatal("mismatch accepted")
	}
}

func TestPinSurvivesGC(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, true)
	writeFile(t, dir, "x.go", []byte("package p\n"))
	r := openRepo(t, dir)
	rev := snapshot(t, r)
	if err := r.Pin(t.Context(), rev); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "gc", "--prune=now")
	cmd := exec.CommandContext(t.Context(), "git", "-C", dir, "rev-parse", "refs/aquila/snapshots/"+rev.Tree+"^{tree}")
	raw, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(raw)) != rev.Tree {
		t.Fatalf("ref=%s err=%v", raw, err)
	}
}

func TestMaterializeExact(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, true)
	for name, body := range map[string]string{".gitattributes": "hidden export-ignore\nversion export-subst\n", "hidden": "retained\x00bytes", "version": "$Format:%H$", "run": "#!/bin/sh\necho ok\n"} {
		writeFile(t, dir, name, []byte(body))
	}
	if err := os.Chmod(filepath.Join(dir, "run"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := openRepo(t, dir)
	rev := snapshot(t, r)
	dst := filepath.Join(t.TempDir(), "tree")
	if err := r.Materialize(t.Context(), rev.Tree, dst); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{".gitattributes", "hidden", "version", "run"} {
		want, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dst, name))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("%s: %q %v", name, got, err)
		}
		info, err := os.Stat(filepath.Join(dst, name))
		if err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if name == "run" {
			mode = 0o755
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("%s mode=%v", name, info.Mode())
		}
	}
}

func TestMaterializeRejectsTraversal(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, true)
	r := openRepo(t, dir)
	blob, err := r.run(t.Context(), "", []byte("escape"), "hash-object", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	// A raw tree bypasses mktree's pathname validation.
	raw := append([]byte("100644 ../escape\x00"), mustDecodeHex(t, strings.TrimSpace(string(blob)))...)
	tree, err := r.run(t.Context(), "", raw, "hash-object", "--literally", "-t", "tree", "-w", "--stdin")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Materialize(t.Context(), strings.TrimSpace(string(tree)), filepath.Join(t.TempDir(), "tree")); err == nil {
		t.Fatal("traversal accepted")
	}
}

func mustDecodeHex(t *testing.T, s string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestSnapshotRefusesFilters(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, true)
	writeFile(t, dir, ".gitattributes", []byte("*.go filter=lfs\n"))
	writeFile(t, dir, "x.go", []byte("package p\n"))
	if _, err := openRepo(t, dir).SnapshotWorktree(t.Context()); err == nil || !strings.Contains(err.Error(), "filters unsupported") {
		t.Fatalf("%v", err)
	}
}

func TestSnapshotDiffRefusesFilters(t *testing.T) {
	t.Parallel()
	r := openRepo(t, stateRepo(t, true))
	raw := []byte("--- /dev/null\n+++ b/.gitattributes\n@@ -0,0 +1 @@\n+*.go filter=aquila-unsupported\n--- /dev/null\n+++ b/x.go\n@@ -0,0 +1 @@\n+package p\n")
	if _, err := r.SnapshotDiff(t.Context(), raw); err == nil || !strings.Contains(err.Error(), "filters unsupported") {
		t.Fatalf("%v", err)
	}
}

func TestSnapshotRefusesContentConversion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		config string
		value  string
		attr   string
		want   string
	}{
		{
			name:   "autocrlf true",
			config: "core.autocrlf",
			value:  "true",
			want:   "core.autocrlf=true",
		},
		{
			name:   "autocrlf input",
			config: "core.autocrlf",
			value:  "input",
			want:   "core.autocrlf=input",
		},
		{
			name:   "sparse checkout",
			config: "core.sparseCheckout",
			value:  "true",
			want:   "core.sparseCheckout=true",
		},
		{
			name: "eol crlf",
			attr: "*.go eol=crlf",
			want: "eol=crlf",
		},
		{
			name: "text",
			attr: "*.go text",
			want: "text=set",
		},
		{
			name: "ident",
			attr: "*.go ident",
			want: "ident=set",
		},
		{
			name: "encoding",
			attr: "*.go working-tree-encoding=UTF-16",
			want: "working-tree-encoding=UTF-16",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := stateRepo(t, true)
			writeFile(t, dir, "x.go", []byte("package p\n"))
			if tc.config != "" {
				runGit(t, dir, "config", tc.config, tc.value)
			}
			if tc.attr != "" {
				writeFile(t, dir, ".gitattributes", []byte(tc.attr+"\n"))
			}
			_, err := openRepo(t, dir).SnapshotWorktree(t.Context())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error=%v, want %s", err, tc.want)
			}
		})
	}
}

func TestSnapshotUsesResolvedBaseline(t *testing.T) {
	t.Parallel()
	dir := stateRepo(t, true)
	r := openRepo(t, dir)
	base, err := r.Head(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "later", []byte("new HEAD\n"))
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "-c", "commit.gpgsign=false", "commit", "-qm", "later")
	patch, err := r.SnapshotDiffAt(t.Context(), base, []byte("--- /dev/null\n+++ b/x.go\n@@ -0,0 +1 @@\n+package p\n"))
	if err != nil {
		t.Fatal(err)
	}
	parent, err := r.run(t.Context(), "", nil, "rev-parse", patch.Commit+"^")
	if err != nil || strings.TrimSpace(string(parent)) != base.Commit {
		t.Fatalf("parent=%s error=%v", parent, err)
	}
	if _, err := r.ReadBlob(t.Context(), patch.Tree, "later"); err == nil {
		t.Fatal("snapshot used later HEAD")
	}
	if !patch.Dirty {
		t.Fatal("patch must differ from captured baseline")
	}
}

func TestRunIncludesGitError(t *testing.T) {
	t.Parallel()
	r := openRepo(t, stateRepo(t, true))
	_, err := r.run(t.Context(), "", nil, "-c", "user.name=aquila", "commit-tree", "missing")
	if err == nil || !strings.Contains(err.Error(), "gitrev: commit-tree:") || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error=%v", err)
	}
}

func TestPlainDiffWithoutBuildSnapshot(t *testing.T) {
	t.Parallel()
	for _, committed := range []bool{false, true} {
		name := "unborn"
		if committed {
			name = "symlink"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := stateRepo(t, committed)
			if err := os.Symlink("target", filepath.Join(dir, "link")); err != nil {
				t.Fatal(err)
			}
			raw, err := openRepo(t, dir).PlainDiff(t.Context())
			if err != nil || !bytes.Contains(raw, []byte("+target")) {
				t.Fatalf("diff=%s error=%v", raw, err)
			}
		})
	}
}
