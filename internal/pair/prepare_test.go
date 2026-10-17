package pair

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/sumedhaerram/aquila/internal/gitrev"
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
	env, err := Prepare(t.Context(), treeOpts(t, shopDir(), raw, parent))
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
		t.Fatal("patch and baseline trees should differ")
	}
	_, err = Prepare(t.Context(), treeOpts(t, shopDir(), raw, parent))
	if err == nil {
		t.Fatal("expected exists error")
	}
}

func TestPrepareRejectsMissingTree(t *testing.T) {
	t.Parallel()
	_, err := Prepare(t.Context(), PrepareOpts{Parent: t.TempDir()})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPrepareRejectsEqualPorts(t *testing.T) {
	t.Parallel()
	_, err := Prepare(t.Context(), PrepareOpts{
		Baseline:  gitrev.Revision{Tree: "base"},
		Patch:     gitrev.Revision{Tree: "patch"},
		Parent:    t.TempDir(),
		BasePort:  18180,
		PatchPort: 18180,
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

func treeOpts(t *testing.T, dir string, raw []byte, parent string) PrepareOpts {
	t.Helper()
	repo, err := gitrev.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	base, err := repo.Head(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	patch, err := repo.SnapshotDiff(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	return PrepareOpts{
		Repo:     repo,
		Baseline: base,
		Patch:    patch,
		Parent:   parent,
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=aquila", "-c", "user.email=aquila@test", "-c", "commit.gpgsign=false", "commit", "-qm", "base"}} {
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v: %s", err, out)
		}
	}
}

func TestPrepareFromTrees(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, raw := range map[string]string{"go.mod": "module example.com/p\n", "Dockerfile": "FROM alpine\n", "a.go": "package p\nconst X = 1\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	initRepo(t, dir)
	repo, err := gitrev.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	base, err := repo.Head(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package p\nconst X = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch, err := repo.SnapshotWorktree(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	env, err := Prepare(t.Context(), PrepareOpts{
		Repo:     repo,
		Baseline: base,
		Patch:    patch,
		Parent:   t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		side Side
		want string
	}{{env.Baseline, "const X = 1"}, {env.Patch, "const X = 2"}} {
		raw, err := os.ReadFile(filepath.Join(tc.side.Dir, "a.go"))
		if err != nil || !strings.Contains(string(raw), tc.want) {
			t.Fatalf("%s: %q %v", tc.side.Role, raw, err)
		}
	}
	if env.BaselineDigest != base.Tree || env.PatchDigest != patch.Tree {
		t.Fatalf("%+v", env)
	}
}

func TestPrepareIDFromTrees(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/d1.diff")
	if err != nil {
		t.Fatal(err)
	}
	opts := treeOpts(t, shopDir(), raw, t.TempDir())
	first, err := Prepare(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.Parent = t.TempDir()
	second, err := Prepare(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(opts.Baseline.Tree + ":" + opts.Patch.Tree))
	if first.ID != hex.EncodeToString(sum[:])[:12] || first.ID != second.ID {
		t.Fatalf("ids %s %s", first.ID, second.ID)
	}
}
