package diff

import (
	"bufio"
	"bytes"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	MaxBytes = 1 << 20
	maxFiles = 500
	maxHunks = 4000
)

// File is one path touched by a unified diff. Hunk text is not retained.
type File struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Binary bool   `json:"binary,omitempty"`
	Lines  []int  `json:"lines,omitempty"`
}

// Diff is a parsed unified diff.
type Diff struct {
	Files []File `json:"files"`
}

// Parse reads a git or unified diff. It does not read the filesystem.
func Parse(data []byte) (Diff, error) {
	if len(data) > MaxBytes {
		return Diff{}, fmt.Errorf("diff: exceeds %d bytes", MaxBytes)
	}
	if len(data) == 0 {
		return Diff{Files: []File{}}, nil
	}
	if !utf8.Valid(data) {
		return Diff{}, fmt.Errorf("diff: not utf-8")
	}

	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), MaxBytes)

	var files []File
	var cur *File
	inHunk := false
	newLn := 0
	hunks := 0

	flush := func() error {
		if cur == nil {
			return nil
		}
		if cur.Path == "" {
			return fmt.Errorf("diff: file missing path")
		}
		p, ok := CanonicalPath(cur.Path)
		if !ok {
			return fmt.Errorf("diff: invalid path %q", cur.Path)
		}
		cur.Path = p
		cur.Lines = uniqueInts(cur.Lines)
		if len(files)+1 > maxFiles {
			return fmt.Errorf("diff: file cap %d exceeded", maxFiles)
		}
		files = append(files, *cur)
		cur = nil
		inHunk = false
		return nil
	}

	startFile := func() error {
		if err := flush(); err != nil {
			return err
		}
		cur = &File{Status: "modify"}
		return nil
	}

	for sc.Scan() {
		line := sc.Text()
		if inHunk && !hunkBodyLine(line) {
			inHunk = false
		}
		switch {
		case strings.HasPrefix(line, "diff --git "):
			if err := startFile(); err != nil {
				return Diff{}, err
			}
			a, b := gitPaths(line[len("diff --git "):])
			if b != "" {
				cur.Path = b
			} else {
				cur.Path = a
			}
		case cur != nil && strings.HasPrefix(line, "new file mode"):
			cur.Status = "add"
		case cur != nil && strings.HasPrefix(line, "deleted file mode"):
			cur.Status = "delete"
		case cur != nil && (strings.HasPrefix(line, "rename from ") || strings.HasPrefix(line, "rename to ")):
			cur.Status = "rename"
			if strings.HasPrefix(line, "rename to ") {
				cur.Path = strings.TrimSpace(line[len("rename to "):])
			}
		case cur != nil && (strings.HasPrefix(line, "Binary files ") || line == "GIT binary patch"):
			cur.Binary = true
			inHunk = false
		case strings.HasPrefix(line, "+++ "):
			if cur == nil {
				if err := startFile(); err != nil {
					return Diff{}, err
				}
			}
			p := headerPath(line[4:])
			if p != "/dev/null" && p != "" {
				cur.Path = p
			} else if p == "/dev/null" {
				cur.Status = "delete"
			}
		case strings.HasPrefix(line, "--- "):
			if cur == nil {
				if err := startFile(); err != nil {
					return Diff{}, err
				}
			}
			p := headerPath(line[4:])
			if cur.Path == "" && p != "/dev/null" {
				cur.Path = p
			}
			if p == "/dev/null" {
				cur.Status = "add"
			}
		case strings.HasPrefix(line, "@@ "):
			if cur == nil {
				return Diff{}, fmt.Errorf("diff: hunk without file header")
			}
			if cur.Binary {
				continue
			}
			start, err := hunkNewStart(line)
			if err != nil {
				return Diff{}, err
			}
			hunks++
			if hunks > maxHunks {
				return Diff{}, fmt.Errorf("diff: hunk cap %d exceeded", maxHunks)
			}
			inHunk = true
			newLn = start
		case inHunk && cur != nil && !cur.Binary:
			if len(line) == 0 {
				newLn++
				continue
			}
			switch line[0] {
			case '+':
				cur.Lines = append(cur.Lines, newLn)
				newLn++
			case '-':
				cur.Lines = append(cur.Lines, newLn)
			case ' ', '\t':
				newLn++
			case '\\':
			default:
				inHunk = false
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Diff{}, fmt.Errorf("diff: scan: %w", err)
	}
	if err := flush(); err != nil {
		return Diff{}, err
	}
	if files == nil {
		files = []File{}
	}
	return Diff{Files: files}, nil
}

// CanonicalPath strips git a/b prefixes and examples/shop/, and rejects traversal.
func CanonicalPath(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	s = strings.ReplaceAll(s, "\\", "/")
	s = strings.TrimPrefix(s, "a/")
	s = strings.TrimPrefix(s, "b/")
	s = path.Clean(s)
	if s == "." || s == "/" || strings.HasPrefix(s, "../") || strings.Contains(s, "/../") || strings.Contains(s, "..") {
		return "", false
	}
	s = strings.TrimPrefix(s, "/")
	s = strings.TrimPrefix(s, "examples/shop/")
	if s == "" {
		return "", false
	}
	return s, true
}

func headerPath(rest string) string {
	rest = strings.TrimSpace(rest)
	if i := strings.IndexByte(rest, '\t'); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, ' '); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

func gitPaths(rest string) (string, string) {
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		if len(fields) == 1 {
			return fields[0], ""
		}
		return "", ""
	}
	return fields[0], fields[len(fields)-1]
}

func hunkNewStart(line string) (int, error) {
	plus := strings.Index(line, " +")
	if plus < 0 {
		return 0, fmt.Errorf("diff: bad hunk header")
	}
	rest := line[plus+2:]
	end := strings.IndexByte(rest, ' ')
	if end < 0 {
		end = strings.Index(rest, "@@")
	}
	if end >= 0 {
		rest = rest[:end]
	}
	rest = strings.Trim(rest, " @")
	if i := strings.IndexByte(rest, ','); i >= 0 {
		rest = rest[:i]
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("diff: bad hunk header")
	}
	return n, nil
}

func hunkBodyLine(line string) bool {
	if line == "" {
		return true
	}
	switch line[0] {
	case ' ', '\t', '+', '-', '\\':
		return true
	default:
		return false
	}
}

func uniqueInts(in []int) []int {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, n := range in {
		if n <= 0 {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}
