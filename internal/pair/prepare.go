package pair

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sumedhaerram/aquila/internal/diff"
)

const (
	maxCopyBytes = 80 << 20
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
	ID             string `json:"id"`
	Root           string `json:"root"`
	Status         string `json:"status"`
	BaselineSHA    string `json:"baseline_sha,omitempty"`
	DirtyShop      bool   `json:"dirty_shop"`
	BaselineDigest string `json:"baseline_digest"`
	PatchDigest    string `json:"patch_digest"`
	ComposeFile    string `json:"compose_file"`
	Baseline       Side   `json:"baseline"`
	Patch          Side   `json:"patch"`
}

// PrepareOpts is the input for creating an environment pair.
type PrepareOpts struct {
	ShopDir   string
	Parent    string
	Diff      []byte
	BasePort  int
	PatchPort int
}

// Prepare copies the shop twice, applies the diff only to patch, and writes
// an equivalent compose file for both sides. It does not start containers.
func Prepare(ctx context.Context, opts PrepareOpts) (Env, error) {
	if err := ctx.Err(); err != nil {
		return Env{}, err
	}
	if len(opts.Diff) == 0 {
		return Env{}, fmt.Errorf("pair: empty diff")
	}
	shop, err := filepath.Abs(opts.ShopDir)
	if err != nil {
		return Env{}, fmt.Errorf("pair: shop: %w", err)
	}
	if err := requireShop(shop); err != nil {
		return Env{}, err
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

	baseDigest, err := digestDir(shop)
	if err != nil {
		return Env{}, err
	}
	patchDigest := sha256Hex(opts.Diff)
	id := sha256Hex([]byte(baseDigest + ":" + patchDigest))[:12]
	root := filepath.Join(parent, id)
	if _, err := os.Stat(root); err == nil {
		return Env{}, fmt.Errorf("pair: %s exists", id)
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
	if err := copyDir(shop, baseDir); err != nil {
		return Env{}, err
	}
	if err := copyDir(shop, patchDir); err != nil {
		return Env{}, err
	}
	if err := diff.Apply(patchDir, opts.Diff); err != nil {
		return Env{}, err
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

	sha, dirty := gitState(ctx, shop)
	env := Env{
		ID:             id,
		Root:           root,
		Status:         "prepared",
		BaselineSHA:    sha,
		DirtyShop:      dirty,
		BaselineDigest: baseDigest,
		PatchDigest:    patchDigest,
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

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func digestDir(root string) (string, error) {
	h := sha256.New()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if skipName(d.Name()) || skipRel(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("pair: refusing symlink %s", rel)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, _ = io.WriteString(h, rel)
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(raw)
		_, _ = h.Write([]byte{0})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("pair: digest: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyDir(src, dst string) error {
	var total int64
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if skipName(d.Name()) || skipRel(filepath.ToSlash(rel)) {
			if d.IsDir() && rel != "." {
				return filepath.SkipDir
			}
			if !d.IsDir() {
				return nil
			}
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("pair: refusing symlink %s", rel)
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		total += st.Size()
		if total > maxCopyBytes {
			return fmt.Errorf("pair: shop exceeds copy cap")
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode().Perm())
		if err != nil {
			return err
		}
		defer func() { _ = out.Close() }()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		return fmt.Errorf("pair: copy: %w", err)
	}
	return nil
}

func skipName(name string) bool {
	switch name {
	case ".git", "out", "bin", "node_modules":
		return true
	default:
		return false
	}
}

func skipRel(rel string) bool {
	return rel == ".git" || strings.HasPrefix(rel, ".git/")
}

func gitState(ctx context.Context, shop string) (sha string, dirty bool) {
	root := gitRoot(ctx, shop)
	if root == "" {
		return "", false
	}
	shaOut, err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", false
	}
	sha = strings.TrimSpace(string(shaOut))
	rel := shop
	if r, err := filepath.Rel(root, shop); err == nil {
		rel = r
	}
	st, err := exec.CommandContext(ctx, "git", "-C", root, "status", "--porcelain", "--", rel).Output()
	if err != nil {
		return sha, false
	}
	return sha, strings.TrimSpace(string(st)) != ""
}

func gitRoot(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
