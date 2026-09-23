package sandbox

import (
	"os/exec"
	"strings"
	"testing"
)

func TestArgsDropCapabilitiesAndSocket(t *testing.T) {
	t.Parallel()
	args := Args(Spec{Args: []string{"echo", "ok"}})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "docker.sock") {
		t.Fatal(joined)
	}
	if !strings.Contains(joined, "--cap-drop ALL") {
		t.Fatal(joined)
	}
	if !strings.Contains(joined, "--network none") {
		t.Fatal(joined)
	}
	if !strings.Contains(joined, "--read-only") {
		t.Fatal(joined)
	}
}

func TestRunEcho(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	out, err := Run(t.Context(), Spec{Args: []string{"echo", "sandboxed"}})
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if !strings.Contains(string(out), "sandboxed") {
		t.Fatalf("%s", out)
	}
}
