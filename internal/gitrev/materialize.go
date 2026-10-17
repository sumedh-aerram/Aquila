package gitrev

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

type treeEntry struct{ mode, blob, name string }

func safeTreePath(name string) bool {
	if name == "" || name == "." || path.IsAbs(name) || strings.ContainsRune(name, 0) {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." || part == "" || part == ".git" {
			return false
		}
	}
	return path.Clean(name) == name
}

func (r Repo) entries(ctx context.Context, index, tree string) ([]treeEntry, error) {
	if !objectID(tree) {
		return nil, fmt.Errorf("gitrev: materialize: invalid tree")
	}
	raw, err := r.run(ctx, index, nil, "ls-tree", "-r", "-z", "--full-tree", tree)
	if err != nil {
		return nil, err
	}
	var entries []treeEntry
	for _, line := range bytes.Split(raw, []byte{0}) {
		if len(line) == 0 {
			continue
		}
		header, name, ok := strings.Cut(string(line), "\t")
		fields := strings.Fields(header)
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("gitrev: snapshot: invalid tree entry")
		}
		if (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" || !safeTreePath(name) {
			return nil, fmt.Errorf("gitrev: snapshot: unsupported entry %s", name)
		}
		entries = append(entries, treeEntry{
			mode: fields[0],
			blob: fields[2],
			name: name,
		})
	}
	return entries, nil
}

// Materialize writes exact blob bytes and executable modes from tree into a new directory.
func (r Repo) Materialize(ctx context.Context, tree, dst string) (err error) {
	defer func() {
		if err != nil && !strings.HasPrefix(err.Error(), "gitrev: materialize:") {
			err = fmt.Errorf("gitrev: materialize: %w", err)
		}
	}()
	entries, err := r.entries(ctx, "", tree)
	if err != nil {
		return err
	}
	if err := os.Mkdir(dst, 0o755); err != nil {
		return fmt.Errorf("gitrev: materialize: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dst)
		}
	}()
	batch := r.command(ctx, "", "cat-file", "--batch")
	input, err := batch.StdinPipe()
	if err != nil {
		return err
	}
	output, err := batch.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return err
	}
	if err := batch.Start(); err != nil {
		_ = input.Close()
		return fmt.Errorf("gitrev: materialize: %w", err)
	}
	defer func() {
		_ = input.Close()
		if err != nil {
			_ = batch.Process.Kill()
		}
		if waitErr := batch.Wait(); err == nil && waitErr != nil {
			err = fmt.Errorf("gitrev: materialize: %w", waitErr)
			_ = os.RemoveAll(dst)
		}
	}()
	reader := bufio.NewReader(output)
	var total int64
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := io.WriteString(input, entry.blob+"\n"); err != nil {
			return err
		}
		header, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[0] != entry.blob || fields[1] != "blob" {
			return fmt.Errorf("gitrev: materialize: invalid blob response")
		}
		n, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil || n < 0 || n > maxCopyBytes-total {
			return fmt.Errorf("gitrev: materialize: tree exceeds cap %d", maxCopyBytes)
		}
		total += n
		target := filepath.Join(dst, filepath.FromSlash(entry.name))
		rel, err := filepath.Rel(dst, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("gitrev: materialize: path outside destination")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if entry.mode == "100755" {
			mode = 0o755
		}
		file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		if err := file.Chmod(mode); err != nil {
			_ = file.Close()
			return err
		}
		_, copyErr := io.CopyN(file, reader, n)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		separator, err := reader.ReadByte()
		if err != nil {
			return err
		}
		if separator != '\n' {
			return fmt.Errorf("gitrev: materialize: invalid blob separator")
		}
	}
	return nil
}
