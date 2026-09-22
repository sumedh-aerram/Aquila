package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sumedhaerram/aquila/internal/evidence"
	"github.com/sumedhaerram/aquila/internal/runs"
)

type storedRun struct {
	ID             string            `json:"id"`
	Overall        string            `json:"overall"`
	Validated      bool              `json:"validated"`
	ArtifactDigest string            `json:"artifact_digest"`
	BaselineSHA    string            `json:"baseline_sha"`
	Dirty          bool              `json:"dirty"`
	Artifact       evidence.Artifact `json:"artifact"`
}

type storedRunList struct {
	Runs []storedRun `json:"runs"`
}

// RunRuns lists, shows, or imports persisted experiment evidence.
func RunRuns(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("runs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	api := fs.String("api", envAPI(), "control-plane base URL")
	file := fs.String("f", "", "import evidence JSON (does not imply a pass)")
	limit := fs.Int("limit", runs.DefaultList, "list limit (max 50)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: runs: %w", err)
	}
	rest := fs.Args()
	if *file != "" && len(rest) != 0 {
		return fmt.Errorf("cli: runs: -f cannot be combined with an id")
	}
	c, err := newClient(*api)
	if err != nil {
		return err
	}
	if *file != "" {
		return importRun(ctx, c, *file, stdout)
	}
	switch len(rest) {
	case 0:
		return listRuns(ctx, c, *limit, stdout)
	case 1:
		return getRun(ctx, c, rest[0], stdout)
	default:
		return fmt.Errorf("cli: runs: unexpected arguments")
	}
}

func importRun(ctx context.Context, c *Client, path string, stdout io.Writer) error {
	if strings.Contains(path, "://") {
		return fmt.Errorf("cli: runs: -f must be a local file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cli: runs: %w", err)
	}
	a, err := evidence.Decode(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	return postRun(ctx, c, a, stdout)
}

func listRuns(ctx context.Context, c *Client, limit int, stdout io.Writer) error {
	var body storedRunList
	if err := c.getJSON(ctx, fmt.Sprintf("/v1/runs?limit=%d", clipRunLimit(limit)), &body); err != nil {
		return err
	}
	writef(stdout, "runs      n=%d\n", len(body.Runs))
	for _, r := range body.Runs {
		if r.Validated {
			return fmt.Errorf("cli: runs: control plane claimed validated")
		}
		writef(stdout, "  %s\n", formatStored(r))
	}
	writef(stdout, "not validated. stored runs are not a pass.\n")
	return nil
}

func getRun(ctx context.Context, c *Client, id string, stdout io.Writer) error {
	norm, ok := runs.NormalizeID(id)
	if !ok {
		return fmt.Errorf("cli: runs: invalid id")
	}
	var body storedRun
	if err := c.getJSON(ctx, "/v1/runs/"+norm, &body); err != nil {
		return err
	}
	if body.Validated {
		return fmt.Errorf("cli: runs: control plane claimed validated")
	}
	writef(stdout, "run       %s\n", formatStored(body))
	if body.Artifact.Schema != "" {
		writeEvidence(stdout, body.Artifact.Result)
	} else {
		writef(stdout, "not validated. stored runs are not a pass.\n")
	}
	return nil
}

func postRun(ctx context.Context, c *Client, a evidence.Artifact, stdout io.Writer) error {
	raw, err := evidence.Marshal(a)
	if err != nil {
		return err
	}
	var out storedRun
	if err := c.postJSON(ctx, "/v1/runs", "application/json", raw, &out); err != nil {
		return err
	}
	if out.Validated {
		return fmt.Errorf("cli: runs: control plane claimed validated")
	}
	writef(stdout, "run       id=%s  stored\n", out.ID)
	writef(stdout, "not validated. stored runs are not a pass.\n")
	return nil
}

func recordRun(ctx context.Context, api string, stdout io.Writer, a evidence.Artifact) {
	c, err := newClient(api)
	if err != nil {
		writef(stdout, "unrecorded %s\n", err)
		return
	}
	raw, err := evidence.Marshal(a)
	if err != nil {
		writef(stdout, "unrecorded %s\n", err)
		return
	}
	var out storedRun
	if err := c.postJSON(ctx, "/v1/runs", "application/json", raw, &out); err != nil {
		writef(stdout, "unrecorded %s\n", err)
		return
	}
	if out.Validated {
		writef(stdout, "unrecorded control plane claimed validated\n")
		return
	}
	writef(stdout, "run       id=%s  stored\n", out.ID)
}

func formatStored(r storedRun) string {
	var b strings.Builder
	b.WriteString("id=" + r.ID + "  overall=" + r.Overall)
	if r.BaselineSHA != "" {
		b.WriteString("  sha=" + r.BaselineSHA)
	}
	if r.Dirty {
		b.WriteString("  dirty")
	}
	if r.ArtifactDigest != "" {
		dig := r.ArtifactDigest
		if len(dig) > 12 {
			dig = dig[:12]
		}
		b.WriteString("  digest=" + dig)
	}
	return b.String()
}

func clipRunLimit(n int) int {
	if n <= 0 {
		return runs.DefaultList
	}
	if n > runs.MaxList {
		return runs.MaxList
	}
	return n
}
