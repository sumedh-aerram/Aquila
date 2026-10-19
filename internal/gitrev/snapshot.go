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

// Revision source labels distinguish materialized builds from URL declarations.
const (
	SourceBuilt    = "built"
	SourceDeclared = "declared"
	maxCopyBytes   = 80 << 20
)

// Revision identifies a module tree and the commit retaining its repository root.
type Revision struct {
	Commit string `json:"commit"`
	Tree   string `json:"tree"`
	Path   string `json:"path"`
	Dirty  bool   `json:"dirty"`
	Source string `json:"source"`
}

// Repo is a repository root and its repository-relative module directory.
type Repo struct{ Root, Rel string }

// Open locates the repository containing dir.
func Open(ctx context.Context, dir string) (Repo, error) {
	root, rel, err := locate(ctx, dir)
	if err != nil {
		return Repo{}, err
	}
	return Repo{
		Root: root,
		Rel:  rel,
	}, nil
}

func (r Repo) command(ctx context.Context, index string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", r.Root}, args...)...)
	for _, v := range os.Environ() {
		key, _, _ := strings.Cut(v, "=")
		switch key {
		case "GIT_INDEX_FILE", "GIT_OPTIONAL_LOCKS", "GIT_LITERAL_PATHSPECS", "GIT_GLOB_PATHSPECS", "GIT_NOGLOB_PATHSPECS", "GIT_ICASE_PATHSPECS":
			continue
		}
		cmd.Env = append(cmd.Env, v)
	}
	cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0", "GIT_LITERAL_PATHSPECS=1")
	if index != "" {
		cmd.Env = append(cmd.Env, "GIT_INDEX_FILE="+index)
	}
	return cmd
}

func (r Repo) run(ctx context.Context, index string, input []byte, args ...string) ([]byte, error) {
	cmd := r.command(ctx, index, args...)
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	out, err := cmd.Output()
	if err != nil {
		label := args[0]
		for i := 0; i+2 < len(args) && args[i] == "-c"; i += 2 {
			label = args[i+2]
		}
		var failure *exec.ExitError
		if errors.As(err, &failure) {
			detail := strings.TrimSpace(string(failure.Stderr))
			if len(detail) > 512 {
				detail = detail[:512]
			}
			if detail != "" {
				return out, fmt.Errorf("gitrev: %s: %w: %s", label, err, detail)
			}
		}
		return out, fmt.Errorf("gitrev: %s: %w", label, err)
	}
	return out, nil
}

// Head returns the committed module tree at HEAD.
func (r Repo) Head(ctx context.Context) (Revision, error) {
	commit, err := r.run(ctx, "", nil, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return Revision{}, fmt.Errorf("gitrev: no commits: %w", err)
	}
	tree, err := r.subtree(ctx, "", strings.TrimSpace(string(commit)))
	if err != nil {
		return Revision{}, err
	}
	return Revision{
		Commit: strings.TrimSpace(string(commit)),
		Tree:   tree,
		Path:   r.Rel,
		Source: SourceDeclared,
	}, nil
}

func (r Repo) subtree(ctx context.Context, index, root string) (string, error) {
	spec := root + "^{tree}"
	if r.Rel != "." {
		spec = root + ":" + r.Rel
	}
	out, err := r.run(ctx, index, nil, "rev-parse", "--verify", spec)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// SnapshotWorktree records tracked and untracked module files using a temporary index.
func (r Repo) SnapshotWorktree(ctx context.Context) (Revision, error) {
	head, err := r.Head(ctx)
	if err != nil {
		return Revision{}, err
	}
	return r.SnapshotWorktreeAt(ctx, head)
}

// SnapshotDiff applies a repository-relative patch to HEAD in a temporary index.
func (r Repo) SnapshotDiff(ctx context.Context, raw []byte) (Revision, error) {
	if len(raw) == 0 {
		return Revision{}, fmt.Errorf("gitrev: snapshot: empty diff")
	}
	head, err := r.Head(ctx)
	if err != nil {
		return Revision{}, err
	}
	return r.SnapshotDiffAt(ctx, head, raw)
}

// SnapshotWorktreeAt captures module files relative to a previously resolved baseline.
func (r Repo) SnapshotWorktreeAt(ctx context.Context, head Revision) (Revision, error) {
	return r.snapshot(ctx, head, nil, true)
}

// SnapshotDiffAt applies a patch relative to a previously resolved baseline.
func (r Repo) SnapshotDiffAt(ctx context.Context, head Revision, raw []byte) (Revision, error) {
	if len(raw) == 0 {
		return Revision{}, fmt.Errorf("gitrev: snapshot: empty diff")
	}
	return r.snapshot(ctx, head, raw, false)
}

func (r Repo) snapshot(ctx context.Context, head Revision, raw []byte, worktree bool) (Revision, error) {
	if !objectID(head.Commit) || !objectID(head.Tree) || head.Path != r.Rel {
		return Revision{}, fmt.Errorf("gitrev: snapshot: invalid baseline")
	}
	if err := r.checkSettings(ctx); err != nil {
		return Revision{}, err
	}
	tmp, err := os.MkdirTemp("", "aquila-snap-")
	if err != nil {
		return Revision{}, fmt.Errorf("gitrev: snapshot: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	index := filepath.Join(tmp, "index")
	if _, err := r.run(ctx, index, nil, "read-tree", head.Commit); err != nil {
		return Revision{}, fmt.Errorf("gitrev: snapshot: no commits: %w", err)
	}
	if worktree {
		if err := r.checkWorktree(ctx, index); err != nil {
			return Revision{}, err
		}
		if _, err := r.run(ctx, index, nil, "add", "-A", "--", r.Rel); err != nil {
			return Revision{}, err
		}
	} else {
		args := []string{"apply", "--cached", "--whitespace=nowarn"}
		if _, err := r.run(ctx, index, raw, append(args, "--check")...); err != nil {
			if r.Rel == "." {
				return Revision{}, err
			}
			args = append(args, "--directory="+r.Rel)
			if _, retryErr := r.run(ctx, index, raw, append(args, "--check")...); retryErr != nil {
				return Revision{}, err
			}
		}
		if _, err := r.run(ctx, index, raw, args...); err != nil {
			return Revision{}, err
		}
		if err := r.checkAttributes(ctx, index, true); err != nil {
			return Revision{}, err
		}
	}
	root, err := r.run(ctx, index, nil, "write-tree")
	if err != nil {
		return Revision{}, err
	}
	rootTree := strings.TrimSpace(string(root))
	tree, err := r.subtree(ctx, index, rootTree)
	if err != nil {
		return Revision{}, err
	}
	if _, err := r.entries(ctx, index, tree); err != nil {
		return Revision{}, err
	}
	commit, err := r.run(ctx, index, nil, "-c", "user.name=aquila", "-c", "user.email=aquila@localhost", "commit-tree", "-p", head.Commit, "-m", "aquila snapshot "+r.Rel, rootTree)
	if err != nil {
		return Revision{}, err
	}
	return Revision{
		Commit: strings.TrimSpace(string(commit)),
		Tree:   tree,
		Path:   r.Rel,
		Dirty:  tree != head.Tree,
		Source: SourceDeclared,
	}, nil
}

func (r Repo) checkWorktree(ctx context.Context, index string) error {
	raw, err := r.run(ctx, index, nil, "ls-files", "-z", "--others", "--exclude-standard", "--", r.Rel)
	if err != nil {
		return err
	}
	var total int64
	for _, p := range bytes.Split(raw, []byte{0}) {
		if len(p) == 0 {
			continue
		}
		st, err := os.Lstat(filepath.Join(r.Root, string(p)))
		if err != nil {
			return fmt.Errorf("gitrev: snapshot: %w", err)
		}
		total += st.Size()
		if total > maxCopyBytes {
			return fmt.Errorf("gitrev: snapshot: untracked files exceed cap %d", maxCopyBytes)
		}
	}
	return r.checkAttributes(ctx, index, false)
}

func (r Repo) checkAttributes(ctx context.Context, index string, cached bool) error {
	paths, err := r.run(ctx, index, nil, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", r.Rel)
	if err != nil {
		return err
	}
	args := []string{"check-attr", "-z", "--stdin", "filter", "text", "eol", "ident", "working-tree-encoding"}
	if cached {
		args = append(args, "--cached")
	}
	attrs, err := r.run(ctx, index, paths, args...)
	if err != nil {
		return err
	}
	fields := bytes.Split(bytes.TrimSuffix(attrs, []byte{0}), []byte{0})
	textAttrs := make(map[string]string)
	eolAttrs := make(map[string]string)
	for i := 2; i < len(fields); i += 3 {
		name, attr, value := string(fields[i-2]), string(fields[i-1]), string(fields[i])
		switch attr {
		case "text":
			textAttrs[name] = value
			continue
		case "eol":
			eolAttrs[name] = value
			if value != "crlf" {
				continue
			}
		}
		if value == "unspecified" || value == "unset" {
			continue
		}
		if attr == "filter" {
			return fmt.Errorf("gitrev: snapshot: filters unsupported: %s=%s on %s", attr, value, name)
		}
		return fmt.Errorf("gitrev: snapshot: transforming attribute %s=%s on %s unsupported", attr, value, name)
	}
	if cached {
		return nil
	}
	eols, err := r.run(ctx, index, nil, "ls-files", "--eol", "-z", "--cached", "--others", "--exclude-standard", "--", r.Rel)
	if err != nil {
		return err
	}
	for _, record := range bytes.Split(eols, []byte{0}) {
		header, name, ok := strings.Cut(string(record), "\t")
		if !ok {
			continue
		}
		columns := strings.Fields(header)
		if len(columns) < 2 || columns[1] != "w/crlf" && columns[1] != "w/mixed" {
			continue
		}
		text, eol := textAttrs[name], eolAttrs[name]
		if text == "unset" || text == "unspecified" && eol != "lf" {
			continue
		}
		return fmt.Errorf("gitrev: snapshot: text=%s eol=%s would normalize CRLF bytes on %s", text, eol, name)
	}
	return nil
}

// Pin retains a snapshot commit under a ref named by its module tree.
func (r Repo) Pin(ctx context.Context, rev Revision) error {
	if !objectID(rev.Tree) || !objectID(rev.Commit) {
		return fmt.Errorf("gitrev: pin: invalid revision")
	}
	head, err := r.Head(ctx)
	if err != nil {
		return err
	}
	if rev.Commit == head.Commit {
		return nil
	}
	_, err = r.run(ctx, "", nil, "update-ref", "refs/aquila/snapshots/"+rev.Tree, rev.Commit)
	return err
}

// DiffTrees returns repository-relative changes restricted to the module directory.
func (r Repo) DiffTrees(ctx context.Context, a, b string) ([]byte, error) {
	if !objectID(a) || !objectID(b) {
		return nil, fmt.Errorf("gitrev: diff: invalid object")
	}
	out, err := r.run(ctx, "", nil, "diff", "--no-color", "--no-ext-diff", a, b, "--", r.Rel)
	if err != nil {
		return nil, err
	}
	if len(out) > maxDiffBytes {
		return nil, fmt.Errorf("gitrev: diff exceeds %d bytes", maxDiffBytes)
	}
	return out, nil
}

// ReadBlob reads a path relative to a module tree without applying filters.
func (r Repo) ReadBlob(ctx context.Context, tree, name string) ([]byte, error) {
	if !objectID(tree) || !safeTreePath(name) {
		return nil, fmt.Errorf("gitrev: blob: invalid path or tree")
	}
	return r.run(ctx, "", nil, "cat-file", "blob", tree+":"+name)
}

func objectID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func (r Repo) checkSettings(ctx context.Context) error {
	for _, key := range []string{"core.autocrlf", "core.sparseCheckout"} {
		args := []string{"config", "--type=bool-or-str", "--get", key}
		if key == "core.sparseCheckout" {
			args = []string{"config", "--bool", "--get", key}
		}
		value, err := r.run(ctx, "", nil, args...)
		if err != nil {
			var failure *exec.ExitError
			if errors.As(err, &failure) && failure.ExitCode() == 1 {
				continue
			}
			return err
		}
		setting := strings.ToLower(strings.TrimSpace(string(value)))
		if setting != "false" {
			return fmt.Errorf("gitrev: snapshot: %s=%s unsupported; byte-exact build requires false or unset", key, setting)
		}
	}
	return nil
}

// PlainDiff describes local changes without imposing build snapshot restrictions.
func (r Repo) PlainDiff(ctx context.Context) ([]byte, error) {
	base, err := r.run(ctx, "", nil, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		if !unbornHEAD(ctx, r.Root) {
			return nil, err
		}
		base, err = r.run(ctx, "", []byte{}, "hash-object", "-w", "-t", "tree", "--stdin")
		if err != nil {
			return nil, err
		}
	}
	raw, err := r.run(ctx, "", nil, "diff", "--no-color", "--no-ext-diff", "--no-textconv", strings.TrimSpace(string(base)), "--", r.Rel)
	if err != nil {
		return nil, err
	}
	paths, err := r.run(ctx, "", nil, "ls-files", "-z", "--others", "--exclude-standard", "--", r.Rel)
	if err != nil {
		return nil, err
	}
	for _, name := range bytes.Split(paths, []byte{0}) {
		if len(name) == 0 {
			continue
		}
		part, err := r.run(ctx, "", nil, "diff", "--no-index", "--no-color", "--no-ext-diff", "--no-textconv", "--", "/dev/null", string(name))
		if err != nil {
			var failure *exec.ExitError
			if !errors.As(err, &failure) || failure.ExitCode() != 1 {
				return nil, err
			}
		}
		raw = append(raw, part...)
		if len(raw) > maxDiffBytes {
			return nil, fmt.Errorf("gitrev: diff exceeds %d bytes", maxDiffBytes)
		}
	}
	if len(raw) > maxDiffBytes {
		return nil, fmt.Errorf("gitrev: diff exceeds %d bytes", maxDiffBytes)
	}
	return raw, nil
}
