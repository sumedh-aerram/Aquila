package gitrev

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	maxDiffBytes    = 1 << 20
	maxChangedPrint = 20
)

// ErrNotGit means path is not inside a git work tree.
var ErrNotGit = fmt.Errorf("gitrev: not a git work tree")

// State reports HEAD and whether path has uncommitted changes.
// An unborn HEAD has an empty SHA. Outside a git work tree, dirty is also false.
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
		if !unbornHEAD(ctx, root) {
			return "", false, fmt.Errorf("gitrev: rev-parse: %w", err)
		}
	} else {
		sha = strings.TrimSpace(string(shaOut))
	}
	stOut, err := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain", "--untracked-files=all", "--", rel).Output()
	if err != nil {
		return sha, false, fmt.Errorf("gitrev: status: %w", err)
	}
	return sha, len(bytes.TrimSpace(stOut)) > 0, nil
}

func unbornHEAD(ctx context.Context, root string) bool {
	ref, err := exec.CommandContext(ctx, "git", "-C", root, "symbolic-ref", "--quiet", "HEAD").Output()
	if err != nil {
		return false
	}
	err = exec.CommandContext(ctx, "git", "-C", root, "show-ref", "--verify", "--quiet", strings.TrimSpace(string(ref))).Run()
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1
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

func porcelainPaths(root string, out []byte) []string {
	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		p := porcelainPath(line)
		if p == "" {
			continue
		}
		paths = append(paths, p)
		if len(paths) >= maxChangedPrint {
			break
		}
	}
	return paths
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

func showToplevel(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
