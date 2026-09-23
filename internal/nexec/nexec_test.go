package nexec

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLookEmptyWithoutBinary(t *testing.T) {
	t.Setenv("AQUILA_EXECUTOR", filepath.Join(t.TempDir(), "missing"))
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if p := Look(); p != "" {
		t.Fatalf("look=%q", p)
	}
}

func TestRunRejectsDockerSock(t *testing.T) {
	t.Parallel()
	_, err := Run(t.Context(), Spec{Args: []string{"echo", "/var/run/docker.sock"}})
	if err == nil || !strings.Contains(err.Error(), "docker.sock") {
		t.Fatalf("got %v", err)
	}
}

func TestRunEchoAndTimeout(t *testing.T) {
	bin := buildExecutor(t)
	t.Setenv("AQUILA_EXECUTOR", bin)

	res, err := Run(t.Context(), Spec{Args: []string{"/bin/echo", "nexec"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.TimedOut {
		t.Fatalf("%+v", res)
	}

	res, err = Run(t.Context(), Spec{Args: []string{"/bin/sleep", "5"}, Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut {
		t.Fatalf("want timeout %+v", res)
	}

	if runtime.GOOS != "linux" {
		_, err := Run(t.Context(), Spec{Args: []string{"/bin/true"}, NetNone: true})
		if err == nil || !strings.Contains(err.Error(), "linux") {
			t.Fatalf("darwin net none: %v", err)
		}
	}
}

func buildExecutor(t *testing.T) string {
	t.Helper()
	root := moduleRoot(t)
	cmd := exec.Command("make", "-C", filepath.Join(root, "executor"), "aquila-exec")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make executor: %v\n%s", err, out)
	}
	bin := filepath.Join(root, "executor", "aquila-exec")
	if st, err := os.Stat(bin); err != nil || st.IsDir() {
		t.Fatalf("missing %s", bin)
	}
	return bin
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
