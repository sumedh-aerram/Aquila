package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/sumedhaerram/aquila/internal/version"
)

type healthBody struct {
	Status string `json:"status"`
}

type readyBody struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type versionBody struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// RunStatus prints control-plane health, readiness, and version.
func RunStatus(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	api := fs.String("api", envAPI(), "control-plane base URL")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: status: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("cli: status: unexpected argument %q", fs.Arg(0))
	}
	c, err := newClient(*api)
	if err != nil {
		return err
	}

	var health healthBody
	if err := c.getJSON(ctx, "/healthz", &health); err != nil {
		return err
	}
	var ready readyBody
	readyErr := c.getJSON(ctx, "/readyz", &ready)
	var ver versionBody
	if err := c.getJSON(ctx, "/version", &ver); err != nil {
		return err
	}

	writef(stdout, "api      %s\n", c.base)
	writef(stdout, "version  %s\n", ver.Version)
	if skew := versionSkew(ver.Version); skew != "" {
		writef(stdout, "skew     %s\n", skew)
	}
	writef(stdout, "health   %s\n", emptyDash(health.Status))
	if readyErr != nil {
		writef(stdout, "ready    unavailable\n")
		return readyErr
	}
	writef(stdout, "ready    %s\n", emptyDash(ready.Status))
	if !strings.EqualFold(ready.Status, "ready") {
		if ready.Error != "" {
			return fmt.Errorf("cli: api not ready: %s", ready.Error)
		}
		return fmt.Errorf("cli: api not ready")
	}
	return nil
}

// versionSkew names a CLI/API build mismatch. Artifacts from a newer CLI can
// carry fields an older API rejects, so the operator needs to rebuild.
func versionSkew(api string) string {
	cli := version.Version
	if api == cli || api == "" || cli == "" || cli == "dev" {
		return ""
	}
	return fmt.Sprintf("api=%s cli=%s; rebuild the control plane with make dev", api, cli)
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
