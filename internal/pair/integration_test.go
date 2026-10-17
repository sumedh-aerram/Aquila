package pair

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestIntegration(t *testing.T) {
	t.Run("pair up from trees", func(t *testing.T) { TestPairUpFromTrees(t) })
}

func TestPairUpFromTrees(t *testing.T) {
	if os.Getenv("AQUILA_TEST_DOCKER") != "1" {
		t.Skip("AQUILA_TEST_DOCKER!=1")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(t.Context(), "docker", "info").CombinedOutput(); err != nil {
		t.Fatalf("docker info: %v: %s", err, out)
	}
	raw, err := os.ReadFile("testdata/d1.diff")
	if err != nil {
		t.Fatal(err)
	}
	opts := treeOpts(t, shopDir(), raw, t.TempDir())
	opts.BasePort = 28180
	opts.PatchPort = 28280
	env, err := Prepare(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	t.Cleanup(func() {
		if err := Down(context.Background(), env); err != nil {
			t.Errorf("down: %v", err)
		}
	})
	if err := Up(ctx, env); err != nil {
		t.Fatal(err)
	}
	client := http.Client{Timeout: 5 * time.Second}
	for _, side := range []Side{env.Baseline, env.Patch} {
		resp, err := client.Get("http://" + side.Gateway + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s health=%d", side.Role, resp.StatusCode)
		}
	}
}
