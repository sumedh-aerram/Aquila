package gitrev

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	maxDiffBytes    = 1 << 20
	maxUntracked    = 50
	maxChangedPrint = 20
)

// ErrNotGit means path is not inside a git work tree.
var ErrNotGit = fmt.Errorf("gitrev: not a git work tree")

// State reports HEAD and whether path has uncommitted changes.
// If path is not inside a git work tree, sha is empty and dirty is false.
func State(ctx context.Context, path string) (sha string, dirty bool, err error) {
	root, rel, err := locate(ctx, path)
	if err != nil {
		if err == ErrNotGit {
			return "", false, nil
		}
		return "", false, err
	}
	shaOut, err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", false, nil
	}
	sha = strings.TrimSpace(string(shaOut))
	stOut, err := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain", "--untracked-files=all", "--", rel).Output()
	if err != nil {
		return sha, false, nil
	}
	return sha, len(sourcePorcelain(root, stOut)) > 0, nil
}

// ChangedPaths lists worktree paths that differ from HEAD, including untracked files.
func ChangedPaths(ctx context.Context, path string) ([]string, error) {
	root, rel, err := locate(ctx, path)
	if err != nil {
		if err == ErrNotGit {
			return nil, nil
		}
		return nil, err
	}
	stOut, err := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain", "--untracked-files=all", "--", rel).Output()
	if err != nil {
		return nil, fmt.Errorf("gitrev: status: %w", err)
	}
	return porcelainPaths(root, stOut), nil
}

// UnifiedDiff returns git diff HEAD for path, plus stub hunks for untracked files.
// A missing git tree returns ErrNotGit. A clean tree returns a nil slice.
func UnifiedDiff(ctx context.Context, path string) ([]byte, error) {
	root, rel, err := locate(ctx, path)
	if err != nil {
		return nil, err
	}
	tracked, err := exec.CommandContext(ctx, "git", "-C", root, "diff", "HEAD", "--", rel).Output()
	if err != nil {
		return nil, fmt.Errorf("gitrev: diff: %w", err)
	}
	var buf bytes.Buffer
	if len(bytes.TrimSpace(tracked)) > 0 {
		buf.Write(tracked)
		if tracked[len(tracked)-1] != '\n' {
			buf.WriteByte('\n')
		}
	}
	stOut, err := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain", "--untracked-files=all", "--", rel).Output()
	if err != nil {
		return nil, fmt.Errorf("gitrev: status: %w", err)
	}
	nUntracked := 0
	for _, p := range porcelainUntracked(root, stOut) {
		if nUntracked >= maxUntracked {
			break
		}
		nUntracked++
		stub := stubNewFile(p)
		if buf.Len()+len(stub) > maxDiffBytes {
			return nil, fmt.Errorf("gitrev: diff exceeds %d bytes", maxDiffBytes)
		}
		buf.Write(stub)
	}
	if buf.Len() > maxDiffBytes {
		return nil, fmt.Errorf("gitrev: diff exceeds %d bytes", maxDiffBytes)
	}
	if buf.Len() == 0 {
		return nil, nil
	}
	return buf.Bytes(), nil
}

func locate(ctx context.Context, path string) (root, rel string, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("gitrev: %w", err)
	}
	if !st.IsDir() {
		return "", "", fmt.Errorf("gitrev: not a directory")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("gitrev: %w", err)
	}
	if resolved, resErr := filepath.EvalSymlinks(abs); resErr == nil {
		abs = resolved
	}
	root = showToplevel(ctx, abs)
	if root == "" {
		return "", "", ErrNotGit
	}
	if resolved, resErr := filepath.EvalSymlinks(root); resErr == nil {
		root = resolved
	}
	rel = "."
	if r, relErr := filepath.Rel(root, abs); relErr == nil && r != "" {
		rel = filepath.ToSlash(r)
	}
	if rel != "." && (rel == ".." || strings.HasPrefix(rel, "../")) {
		return "", "", ErrNotGit
	}
	return root, rel, nil
}

func sourcePorcelain(root string, out []byte) []string {
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		p := porcelainPath(line)
		if p == "" || skipWorktree(root, p) {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func porcelainPaths(root string, out []byte) []string {
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		p := porcelainPath(line)
		if p == "" || skipWorktree(root, p) {
			continue
		}
		paths = append(paths, p)
		if len(paths) >= maxChangedPrint {
			break
		}
	}
	return paths
}

func porcelainUntracked(root string, out []byte) []string {
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "?? ") {
			continue
		}
		p := porcelainPath(line)
		if p == "" || skipWorktree(root, p) {
			continue
		}
		paths = append(paths, p)
	}
	return paths
}

func skipWorktree(root, rel string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" || strings.Contains(rel, "..") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(rel))
	switch ext {
	case ".exe", ".so", ".dylib", ".a", ".o", ".bin", ".png", ".jpg", ".jpeg", ".gif", ".webp", ".pdf", ".zip", ".tar", ".gz", ".wasm", ".class", ".jar":
		return true
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	st, err := os.Lstat(full)
	if err != nil {
		return false
	}
	if st.Mode()&os.ModeSymlink != 0 || st.IsDir() {
		return false
	}
	if ext == "" && st.Mode()&0o111 != 0 {
		return true
	}
	f, err := os.Open(full)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	var buf [512]byte
	n, _ := f.Read(buf[:])
	return bytes.IndexByte(buf[:n], 0) >= 0
}

func porcelainPath(line string) string {
	if len(line) < 4 {
		return ""
	}
	rest := strings.TrimSpace(line[3:])
	if i := strings.Index(rest, " -> "); i >= 0 {
		rest = rest[i+4:]
	}
	rest = strings.Trim(rest, `"`)
	if rest == "" || strings.Contains(rest, "..") {
		return ""
	}
	return filepath.ToSlash(rest)
}

func stubNewFile(path string) []byte {
	p := filepath.ToSlash(path)
	return []byte("diff --git a/" + p + " b/" + p + "\nnew file mode 100644\n--- /dev/null\n+++ b/" + p + "\n@@ -0,0 +1 @@\n+\n")
}

func showToplevel(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
