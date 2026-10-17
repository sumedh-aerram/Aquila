package cli

import (
	"context"
	"io"

	"github.com/sumedhaerram/aquila/internal/gitrev"
)

func declaredRevisions(ctx context.Context, dir string) (gitrev.Revision, gitrev.Revision, error) {
	repo, err := gitrev.Open(ctx, dir)
	if err != nil {
		return gitrev.Revision{}, gitrev.Revision{}, nil
	}
	base, err := repo.Head(ctx)
	if err != nil {
		return base, gitrev.Revision{}, nil
	}
	patch, _ := repo.SnapshotWorktreeAt(ctx, base)
	return base, patch, nil
}

type buildSnapshot struct {
	Repo     gitrev.Repo
	Baseline gitrev.Revision
	Patch    gitrev.Revision
	Diff     []byte
}

func captureBuild(ctx context.Context, stdin io.Reader, path, dir string) (buildSnapshot, error) {
	var snap buildSnapshot
	raw, origin, err := readDiff(stdin, path)
	if err != nil {
		return snap, err
	}
	snap.Repo, err = gitrev.Open(ctx, dir)
	if err != nil {
		return snap, err
	}
	snap.Baseline, err = snap.Repo.Head(ctx)
	if err != nil {
		return snap, err
	}
	if origin == "worktree" {
		snap.Patch, err = snap.Repo.SnapshotWorktreeAt(ctx, snap.Baseline)
	} else {
		snap.Patch, err = snap.Repo.SnapshotDiffAt(ctx, snap.Baseline, raw)
	}
	if err != nil {
		return snap, err
	}
	snap.Diff, err = snap.Repo.DiffTrees(ctx, snap.Baseline.Commit, snap.Patch.Commit)
	if err != nil {
		return snap, err
	}
	if err := checkDiffBytes(snap.Diff); err != nil {
		return snap, err
	}
	for _, rev := range []gitrev.Revision{snap.Baseline, snap.Patch} {
		if err := snap.Repo.Pin(ctx, rev); err != nil {
			return snap, err
		}
	}
	return snap, nil
}

func writeRevisions(w io.Writer, base, patch gitrev.Revision) {
	writef(w, "baseline  tree=%s commit=%s source=%s\n", shortDigest(base.Tree), shortDigest(base.Commit), base.Source)
	writef(w, "patch     tree=%s commit=%s source=%s dirty=%t\n", shortDigest(patch.Tree), shortDigest(patch.Commit), patch.Source, patch.Dirty)
}
