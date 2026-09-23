package nexec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const defaultTimeout = 5 * time.Second

// Spec is one native process run through aquila-exec.
type Spec struct {
	Args    []string
	Timeout time.Duration
	Memory  int64
	NetNone bool
}

// Result is the supervisor JSON written by aquila-exec --result.
type Result struct {
	ExitCode   int    `json:"exit_code"`
	Signal     int    `json:"signal"`
	TimedOut   bool   `json:"timed_out"`
	DurationMS int    `json:"duration_ms"`
	Error      string `json:"error"`
}

// Look returns aquila-exec from AQUILA_EXECUTOR, a sibling of this process, or the repo tree.
func Look() string {
	if p := strings.TrimSpace(os.Getenv("AQUILA_EXECUTOR")); p != "" {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), "aquila-exec")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	for _, rel := range []string{"bin/aquila-exec", "executor/aquila-exec"} {
		if p := lookUp(rel); p != "" {
			return p
		}
	}
	return ""
}

func lookUp(rel string) string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		cand := filepath.Join(dir, rel)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Run executes spec.Args under aquila-exec. It does not use Docker.
func Run(ctx context.Context, spec Spec) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if len(spec.Args) == 0 {
		return Result{}, fmt.Errorf("nexec: command required")
	}
	joined := strings.Join(spec.Args, " ")
	if strings.Contains(joined, "docker.sock") {
		return Result{}, fmt.Errorf("nexec: docker.sock is forbidden")
	}
	bin := Look()
	if bin == "" {
		return Result{}, fmt.Errorf("nexec: aquila-exec not found; build with make executor")
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	tmp, err := os.CreateTemp("", "aquila-exec-*.json")
	if err != nil {
		return Result{}, fmt.Errorf("nexec: result file: %w", err)
	}
	resultPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(resultPath) }()

	args := []string{
		"--timeout-ms", fmt.Sprintf("%d", timeout.Milliseconds()),
		"--result", resultPath,
	}
	if spec.Memory > 0 {
		args = append(args, "--as-bytes", fmt.Sprintf("%d", spec.Memory))
	}
	if spec.NetNone {
		args = append(args, "--net", "none")
	}
	args = append(args, "--")
	args = append(args, spec.Args...)

	runCtx, cancel := context.WithTimeout(ctx, timeout+2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, bin, args...)
	out, err := cmd.CombinedOutput()
	raw, readErr := os.ReadFile(resultPath)
	if readErr != nil {
		if err != nil {
			return Result{}, fmt.Errorf("nexec: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return Result{}, fmt.Errorf("nexec: result: %w", readErr)
	}
	var res Result
	if uerr := json.Unmarshal(raw, &res); uerr != nil {
		return Result{}, fmt.Errorf("nexec: result json: %w", uerr)
	}
	if res.Error != "" && !res.TimedOut {
		return res, fmt.Errorf("nexec: %s", res.Error)
	}
	return res, nil
}
