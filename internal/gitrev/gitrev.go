package gitrev

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// State reports HEAD and whether path has uncommitted changes.
// If path is not inside a git work tree, sha is empty and dirty is false.
func State(ctx context.Context, path string) (sha string, dirty bool, err error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", false, fmt.Errorf("gitrev: %w", err)
	}
	if !st.IsDir() {
		return "", false, fmt.Errorf("gitrev: not a directory")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false, fmt.Errorf("gitrev: %w", err)
	}
	if resolved, resErr := filepath.EvalSymlinks(abs); resErr == nil {
		abs = resolved
	}
	root := showToplevel(ctx, abs)
	if root == "" {
		return "", false, nil
	}
	if resolved, resErr := filepath.EvalSymlinks(root); resErr == nil {
		root = resolved
	}
	shaOut, err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", false, nil
	}
	sha = strings.TrimSpace(string(shaOut))
	rel := "."
	if r, relErr := filepath.Rel(root, abs); relErr == nil && r != "" {
		rel = filepath.ToSlash(r)
	}
	if rel != "." && (rel == ".." || strings.HasPrefix(rel, "../")) {
		return sha, false, nil
	}
	stOut, err := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain", "--untracked-files=all", "--", rel).Output()
	if err != nil {
		return sha, false, nil
	}
	return sha, strings.TrimSpace(string(stOut)) != "", nil
}

func showToplevel(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
