package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sumedhaerram/aquila/internal/pair"
)

// RunEnv prepares isolated baseline and patch shop trees. It does not start Compose.
func RunEnv(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("env", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	shop := fs.String("shop", defaultShop(), "shop module directory")
	outDir := fs.String("out", filepath.Join("out", "env"), "parent directory for environment pairs")
	file := fs.String("f", "", "diff file (default stdin)")
	basePort := fs.Int("base-port", 0, "baseline gateway host port (default 18180)")
	patchPort := fs.Int("patch-port", 0, "patch gateway host port (default 18280)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: env: %w", err)
	}
	path, err := diffPath(*file, fs.Args())
	if err != nil {
		return fmt.Errorf("cli: env: %w", err)
	}

	raw, err := slurpDiff(stdin, path)
	if err != nil {
		return fmt.Errorf("cli: env: %w", err)
	}

	env, err := pair.Prepare(ctx, pair.PrepareOpts{
		ShopDir:   *shop,
		Parent:    *outDir,
		Diff:      raw,
		BasePort:  *basePort,
		PatchPort: *patchPort,
	})
	if err != nil {
		return err
	}
	writeEnv(stdout, env)
	return nil
}

func writeEnv(w io.Writer, env pair.Env) {
	writef(w, "env       id=%s  status=%s\n", env.ID, env.Status)
	if env.BaselineSHA != "" {
		dirty := "clean"
		if env.DirtyShop {
			dirty = "dirty"
		}
		writef(w, "git       %s  shop=%s\n", env.BaselineSHA, dirty)
	}
	writef(w, "baseline  %s  %s  digest=%s\n", env.Baseline.Gateway, env.Baseline.Dir, shortDigest(env.BaselineDigest))
	writef(w, "patch     %s  %s  digest=%s\n", env.Patch.Gateway, env.Patch.Dir, shortDigest(env.PatchDigest))
	writef(w, "compose   %s\n", env.ComposeFile)
	writef(w, "up        docker compose --env-file %s -p %s -f %s up -d --build\n",
		env.Baseline.EnvFile, env.Baseline.Project, env.ComposeFile)
	writef(w, "up        docker compose --env-file %s -p %s -f %s up -d --build\n",
		env.Patch.EnvFile, env.Patch.Project, env.ComposeFile)
	writef(w, "not started. traces stay out of the live Aquila store.\n")
}

func shortDigest(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func defaultShop() string {
	if st, err := os.Stat("examples/shop"); err == nil && st.IsDir() {
		return "examples/shop"
	}
	return "examples/shop"
}
