package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"

	"github.com/sumedhaerram/aquila/internal/gitrev"
	"github.com/sumedhaerram/aquila/internal/jobs"
	"github.com/sumedhaerram/aquila/internal/plan"
)

// RunJob enqueues a plan as a durable DAG. It does not start Compose.
func RunJob(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("job", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	api := fs.String("api", envAPI(), "control-plane base URL")
	traces := fs.Int("traces", defaultTraces, "trace window (max 200)")
	file := fs.String("f", "", "diff file (default stdin)")
	base := fs.String("base", "", "baseline gateway URL")
	patch := fs.String("patch", "", "patch gateway URL")
	fixture := fs.Bool("fixture", false, "use shop smoke fixture instead of span routes")
	workload := fs.String("workload", "", "operator workload JSON (not derived from traces)")
	n := fs.Int("n", 0, "latency repeats (0 uses the plan default)")
	dir := fs.String("dir", ".", "module under change (default cwd)")
	service := fs.String("service", "", "OTEL service.name; scopes span-derived replay")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: job: %w", err)
	}
	if *base == "" || *patch == "" {
		return fmt.Errorf("cli: job: -base and -patch are required")
	}
	path, err := diffPath(*file, fs.Args())
	if err != nil {
		return fmt.Errorf("cli: job: %w", err)
	}
	raw, _, err := resolveDiff(ctx, stdin, path, *dir)
	if err != nil {
		return fmt.Errorf("cli: job: %w", err)
	}
	rep, _, err := analyzeDiff(ctx, *api, *dir, *traces, *service, raw)
	if err != nil {
		return err
	}
	dag, err := plan.FromImpact(rep)
	if err != nil {
		return err
	}
	if *n > 0 {
		dag = plan.WithLatencyN(dag, *n)
	}
	w, err := resolveWorkload(ctx, *api, *traces, *service, *fixture, *workload, *dir)
	if err != nil {
		return err
	}
	if len(w.Steps) == 0 {
		return fmt.Errorf("cli: job: empty workload")
	}
	sha, dirty, err := gitrev.State(ctx, *dir)
	if err != nil {
		return fmt.Errorf("cli: job: %w", err)
	}
	c, err := newClient(*api)
	if err != nil {
		return err
	}
	body, err := json.Marshal(jobs.CreateOpts{
		Baseline:    *base,
		Patch:       *patch,
		BaselineSHA: sha,
		Dirty:       dirty,
		Workload:    w,
		Plan:        dag,
	})
	if err != nil {
		return fmt.Errorf("cli: job: %w", err)
	}
	var job jobs.Job
	if err := c.postJSON(ctx, "/v1/jobs", "application/json", body, &job); err != nil {
		return err
	}
	if job.Validated {
		return fmt.Errorf("cli: job: store claimed validated")
	}
	writef(stdout, "job      id=%s  status=%s  tasks=%d\n", job.ID, job.Status, len(job.Tasks))
	for _, t := range job.Tasks {
		writef(stdout, "  %-14s %s\n", t.Kind, t.State)
	}
	writef(stdout, "not started by this command. aquila worker leases READY tasks.\n")
	writef(stdout, "not validated.\n")
	return nil
}

// RunJobs lists or shows persisted jobs.
func RunJobs(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("jobs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	api := fs.String("api", envAPI(), "control-plane base URL")
	limit := fs.Int("limit", jobs.DefaultList, "list limit (max 50)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: jobs: %w", err)
	}
	c, err := newClient(*api)
	if err != nil {
		return err
	}
	rest := fs.Args()
	switch len(rest) {
	case 0:
		var body struct {
			Jobs []jobs.Job `json:"jobs"`
		}
		if err := c.getJSON(ctx, "/v1/jobs?limit="+strconv.Itoa(*limit), &body); err != nil {
			return err
		}
		writef(stdout, "jobs     n=%d\n", len(body.Jobs))
		for _, j := range body.Jobs {
			writef(stdout, "  %s  %s  tasks=%d\n", j.ID, j.Status, len(j.Tasks))
		}
		return nil
	case 1:
		var job jobs.Job
		if err := c.getJSON(ctx, "/v1/jobs/"+rest[0], &job); err != nil {
			return err
		}
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		if err := enc.Encode(job); err != nil {
			return fmt.Errorf("cli: jobs: %w", err)
		}
		_, _ = stdout.Write(buf.Bytes())
		return nil
	default:
		return fmt.Errorf("cli: jobs: unexpected arguments")
	}
}
