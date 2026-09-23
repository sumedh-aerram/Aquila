package rewrite

import (
	"fmt"
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
	return candidate(dir, files, d1File, d1Old, d1New)
}

func touches(files []string, want string) bool {
	if len(files) == 0 {
		return true
	}
	want = filepath.ToSlash(want)
	for _, f := range files {
		p := filepath.ToSlash(f)
		if p == want || strings.HasSuffix(p, "/"+want) {
			return true
		}
	}
	return false
}

func unified(path string, start int, oldText, newText string) []byte {
	oldText = strings.TrimSuffix(oldText, "\n")
	newText = strings.TrimSuffix(newText, "\n")
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")
	var b strings.Builder
	b.WriteString("diff --git a/" + path + " b/" + path + "\n")
	b.WriteString("--- a/" + path + "\n")
	b.WriteString("+++ b/" + path + "\n")
	b.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", start, len(oldLines), start, len(newLines)))
	for _, line := range oldLines {
		b.WriteString("-")
		b.WriteString(line)
		b.WriteString("\n")
	}
	for _, line := range newLines {
		b.WriteString("+")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return []byte(b.String())
}
