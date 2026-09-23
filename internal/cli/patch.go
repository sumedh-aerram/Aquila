package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sumedhaerram/aquila/internal/rewrite"
)

// RunPatch prints a candidate unified diff for a known shop rewrite.
// It does not write the module tree and does not claim the change is safe.
func RunPatch(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("patch", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	api := fs.String("api", envAPI(), "control-plane base URL")
	traces := fs.Int("traces", defaultTraces, "trace window (max 200)")
	dir := fs.String("dir", ".", "module under change (default cwd)")
	file := fs.String("f", "", "diff file (default stdin)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: patch: %w", err)
	}
	path, err := diffPath(*file, fs.Args())
	if err != nil {
		return fmt.Errorf("cli: patch: %w", err)
	}
	raw, _, err := resolveDiff(ctx, stdin, path, *dir)
	if err != nil {
		return fmt.Errorf("cli: patch: %w", err)
	}
	rep, _, err := analyzeDiff(ctx, *api, *dir, *traces, "", raw)
	if err != nil {
		return err
	}
	out, err := rewrite.D1(patchModule(ctx, *dir), rep.Files)
	if err != nil {
		return fmt.Errorf("cli: patch: %w", err)
	}
	_, _ = stdout.Write(out)
	writef(stdout, "not applied. not validated. candidate only.\n")
	return nil
}

func patchModule(ctx context.Context, dir string) string {
	if loadTargetSource(ctx, dir) != nil {
		return dir
	}
	shop := filepath.Join(dir, "examples", "shop")
	if _, err := os.Stat(filepath.Join(shop, filepath.FromSlash("internal/payment/handler.go"))); err == nil {
		return shop
	}
	return dir
}
