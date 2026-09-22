package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/sumedhaerram/aquila/internal/diff"
	"github.com/sumedhaerram/aquila/internal/impact"
)

// RunImpact posts a unified diff and prints direct, likely, runtime, and unobserved findings.
func RunImpact(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("impact", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	api := fs.String("api", envAPI(), "control-plane base URL")
	traces := fs.Int("traces", defaultTraces, "trace window (max 200)")
	file := fs.String("f", "", "diff file (default stdin)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: impact: %w", err)
	}
	path := *file
	if path == "" && fs.NArg() == 1 {
		path = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fmt.Errorf("cli: impact: unexpected argument %q", fs.Arg(0))
	}

	var raw []byte
	var err error
	if path != "" {
		raw, err = os.ReadFile(path)
	} else {
		raw, err = io.ReadAll(io.LimitReader(stdin, int64(diff.MaxBytes)+1))
	}
	if err != nil {
		return fmt.Errorf("cli: impact: %w", err)
	}
	if len(raw) == 0 {
		return fmt.Errorf("cli: impact: empty diff")
	}
	if len(raw) > diff.MaxBytes {
		return fmt.Errorf("cli: impact: diff too large")
	}

	c, err := newClient(*api)
	if err != nil {
		return err
	}
	n := clipTraces(*traces)
	var rep impact.Report
	if err := c.postJSON(ctx, "/v1/impact?traces="+strconv.Itoa(n), "text/plain", raw, &rep); err != nil {
		return err
	}
	writeImpact(stdout, rep)
	return nil
}

func writeImpact(w io.Writer, rep impact.Report) {
	writef(w, "impact  files=%d  direct=%d  likely=%d  runtime=%d  unobserved=%d\n",
		len(rep.Files), len(rep.Direct), len(rep.Likely), len(rep.Runtime), len(rep.Unobserved))
	writeSection(w, "direct", rep.Direct)
	writeSection(w, "likely", rep.Likely)
	writeSection(w, "runtime", rep.Runtime)
	writeSection(w, "unobserved", rep.Unobserved)
}

func writeSection(w io.Writer, title string, fs []impact.Finding) {
	writef(w, "%s\n", title)
	if len(fs) == 0 {
		writef(w, "  (none)\n")
		return
	}
	for _, f := range fs {
		switch {
		case f.Path != "":
			writef(w, "  %s  %s  %s\n", f.Path, f.Reason, f.Provenance)
		case f.Service != "":
			writef(w, "  %s  %s  %s  %s\n", f.Service, f.Name, f.Reason, f.Provenance)
		default:
			writef(w, "  %s  %s  %s  %s\n", f.Name, f.File, f.Reason, f.Provenance)
		}
	}
}
