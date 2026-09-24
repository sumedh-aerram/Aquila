package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/sumedhaerram/aquila/internal/gitrev"
	"github.com/sumedhaerram/aquila/internal/impact"
	"github.com/sumedhaerram/aquila/internal/ingest"
	"github.com/sumedhaerram/aquila/internal/jobs"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
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
	smoke := fs.Bool("smoke", false, "latency n=1; cannot validate")
	dir := fs.String("dir", ".", "module under change (default cwd)")
	service := fs.String("service", "", "OTEL service.name; scopes span-derived replay")
	health := fs.String("health", replay.DefaultHealthPath, "GET route the env step probes on both gateways")
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
	} else if *smoke {
		dag = plan.WithLatencyN(dag, 1)
	}
	w, err := resolveWorkload(ctx, *api, *traces, *service, *fixture, *workload, *dir)
	if err != nil {
		return err
	}
	if len(w.Steps) == 0 {
		return fmt.Errorf("cli: job: empty workload")
	}
	for _, s := range w.Steps {
		if len(s.Headers) > 0 {
			return fmt.Errorf("cli: job: workload headers are not stored in jobs (they may hold credentials); run aquila experiment locally")
		}
	}
	dag = plan.WithHealthPath(dag, *health)
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
		Service:     ingest.ClipService(*service),
		BaselineSHA: sha,
		Dirty:       dirty,
		Workload:    w,
		Plan:        dag,
		Impacted:    impact.RuntimeRoutes(rep),
		Direct:      len(rep.Direct),
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
	if job.Service != "" {
		writef(stdout, "service  %s\n", job.Service)
	}
	for _, t := range job.Tasks {
		writef(stdout, "  %-14s %s\n", t.Kind, t.State)
	}
	writef(stdout, "impacted %d routes; validated needs the workload to reach one\n", len(job.Impacted))
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
	service := fs.String("service", "", "filter by recorded OTEL service.name")
	asJSON := fs.Bool("json", false, "show: print the raw job document")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: jobs: %w", err)
	}
	c, err := newClient(*api)
	if err != nil {
		return err
	}
	rest := fs.Args()
	switch {
	case len(rest) == 0:
		var body struct {
			Jobs []jobs.Job `json:"jobs"`
		}
		if err := c.getJSON(ctx, "/v1/jobs"+encodeListQuery(*limit, *service), &body); err != nil {
			return err
		}
		writef(stdout, "jobs     n=%d\n", len(body.Jobs))
		for _, j := range body.Jobs {
			svc := ""
			if j.Service != "" {
				svc = "  " + j.Service
			}
			val := ""
			if j.Validated {
				val = "  validated"
			}
			writef(stdout, "  %s  %s  tasks=%d%s%s\n", j.ID, j.Status, len(j.Tasks), svc, val)
		}
		return nil
	case len(rest) == 2 && rest[0] == "cancel":
		var job jobs.Job
		if err := c.postJSON(ctx, "/v1/jobs/"+rest[1]+"/cancel", "application/json", []byte("{}"), &job); err != nil {
			return err
		}
		writef(stdout, "job      id=%s  status=%s\n", job.ID, job.Status)
		writef(stdout, "canceled. remaining READY tasks will not lease.\n")
		return nil
	case len(rest) == 1:
		var job jobs.Job
		if err := c.getJSON(ctx, "/v1/jobs/"+rest[0], &job); err != nil {
			return err
		}
		if *asJSON {
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetIndent("", "  ")
			if err := enc.Encode(job); err != nil {
				return fmt.Errorf("cli: jobs: %w", err)
			}
			_, _ = stdout.Write(buf.Bytes())
			return nil
		}
		writeJob(stdout, job)
		return nil
	default:
		return fmt.Errorf("cli: jobs: unexpected arguments")
	}
}

func writeJob(w io.Writer, j jobs.Job) {
	writef(w, "job      id=%s  status=%s  tasks=%d\n", j.ID, j.Status, len(j.Tasks))
	if j.Service != "" {
		writef(w, "service  %s\n", j.Service)
	}
	writef(w, "baseline %s\npatch    %s\n", j.Baseline, j.Patch)
	if j.BaselineSHA != "" {
		state := "clean"
		if j.Dirty {
			state = "dirty"
		}
		writef(w, "git      %s  %s\n", j.BaselineSHA, state)
	}
	results := make([]plan.StepResult, 0, len(j.Tasks))
	for _, t := range j.Tasks {
		results = append(results, t.Result)
	}
	refused := authRefused(results)
	for _, t := range j.Tasks {
		verdict := "-"
		if t.Result.Verdict != "" {
			verdict = t.Result.Verdict
		}
		writef(w, "  %-14s %-10s verdict=%-10s fence=%d  attempt=%s\n", t.Kind, t.State, verdict, t.Fence, t.AttemptID)
		for _, n := range t.Result.Notes {
			writef(w, "    notes      %s\n", n)
		}
		for _, lat := range t.Result.Latency {
			writef(w, "    %-6s %-22s %s%s\n", lat.Method, clipRoute(lat.Path), formatLatency(lat), refused[lat.Method+" "+lat.Path])
		}
	}
	writef(w, "validated  %t\n", j.Validated)
	if !j.Validated {
		writef(w, "not validated. match is not a pass.\n")
	}
}
