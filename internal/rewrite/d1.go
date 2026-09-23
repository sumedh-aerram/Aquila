package rewrite

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	d1File = "internal/payment/handler.go"
	d1Old  = "\tclient := svcclient.NewEphemeral()\n"
	d1New  = "\tclient := svcclient.Shared()\n"
)

// D1 returns a unified diff that reuses the shared HTTP client in
// payment.chargeProcessor. It does not write the module tree.
func D1(dir string, files []string) ([]byte, error) {
	if !touches(files, d1File) {
		return nil, fmt.Errorf("rewrite: no candidate")
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("rewrite: %w", err)
	}
	path := filepath.Join(root, filepath.FromSlash(d1File))
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("rewrite: %w", err)
	}
	if !bytes.Contains(raw, []byte(d1Old)) {
		return nil, fmt.Errorf("rewrite: no candidate")
	}
	if bytes.Contains(raw, []byte(d1New)) {
		return nil, fmt.Errorf("rewrite: no candidate")
	}
	idx := bytes.Index(raw, []byte(d1Old))
	if idx < 0 {
		return nil, fmt.Errorf("rewrite: no candidate")
	}
	start := 1 + bytes.Count(raw[:idx], []byte("\n"))
	return unified(d1File, start, d1Old, d1New), nil
}

func touches(files []string, want string) bool {
	want = filepath.ToSlash(want)
	for _, f := range files {
		p := filepath.ToSlash(f)
		if p == want || strings.HasSuffix(p, "/"+want) {
			return true
		}
	}
	return false
}

func unified(path string, start int, oldLine, newLine string) []byte {
	var b strings.Builder
	b.WriteString("diff --git a/" + path + " b/" + path + "\n")
	b.WriteString("--- a/" + path + "\n")
	b.WriteString("+++ b/" + path + "\n")
	b.WriteString(fmt.Sprintf("@@ -%d,1 +%d,1 @@\n", start, start))
	b.WriteString("-")
	b.WriteString(strings.TrimSuffix(oldLine, "\n"))
	b.WriteString("\n+")
	b.WriteString(strings.TrimSuffix(newLine, "\n"))
	b.WriteString("\n")
	return []byte(b.String())
}
