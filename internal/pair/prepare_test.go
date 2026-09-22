package pair

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestComposeShapeMatchesShopServices(t *testing.T) {
	t.Parallel()
	raw, err := bundled.ReadFile("compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, "18080") {
		t.Fatal("experiment compose must not publish the live shop port")
	}
	if strings.Contains(body, "aquila-api") || strings.Contains(body, "AQUILA_") {
		t.Fatal("experiment compose must not include the control plane")
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	services, _ := doc["services"].(map[string]any)
	for _, name := range []string{
		"gateway", "checkout", "payment", "users", "inventory", "processor", "notification",
		"shop-postgres", "shop-redis", "shop-migrate", "otel-collector",
	} {
		if _, ok := services[name]; !ok {
			t.Errorf("missing service %s", name)
		}
	}
	if !strings.Contains(body, "${SHOP_CONTEXT}") || !strings.Contains(body, "${GATEWAY_PORT}") {
		t.Fatal("compose must interpolate context and port")
	}
}

func TestPrepareShopD1IsolatesPatch(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "testdata", "d1.diff"))
	if err != nil {
		t.Fatal(err)
	}
	env, err := Prepare(t.Context(), PrepareOpts{ShopDir: shopDir(), Parent: parent, Diff: raw})
	if err != nil {
		t.Fatal(err)
	}
	if env.Status != "prepared" || env.ID == "" {
		t.Fatalf("%+v", env)
	}
	basePay := filepath.Join(env.Baseline.Dir, "internal", "payment", "handler.go")
	patchPay := filepath.Join(env.Patch.Dir, "internal", "payment", "handler.go")
	b, err := os.ReadFile(basePay)
	if err != nil {
		t.Fatal(err)
	}
	p, err := os.ReadFile(patchPay)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "NewEphemeral") || strings.Contains(string(b), "client := svcclient.Shared()") {
		t.Fatalf("baseline mutated: %s", b)
	}
	if !strings.Contains(string(p), "svcclient.Shared()") || strings.Contains(string(p), "NewEphemeral") {
		t.Fatalf("patch not applied: %s", p)
	}
	man, err := os.ReadFile(filepath.Join(env.Root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(man), "NewEphemeral") || strings.Contains(string(man), "Shared()") {
		t.Fatal("manifest retained hunk text")
	}
	if env.Baseline.GatewayPort == env.Patch.GatewayPort {
		t.Fatal("ports must differ")
	}
	if env.Baseline.ComposeFile != env.Patch.ComposeFile {
		t.Fatal("sides must share compose file")
	}
	if _, err := os.Stat(env.ComposeFile); err != nil {
		t.Fatal(err)
	}
	if env.BaselineDigest == env.PatchDigest {
		t.Fatal("patch digest should be the diff hash, not the tree")
	}
	_, err = Prepare(t.Context(), PrepareOpts{ShopDir: shopDir(), Parent: parent, Diff: raw})
	if err == nil {
		t.Fatal("expected exists error")
	}
}

func TestPrepareRejectsEmptyDiff(t *testing.T) {
	t.Parallel()
	_, err := Prepare(t.Context(), PrepareOpts{ShopDir: shopDir(), Parent: t.TempDir(), Diff: nil})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPrepareRejectsEqualPorts(t *testing.T) {
	t.Parallel()
	_, err := Prepare(t.Context(), PrepareOpts{
		ShopDir: shopDir(), Parent: t.TempDir(), Diff: []byte("x"), BasePort: 18180, PatchPort: 18180,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func shopDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "examples", "shop"))
}
