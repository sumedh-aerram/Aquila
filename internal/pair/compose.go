package pair

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	healthPath     = "/healthz"
	healthPoll     = 400 * time.Millisecond
	healthProbe    = 2 * time.Second
	downTimeout    = 60 * time.Second
	maxComposeErr  = 512
	projectPrefix  = "aquila-env-"
	composeBinName = "docker"
)

var (
	lookPath   = exec.LookPath
	runCompose = dockerCompose
)

// Up starts both sides with docker compose and waits for gateway /healthz.
// It does not mount the Docker socket into shop containers.
func Up(ctx context.Context, env Env) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkEnv(env); err != nil {
		return err
	}
	if err := portsFree(env); err != nil {
		return err
	}
	started := make([]Side, 0, 2)
	fail := func(err error) error {
		_ = downSides(context.WithoutCancel(ctx), env, started)
		return err
	}
	for _, side := range []Side{env.Baseline, env.Patch} {
		if err := upSide(ctx, env, side); err != nil {
			return fail(err)
		}
		started = append(started, side)
	}
	return nil
}

// Down stops both compose projects. It is safe after a partial Up.
func Down(ctx context.Context, env Env) error {
	if err := checkEnv(env); err != nil {
		return err
	}
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), downTimeout)
	defer cancel()
	return downSides(stopCtx, env, []Side{env.Baseline, env.Patch})
}

func replaceRoot(ctx context.Context, root string) error {
	if env, err := loadManifest(root); err == nil {
		_ = Down(ctx, env)
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("pair: replace: %w", err)
	}
	return nil
}

func loadManifest(root string) (Env, error) {
	raw, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return Env{}, err
	}
	var env Env
	if err := json.Unmarshal(raw, &env); err != nil {
		return Env{}, fmt.Errorf("pair: manifest: %w", err)
	}
	if env.ID == "" || env.Root == "" {
		return Env{}, fmt.Errorf("pair: invalid manifest")
	}
	return env, nil
}

func upSide(ctx context.Context, env Env, side Side) error {
	args, err := composeArgs(env, side, "up", "-d", "--build")
	if err != nil {
		return err
	}
	if err := runCompose(ctx, env.Root, args); err != nil {
		return err
	}
	return waitHealthy(ctx, side.Gateway)
}

func downSides(ctx context.Context, env Env, sides []Side) error {
	var first error
	for i := len(sides) - 1; i >= 0; i-- {
		args, err := composeArgs(env, sides[i], "down", "--volumes", "--remove-orphans")
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if err := runCompose(ctx, env.Root, args); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func composeArgs(env Env, side Side, extra ...string) ([]string, error) {
	if err := checkSide(env, side); err != nil {
		return nil, err
	}
	args := []string{
		"compose",
		"--env-file", side.EnvFile,
		"-p", side.Project,
		"-f", env.ComposeFile,
	}
	return append(args, extra...), nil
}

func checkEnv(env Env) error {
	if env.ID == "" {
		return fmt.Errorf("pair: missing id")
	}
	if !isHexID(env.ID) {
		return fmt.Errorf("pair: invalid id")
	}
	if env.Root == "" {
		return fmt.Errorf("pair: missing root")
	}
	if err := checkSide(env, env.Baseline); err != nil {
		return err
	}
	return checkSide(env, env.Patch)
}

func checkSide(env Env, side Side) error {
	want := projectPrefix + env.ID + "-" + side.Role
	if side.Project != want {
		return fmt.Errorf("pair: unexpected compose project")
	}
	if !underRoot(env.Root, env.ComposeFile) || !underRoot(env.Root, side.EnvFile) {
		return fmt.Errorf("pair: compose paths must stay under the pair root")
	}
	host, port, err := net.SplitHostPort(side.Gateway)
	if err != nil {
		return fmt.Errorf("pair: gateway: %w", err)
	}
	if host != "127.0.0.1" && host != "localhost" {
		return fmt.Errorf("pair: gateway must be loopback")
	}
	if port == "" {
		return fmt.Errorf("pair: gateway missing port")
	}
	return nil
}

func portsFree(env Env) error {
	for _, side := range []Side{env.Baseline, env.Patch} {
		ln, err := net.Listen("tcp", side.Gateway)
		if err != nil {
			return fmt.Errorf("pair: port in use (%s)", side.Gateway)
		}
		_ = ln.Close()
	}
	return nil
}

func waitHealthy(ctx context.Context, gateway string) error {
	u := "http://" + gateway + healthPath
	client := &http.Client{Timeout: healthProbe}
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("pair: gateway %s not healthy: %w", gateway, err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return fmt.Errorf("pair: health: %w", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		timer := time.NewTimer(healthPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("pair: gateway %s not healthy: %w", gateway, ctx.Err())
		case <-timer.C:
		}
	}
}

func dockerCompose(ctx context.Context, dir string, args []string) error {
	if _, err := lookPath(composeBinName); err != nil {
		return fmt.Errorf("pair: docker is required to start environments")
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return fmt.Errorf("pair: compose directory")
	}
	cmd := exec.CommandContext(ctx, composeBinName, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return fmt.Errorf("pair: docker compose: %w", err)
		}
		if len(msg) > maxComposeErr {
			msg = msg[:maxComposeErr]
		}
		return fmt.Errorf("pair: docker compose: %s", msg)
	}
	return nil
}

func underRoot(root, path string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func isHexID(id string) bool {
	if len(id) != 12 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' {
			continue
		}
		return false
	}
	return true
}
