package diff

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxApplyFile = 2 << 20

// Apply writes a unified diff onto a directory. Paths are CanonicalPath-relative
// to root. It does not execute code. Binary, rename, and mismatched context fail.
func Apply(root string, data []byte) error {
	if _, err := Parse(data); err != nil {
		return err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("diff: apply: %w", err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("diff: apply: %w", err)
	}
	if !st.IsDir() {
		return fmt.Errorf("diff: apply: %s is not a directory", root)
	}
	files, err := collectApply(data)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("diff: apply: no files")
	}
	for _, f := range files {
		if err := applyOne(abs, f); err != nil {
			return err
		}
	}
	return nil
}

type applyFile struct {
	path   string
	status string
	binary bool
	hunks  []applyHunk
}

type applyHunk struct {
	oldStart int
	lines    []string
}

func collectApply(data []byte) ([]applyFile, error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), MaxBytes)

	var files []applyFile
	var cur *applyFile
	inHunk := false
	var hunk applyHunk

	flushHunk := func() {
		if cur == nil || !inHunk {
			return
		}
		cur.hunks = append(cur.hunks, hunk)
		hunk = applyHunk{}
		inHunk = false
	}
	flushFile := func() {
		flushHunk()
		if cur == nil {
			return
		}
		if cur.path == "" {
			cur = nil
			return
		}
		p, ok := CanonicalPath(cur.path)
		if !ok {
			cur = nil
			return
		}
		cur.path = p
		files = append(files, *cur)
		cur = nil
	}
	startFile := func() {
		flushFile()
		cur = &applyFile{status: "modify"}
	}

	for sc.Scan() {
		line := sc.Text()
		if inHunk && !hunkBodyLine(line) {
			flushHunk()
		}
		switch {
		case strings.HasPrefix(line, "diff --git "):
			startFile()
			a, b := gitPaths(line[len("diff --git "):])
			if b != "" {
				cur.path = b
			} else {
				cur.path = a
			}
		case cur != nil && strings.HasPrefix(line, "new file mode"):
			cur.status = "add"
		case cur != nil && strings.HasPrefix(line, "deleted file mode"):
			cur.status = "delete"
		case cur != nil && (strings.HasPrefix(line, "rename from ") || strings.HasPrefix(line, "rename to ")):
			return nil, fmt.Errorf("diff: apply: rename not supported")
		case cur != nil && (strings.HasPrefix(line, "Binary files ") || line == "GIT binary patch"):
			cur.binary = true
			inHunk = false
		case strings.HasPrefix(line, "+++ "):
			if cur == nil {
				startFile()
			}
			p := headerPath(line[4:])
			if p != "/dev/null" && p != "" {
				cur.path = p
			} else if p == "/dev/null" {
				cur.status = "delete"
			}
		case strings.HasPrefix(line, "--- "):
			if cur == nil {
				startFile()
			}
			p := headerPath(line[4:])
			if cur.path == "" && p != "/dev/null" {
				cur.path = p
			}
			if p == "/dev/null" {
				cur.status = "add"
			}
		case strings.HasPrefix(line, "@@ "):
			if cur == nil {
				return nil, fmt.Errorf("diff: apply: hunk without file header")
			}
			if cur.binary {
				continue
			}
			oldStart, err := hunkOldStart(line)
			if err != nil {
				return nil, err
			}
			flushHunk()
			inHunk = true
			hunk = applyHunk{oldStart: oldStart}
		case inHunk && cur != nil && !cur.binary:
			if line == "" {
				hunk.lines = append(hunk.lines, " ")
				continue
			}
			switch line[0] {
			case '+', '-', ' ', '\t':
				hunk.lines = append(hunk.lines, line)
			case '\\':
			default:
				flushHunk()
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("diff: apply: %w", err)
	}
	flushFile()
	return files, nil
}

func applyOne(root string, f applyFile) error {
	if f.binary {
		return fmt.Errorf("diff: apply: binary not supported")
	}
	dest, err := containedPath(root, f.path)
	if err != nil {
		return err
	}
	switch f.status {
	case "add":
		if _, err := os.Lstat(dest); err == nil {
			return fmt.Errorf("diff: apply: %s already exists", f.path)
		}
		out, err := applyToLines(nil, f.hunks)
		if err != nil {
			return fmt.Errorf("diff: apply %s: %w", f.path, err)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("diff: apply: %w", err)
		}
		return os.WriteFile(dest, out, 0o644)
	case "delete":
		if err := os.Remove(dest); err != nil {
			return fmt.Errorf("diff: apply: %w", err)
		}
		return nil
	default:
		st, err := os.Lstat(dest)
		if err != nil {
			return fmt.Errorf("diff: apply: %s: %w", f.path, err)
		}
		if st.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("diff: apply: refusing symlink %s", f.path)
		}
		if st.Size() > maxApplyFile {
			return fmt.Errorf("diff: apply: %s too large", f.path)
		}
		raw, err := os.ReadFile(dest)
		if err != nil {
			return fmt.Errorf("diff: apply: %w", err)
		}
		if !utf8.Valid(raw) {
			return fmt.Errorf("diff: apply: %s is not utf-8", f.path)
		}
		out, err := applyToLines(splitLines(raw), f.hunks)
		if err != nil {
			return fmt.Errorf("diff: apply %s: %w", f.path, err)
		}
		return os.WriteFile(dest, out, 0o644)
	}
}

func applyToLines(orig []string, hunks []applyHunk) ([]byte, error) {
	var out []string
	cursor := 1
	oi := 0
	for _, h := range hunks {
		if h.oldStart == 0 {
			h.oldStart = 1
		}
		if h.oldStart < cursor {
			return nil, fmt.Errorf("hunks out of order")
		}
		for cursor < h.oldStart {
			if oi >= len(orig) {
				return nil, fmt.Errorf("hunk past end of file")
			}
			out = append(out, orig[oi])
			oi++
			cursor++
		}
		for _, ln := range h.lines {
			if ln == "" {
				ln = " "
			}
			switch ln[0] {
			case ' ', '\t':
				want := ln[1:]
				if oi >= len(orig) || orig[oi] != want {
					return nil, fmt.Errorf("context mismatch")
				}
				out = append(out, orig[oi])
				oi++
				cursor++
			case '-':
				want := ln[1:]
				if oi >= len(orig) || orig[oi] != want {
					return nil, fmt.Errorf("context mismatch")
				}
				oi++
				cursor++
			case '+':
				out = append(out, ln[1:])
			}
		}
	}
	for oi < len(orig) {
		out = append(out, orig[oi])
		oi++
	}
	body := strings.Join(out, "\n")
	if len(orig) > 0 || len(out) > 0 {
		body += "\n"
	}
	return []byte(body), nil
}

func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	s := string(b)
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return []string{""}
	}
	return strings.Split(s, "\n")
}

func containedPath(root, rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("diff: apply: empty path")
	}
	dest := filepath.Join(root, filepath.FromSlash(rel))
	clean, err := filepath.Abs(dest)
	if err != nil {
		return "", fmt.Errorf("diff: apply: %w", err)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("diff: apply: %w", err)
	}
	sep := string(os.PathSeparator)
	if clean != rootAbs && !strings.HasPrefix(clean, rootAbs+sep) {
		return "", fmt.Errorf("diff: apply: path escapes root")
	}
	return clean, nil
}

func hunkOldStart(line string) (int, error) {
	minus := strings.Index(line, "-")
	if minus < 0 {
		return 0, fmt.Errorf("diff: bad hunk header")
	}
	rest := line[minus+1:]
	end := strings.IndexAny(rest, " ,")
	if end < 0 {
		return 0, fmt.Errorf("diff: bad hunk header")
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil || n < 0 {
		return 0, fmt.Errorf("diff: bad hunk header")
	}
	return n, nil
}
