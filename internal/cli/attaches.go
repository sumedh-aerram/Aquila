package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/sumedhaerram/aquila/internal/ingest"
)

// RunAttaches lists OTEL service.name values seen in the span store.
func RunAttaches(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("attaches", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	api := fs.String("api", envAPI(), "control-plane base URL")
	limit := fs.Int("limit", ingest.DefaultAttachList, "list limit (max 50)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: attaches: %w", err)
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("cli: attaches: unexpected arguments")
	}
	c, err := newClient(*api)
	if err != nil {
		return err
	}
	var body struct {
		Attaches []ingest.Attach `json:"attaches"`
	}
	if err := c.getJSON(ctx, "/v1/attaches"+encodeListQuery(*limit, ""), &body); err != nil {
		return err
	}
	writef(stdout, "attaches n=%d\n", len(body.Attaches))
	for _, a := range body.Attaches {
		last := ""
		if !a.LastSeen.IsZero() {
			last = "  last=" + a.LastSeen.UTC().Format("2006-01-02T15:04:05Z")
		}
		writef(stdout, "  %-16s spans=%d%s\n", a.Service, a.Spans, last)
	}
	writef(stdout, "not tenants. aquila observe -service scopes one window.\n")
	return nil
}
