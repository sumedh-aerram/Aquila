package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultImage = "alpine:3.21"
	maxRuntime   = 30 * time.Second
)

// Spec is one OCI run. It never mounts the host Docker socket.
type Spec struct {
	Image   string
	Args    []string
	Memory  string
	CPUs    string
	PIDs    int
	Network string
}

func (s Spec) image() string {
	if strings.TrimSpace(s.Image) == "" {
		return defaultImage
	}
	return s.Image
}

func (s Spec) memory() string {
	if strings.TrimSpace(s.Memory) == "" {
		return "64m"
	}
	return s.Memory
}

func (s Spec) cpus() string {
	if strings.TrimSpace(s.CPUs) == "" {
		return "0.25"
	}
	return s.CPUs
}

func (s Spec) pids() string {
	n := s.PIDs
	if n < 1 {
		n = 32
	}
	return fmt.Sprintf("%d", n)
}

func (s Spec) network() string {
	if s.Network == "host" {
		return "host"
	}
	return "none"
}

// Args returns docker run arguments. Docker.sock is never attached.
func Args(s Spec) []string {
	out := []string{
		"run", "--rm",
		"--user", "65532:65532",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--read-only",
		"--network", s.network(),
		"--memory", s.memory(),
		"--cpus", s.cpus(),
		"--pids-limit", s.pids(),
		s.image(),
	}
	return append(out, s.Args...)
}

func hasDockerSock(args []string) bool {
	joined := strings.Join(args, " ")
	return strings.Contains(joined, "docker.sock")
}

// Run starts a bounded container. It is for untrusted commands, not shop Compose.
func Run(ctx context.Context, s Spec) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(s.Args) == 0 {
		return nil, fmt.Errorf("sandbox: command required")
	}
	args := Args(s)
	if hasDockerSock(args) {
		return nil, fmt.Errorf("sandbox: docker.sock is forbidden")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("sandbox: docker unavailable")
	}
	runCtx, cancel := context.WithTimeout(ctx, maxRuntime)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("sandbox: %w", err)
	}
	return out, nil
}
