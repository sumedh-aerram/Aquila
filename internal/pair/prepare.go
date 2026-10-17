package pair

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sumedhaerram/aquila/internal/gitrev"
)

const (
	defaultBase  = 18180
	defaultPatch = 18280
)

// Side is one isolated shop tree and the compose identity used to run it.
type Side struct {
	Role        string `json:"role"`
	Dir         string `json:"dir"`
	Gateway     string `json:"gateway"`
	Project     string `json:"project"`
	EnvFile     string `json:"env_file"`
	ComposeFile string `json:"compose_file"`
	Context     string `json:"context"`
	GatewayPort int    `json:"gateway_port"`
}

// Env is a baseline/patch pair. Status is prepared, never running.
type Env struct {
	ID             string          `json:"id"`
	Root           string          `json:"root"`
	Status         string          `json:"status"`
	BaselineRev    gitrev.Revision `json:"baseline_rev"`
	PatchRev       gitrev.Revision `json:"patch_rev"`
	BaselineDigest string          `json:"baseline_digest"`
	PatchDigest    string          `json:"patch_digest"`
	ComposeFile    string          `json:"compose_file"`
	Baseline       Side            `json:"baseline"`
	Patch          Side            `json:"patch"`
}

// PrepareOpts is the input for creating an environment pair.
type PrepareOpts struct {
	Repo      gitrev.Repo
	Baseline  gitrev.Revision
	Patch     gitrev.Revision
	Parent    string
	BasePort  int
	PatchPort int
	Replace   bool
}

// Prepare materializes the baseline and patch trees and writes equivalent shop
// compose environments. It does not start containers.
func Prepare(ctx context.Context, opts PrepareOpts) (Env, error) {
	if err := ctx.Err(); err != nil {
		return Env{}, err
	}
	if opts.Baseline.Tree == "" || opts.Patch.Tree == "" {
		return Env{}, fmt.Errorf("pair: missing revision tree")
	}
	parent, err := filepath.Abs(opts.Parent)
	if err != nil {
		return Env{}, fmt.Errorf("pair: out: %w", err)
	}
	basePort := opts.BasePort
	if basePort == 0 {
		basePort = defaultBase
	}
	patchPort := opts.PatchPort
	if patchPort == 0 {
		patchPort = defaultPatch
	}
	if err := checkPorts(basePort, patchPort); err != nil {
		return Env{}, err
	}

	sum := sha256.Sum256([]byte(opts.Baseline.Tree + ":" + opts.Patch.Tree))
	id := hex.EncodeToString(sum[:])[:12]
	root := filepath.Join(parent, id)
	if _, err := os.Stat(root); err == nil {
		if !opts.Replace {
			return Env{}, fmt.Errorf("pair: %s exists", id)
		}
		if err := replaceRoot(ctx, root); err != nil {
			return Env{}, err
		}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Env{}, fmt.Errorf("pair: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(root)
		}
	}()

	baseDir := filepath.Join(root, "baseline")
	patchDir := filepath.Join(root, "patch")
	if err := opts.Repo.Materialize(ctx, opts.Baseline.Tree, baseDir); err != nil {
		return Env{}, err
	}
	if err := opts.Repo.Materialize(ctx, opts.Patch.Tree, patchDir); err != nil {
		return Env{}, err
	}
	for _, dir := range []string{baseDir, patchDir} {
		if err := requireShop(dir); err != nil {
			return Env{}, err
		}
	}

	composePath := filepath.Join(root, "docker-compose.yaml")
	if err := writeEmbedded(composePath, "compose.yaml"); err != nil {
		return Env{}, err
	}
	if err := writeEmbedded(filepath.Join(root, "otel.yaml"), "otel.yaml"); err != nil {
		return Env{}, err
	}
	baseEnv := filepath.Join(root, "baseline.env")
	patchEnv := filepath.Join(root, "patch.env")
	if err := writeEnvFile(baseEnv, "./baseline", basePort); err != nil {
		return Env{}, err
	}
	if err := writeEnvFile(patchEnv, "./patch", patchPort); err != nil {
		return Env{}, err
	}

	env := Env{
		ID:             id,
		Root:           root,
		Status:         "prepared",
		BaselineRev:    opts.Baseline,
		PatchRev:       opts.Patch,
		BaselineDigest: opts.Baseline.Tree,
		PatchDigest:    opts.Patch.Tree,
		ComposeFile:    composePath,
		Baseline: Side{
			Role:        "baseline",
			Dir:         baseDir,
			Gateway:     fmt.Sprintf("127.0.0.1:%d", basePort),
			Project:     "aquila-env-" + id + "-baseline",
			EnvFile:     baseEnv,
			ComposeFile: composePath,
			Context:     "./baseline",
			GatewayPort: basePort,
		},
		Patch: Side{
			Role:        "patch",
			Dir:         patchDir,
			Gateway:     fmt.Sprintf("127.0.0.1:%d", patchPort),
			Project:     "aquila-env-" + id + "-patch",
			EnvFile:     patchEnv,
			ComposeFile: composePath,
			Context:     "./patch",
			GatewayPort: patchPort,
		},
	}
	raw, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return Env{}, fmt.Errorf("pair: manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), append(raw, '\n'), 0o644); err != nil {
		return Env{}, fmt.Errorf("pair: %w", err)
	}
	cleanup = false
	return env, nil
}

func requireShop(dir string) error {
	st, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("pair: shop: %w", err)
	}
	if !st.IsDir() {
		return fmt.Errorf("pair: shop %s is not a directory", dir)
	}
	for _, name := range []string{"go.mod", "Dockerfile"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("pair: shop missing %s", name)
		}
	}
	return nil
}

func checkPorts(base, patch int) error {
	if base < 1024 || patch < 1024 || base > 65535 || patch > 65535 {
		return fmt.Errorf("pair: ports must be 1024-65535")
	}
	if base == patch {
		return fmt.Errorf("pair: baseline and patch ports must differ")
	}
	return nil
}

func writeEnvFile(path, context string, port int) error {
	body := fmt.Sprintf("SHOP_CONTEXT=%s\nGATEWAY_PORT=%d\n", context, port)
	return os.WriteFile(path, []byte(body), 0o644)
}
