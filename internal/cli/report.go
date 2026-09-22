package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sumedhaerram/aquila/internal/evidence"
)

// RunReport prints Markdown derived from a saved evidence JSON file.
func RunReport(_ context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	mdPath := fs.String("out", "", "optional markdown file")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: report: %w", err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("cli: report: evidence json path is required")
	}
	path := fs.Arg(0)
	if strings.Contains(path, "://") {
		return fmt.Errorf("cli: report: path must be a local file")
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("cli: report: %w", err)
	}
	defer func() { _ = f.Close() }()
	a, err := evidence.Decode(f)
	if err != nil {
		return err
	}
	md := evidence.Markdown(a)
	if _, err := io.WriteString(stdout, md); err != nil {
		return fmt.Errorf("cli: report: %w", err)
	}
	if *mdPath == "" {
		return nil
	}
	if strings.Contains(*mdPath, "://") {
		return fmt.Errorf("cli: report: -out must be a local file")
	}
	if err := os.WriteFile(*mdPath, []byte(md), 0o644); err != nil {
		return fmt.Errorf("cli: report: %w", err)
	}
	return nil
}
