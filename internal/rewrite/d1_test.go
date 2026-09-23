package rewrite

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sumedhaerram/aquila/internal/diff"
)

func TestD1ProducesApplyableDiff(t *testing.T) {
	t.Parallel()
	shop := shopDir(t)
	raw, err := D1(shop, []string{"internal/payment/handler.go"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "NewEphemeral") && !strings.Contains(string(raw), "-") {
		t.Fatalf("%s", raw)
	}
	if !strings.Contains(string(raw), "svcclient.Shared()") {
		t.Fatalf("%s", raw)
	}
	root := t.TempDir()
	src := filepath.Join(shop, "internal", "payment", "handler.go")
	dstDir := filepath.Join(root, "internal", "payment")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dstDir, "handler.go"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := diff.Apply(root, raw); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dstDir, "handler.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "svcclient.Shared()") || strings.Contains(string(got), "client := svcclient.NewEphemeral()") {
		t.Fatalf("%s", got)
	}
}

func TestD1RejectsUnrelatedImpact(t *testing.T) {
	t.Parallel()
	_, err := D1(shopDir(t), []string{"internal/users/store.go"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func shopDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "examples", "shop"))
}
