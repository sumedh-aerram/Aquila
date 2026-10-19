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
	raw, _, err := resolveDiff(context.Background(), stdin, diffOptions{path: path})
	return raw, err
}

type diffOptions struct {
	path    string
	dir     string
	notices io.Writer
}

func resolveDiff(ctx context.Context, stdin io.Reader, opts diffOptions) ([]byte, string, error) {
	raw, origin, err := readDiff(stdin, opts.path)
	if err != nil || origin != "worktree" {
		return raw, origin, err
	}
	if opts.dir == "" {
		return nil, "worktree", fmt.Errorf("empty diff")
	}
	repo, err := gitrev.Open(ctx, opts.dir)
	raw = nil
	if err == nil {
		base, headErr := repo.Head(ctx)
		err = headErr
		if err == nil {
			patch, snapErr := repo.SnapshotWorktreeAt(ctx, base)
			err = snapErr
			if err == nil {
				raw, err = repo.DiffTrees(ctx, base.Commit, patch.Commit)
			}
		}
		if err != nil {
			if opts.notices != nil {
				writef(opts.notices, "diff       plain fallback: %v\n", err)
			}
			raw, err = repo.PlainDiff(ctx)
		}
	}
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

func readDiff(stdin io.Reader, path string) ([]byte, string, error) {
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
	return nil, "worktree", nil
}
