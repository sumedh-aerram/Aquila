package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/sumedhaerram/aquila/internal/diff"
	"github.com/sumedhaerram/aquila/internal/gitrev"
)

func slurpDiff(stdin io.Reader, path string) ([]byte, error) {
	raw, _, err := resolveDiff(context.Background(), stdin, path, "")
	return raw, err
}

func resolveDiff(ctx context.Context, stdin io.Reader, path, dir string) ([]byte, string, error) {
	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, "file", err
		}
		if err := checkDiffBytes(raw); err != nil {
			return nil, "file", err
		}
		return raw, "file", nil
	}
	if !stdinInteractive(stdin) {
		raw, err := io.ReadAll(io.LimitReader(stdin, int64(diff.MaxBytes)+1))
		if err != nil {
			return nil, "stdin", err
		}
		if len(raw) > 0 {
			if err := checkDiffBytes(raw); err != nil {
				return nil, "stdin", err
			}
			return raw, "stdin", nil
		}
	}
	if dir == "" {
		return nil, "worktree", fmt.Errorf("empty diff")
	}
	raw, err := gitrev.UnifiedDiff(ctx, dir)
	if err != nil {
		if errors.Is(err, gitrev.ErrNotGit) {
			return nil, "worktree", fmt.Errorf("no local changes (not a git work tree); pass -f or a unified diff on stdin")
		}
		return nil, "worktree", err
	}
	if len(raw) == 0 {
		return nil, "worktree", fmt.Errorf("no local changes")
	}
	if err := checkDiffBytes(raw); err != nil {
		return nil, "worktree", err
	}
	return raw, "worktree", nil
}

func checkDiffBytes(raw []byte) error {
	if len(raw) == 0 {
		return fmt.Errorf("empty diff")
	}
	if len(raw) > diff.MaxBytes {
		return fmt.Errorf("diff too large")
	}
	return nil
}

func stdinInteractive(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

func diffPath(file string, rest []string) (string, error) {
	if file != "" && len(rest) != 0 {
		return "", fmt.Errorf("unexpected argument %q", rest[0])
	}
	if file != "" {
		return file, nil
	}
	if len(rest) == 1 {
		return rest[0], nil
	}
	if len(rest) > 1 {
		return "", fmt.Errorf("unexpected argument %q", rest[1])
	}
	return "", nil
}
