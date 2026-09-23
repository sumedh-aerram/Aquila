package pair

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var composeMu sync.Mutex

func TestWaitHealthy(t *testing.T) {
	t.Parallel()
	srv := loopbackHealth(t)
	if err := waitHealthy(t.Context(), srv.Listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
}

func TestWaitHealthyTimeout(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	if err := waitHealthy(ctx, addr); err == nil {
		t.Fatal("expected timeout")
	}
}

func TestComposeArgsStayUnderRoot(t *testing.T) {
	t.Parallel()
	env := fixtureEnv(t, "127.0.0.1:18181", "127.0.0.1:18182")
	args, err := composeArgs(env, env.Baseline, "up", "-d", "--build")
	if err != nil {
		t.Fatal(err)
	}
	if !hasArg(args, "compose") || !hasArg(args, env.Baseline.Project) || !hasArg(args, env.ComposeFile) {
		t.Fatalf("%v", args)
	}
	if hasArg(args, "/var/run/docker.sock") {
		t.Fatal(args)
	}
	env.ComposeFile = filepath.Join(t.TempDir(), "docker-compose.yaml")
	if _, err := composeArgs(env, env.Baseline, "up"); err == nil {
		t.Fatal("expected path error")
	}
}

func TestUpRejectsNonLoopback(t *testing.T) {
	t.Parallel()
	env := fixtureEnv(t, "8.8.8.8:18181", "127.0.0.1:18182")
	if err := Up(t.Context(), env); err == nil {
		t.Fatal("expected error")
	}
}

func TestUpRejectsUnexpectedProject(t *testing.T) {
	t.Parallel()
	env := fixtureEnv(t, "127.0.0.1:18181", "127.0.0.1:18182")
	env.Baseline.Project = "other"
	if err := Up(t.Context(), env); err == nil {
		t.Fatal("expected error")
	}
}

func TestUpStartsBothAndDownsOnHealthFailure(t *testing.T) {
	composeMu.Lock()
	defer composeMu.Unlock()
	orig := runCompose
	t.Cleanup(func() { runCompose = orig })

	baseAddr := freeLoopback(t)
	patchAddr := freeLoopback(t)
	env := fixtureEnv(t, baseAddr, patchAddr)
	var started []*httptest.Server
	t.Cleanup(func() {
		for _, s := range started {
			s.Close()
		}
	})
	var calls []string
	runCompose = func(_ context.Context, dir string, args []string) error {
		if dir != env.Root {
			t.Fatalf("dir=%s want %s", dir, env.Root)
		}
		if hasArg(args, "/var/run/docker.sock") {
			t.Fatal(args)
		}
		calls = append(calls, strings.Join(args, " "))
		if hasArg(args, "up") {
			p := args[indexOf(args, "-p")+1]
			if strings.HasSuffix(p, "-baseline") {
				started = append(started, healthAt(t, baseAddr))
			}
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := Up(ctx, env); err == nil {
		t.Fatal("expected health failure")
	}
	if !joinContains(calls, "up") {
		t.Fatalf("missing up: %v", calls)
	}
	if !joinContains(calls, "down") {
		t.Fatalf("must down after failed health: %v", calls)
	}
}

func TestDownBothSides(t *testing.T) {
	composeMu.Lock()
	defer composeMu.Unlock()
	orig := runCompose
	t.Cleanup(func() { runCompose = orig })
	env := fixtureEnv(t, "127.0.0.1:18181", "127.0.0.1:18182")
	var downs []string
	runCompose = func(_ context.Context, dir string, args []string) error {
		if !hasArg(args, "down") {
			t.Fatalf("%v", args)
		}
		if !hasArg(args, "--volumes") {
			t.Fatal("down must remove volumes")
		}
		i := indexOf(args, "-p")
		if i < 0 || i+1 >= len(args) {
			t.Fatalf("%v", args)
		}
		downs = append(downs, args[i+1])
		return nil
	}
	if err := Down(t.Context(), env); err != nil {
		t.Fatal(err)
	}
	if len(downs) != 2 {
		t.Fatalf("%v", downs)
	}
}

func TestDockerComposeMissingBinary(t *testing.T) {
	composeMu.Lock()
	defer composeMu.Unlock()
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })
	lookPath = func(string) (string, error) { return "", errors.New("missing") }
	err := dockerCompose(t.Context(), t.TempDir(), []string{"compose", "version"})
	if err == nil || !strings.Contains(err.Error(), "docker is required") {
		t.Fatalf("%v", err)
	}
}

func TestPrepareReplaceTinyShop(t *testing.T) {
	t.Parallel()
	shop := t.TempDir()
	if err := os.WriteFile(filepath.Join(shop, "go.mod"), []byte("module example.com/s\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shop, "Dockerfile"), []byte("FROM alpine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(shop, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shop, "internal", "a.go"), []byte("package p\nconst X = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	raw := []byte("--- a/internal/a.go\n+++ b/internal/a.go\n@@ -1,2 +1,2 @@\n package p\n-const X = 1\n+const X = 2\n")
	first, err := Prepare(t.Context(), PrepareOpts{ShopDir: shop, Parent: parent, Diff: raw})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Prepare(t.Context(), PrepareOpts{ShopDir: shop, Parent: parent, Diff: raw, Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("%s vs %s", second.ID, first.ID)
	}
}

func TestUnderRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if !underRoot(root, filepath.Join(root, "docker-compose.yaml")) {
		t.Fatal("child")
	}
	if underRoot(root, filepath.Join(root, "..", "x.yaml")) {
		t.Fatal("parent")
	}
}

func fixtureEnv(t *testing.T, baseGw, patchGw string) Env {
	t.Helper()
	root := t.TempDir()
	compose := filepath.Join(root, "docker-compose.yaml")
	if err := os.WriteFile(compose, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	baseEnv := filepath.Join(root, "baseline.env")
	patchEnv := filepath.Join(root, "patch.env")
	if err := os.WriteFile(baseEnv, []byte("SHOP_CONTEXT=./baseline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(patchEnv, []byte("SHOP_CONTEXT=./patch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := "aaaaaaaaaaaa"
	return Env{
		ID:          id,
		Root:        root,
		ComposeFile: compose,
		Baseline: Side{
			Role:        "baseline",
			Gateway:     baseGw,
			Project:     projectPrefix + id + "-baseline",
			EnvFile:     baseEnv,
			ComposeFile: compose,
		},
		Patch: Side{
			Role:        "patch",
			Gateway:     patchGw,
			Project:     projectPrefix + id + "-patch",
			EnvFile:     patchEnv,
			ComposeFile: compose,
		},
	}
}

func loopbackHealth(t *testing.T) *httptest.Server {
	t.Helper()
	return healthAt(t, "127.0.0.1:0")
}

func healthAt(t *testing.T, addr string) *httptest.Server {
	t.Helper()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewUnstartedServer(mux)
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func freeLoopback(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

func joinContains(calls []string, verb string) bool {
	for _, c := range calls {
		if strings.Contains(c, verb) {
			return true
		}
	}
	return false
}
