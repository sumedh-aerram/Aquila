package replay

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFilePOSTWithObjectBody(t *testing.T) {
	t.Parallel()
	path := writeWorkload(t, `{"steps":[{"method":"POST","path":"/v1/foo","body":{"user":"a"}}]}`)
	w, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Steps) != 1 {
		t.Fatalf("%+v", w.Steps)
	}
	s := w.Steps[0]
	if s.Method != http.MethodPost || s.Path != "/v1/foo" || s.Provenance != ProvenanceFile {
		t.Fatalf("%+v", s)
	}
	if string(s.Body) != `{"user":"a"}` {
		t.Fatalf("body=%s", s.Body)
	}
}

func TestReadFileRejectsTemplateAndTraversal(t *testing.T) {
	t.Parallel()
	path := writeWorkload(t, `{"steps":[{"method":"GET","path":"/users/{id}"}]}`)
	if _, err := ReadFile(path); err == nil {
		t.Fatal("expected parameterized path to fail")
	}
	path = writeWorkload(t, `{"steps":[{"method":"GET","path":"/../etc/passwd"}]}`)
	if _, err := ReadFile(path); err == nil {
		t.Fatal("expected traversal to fail")
	}
}

func TestReadFileRejectsRemotePath(t *testing.T) {
	t.Parallel()
	if _, err := ReadFile("https://example.com/w.json"); err == nil {
		t.Fatal("expected remote path to fail")
	}
}

func TestReadFileRejectsEmpty(t *testing.T) {
	t.Parallel()
	path := writeWorkload(t, `{"steps":[]}`)
	if _, err := ReadFile(path); err == nil {
		t.Fatal("expected empty steps to fail")
	}
}

func TestReadFileCapsSteps(t *testing.T) {
	t.Parallel()
	b := strings.Builder{}
	b.WriteString(`{"steps":[`)
	for i := 0; i < maxSteps+1; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"method":"GET","path":"/x"}`)
	}
	b.WriteString(`]}`)
	path := writeWorkload(t, b.String())
	if _, err := ReadFile(path); err == nil {
		t.Fatal("expected cap")
	}
}

func writeWorkload(t *testing.T, raw string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "w.json")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
