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

func TestShopRewritesApply(t *testing.T) {
	t.Parallel()
	shop := shopDir(t)
	cases := []struct {
		name string
		fn   func(string, []string) ([]byte, error)
		file string
		want string
		gone string
	}{
		{"d2", D2, "internal/users/store.go", "SELECT id, line1, city FROM user_addresses WHERE user_id", "SELECT id FROM user_addresses WHERE user_id"},
		{"d3", D3, "migrations/000001_init.up.sql", "CREATE INDEX inventory_events_sku", "CREATE TABLE inventory_events_sku"},
		{"d4", D4, "internal/checkout/handler.go", "for range 1 {", "for range 5 {"},
		{"d5", D5, "internal/checkout/handler.go", "go func() {", `svcclient.PostJSON(ctx, h.http, h.notificationURL+"/notify"`},
		{"d6", D6, "internal/inventory/handler.go", `lockKey := "inventory:" + req.SKU`, `"inventory:global"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			raw, err := tc.fn(shop, []string{tc.file})
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			src := filepath.Join(shop, filepath.FromSlash(tc.file))
			dst := filepath.Join(root, filepath.FromSlash(tc.file))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(src)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dst, body, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := diff.Apply(root, raw); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(dst)
			if err != nil {
				t.Fatal(err)
			}
			if tc.gone != "" && strings.Contains(string(got), tc.gone) {
				t.Fatalf("%s", got)
			}
			if !strings.Contains(string(got), tc.want) {
				t.Fatalf("%s", got)
			}
		})
	}
}

func TestCandidatesPrefersD1(t *testing.T) {
	t.Parallel()
	raw, err := Candidates(shopDir(t), []string{"internal/payment/handler.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "svcclient.Shared()") {
		t.Fatalf("%s", raw)
	}
}

func TestCandidatesEmptyFilesUsesContent(t *testing.T) {
	t.Parallel()
	raw, err := Candidates(shopDir(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "svcclient.Shared()") {
		t.Fatalf("%s", raw)
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
