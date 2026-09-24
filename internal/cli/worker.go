package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/sumedhaerram/aquila/internal/jobs"
	"github.com/sumedhaerram/aquila/internal/worker"
)

// RunWorker leases executable tasks over gRPC and commits results.
func RunWorker(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("worker", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("grpc", envWorker(), "worker gRPC address")
	id := fs.String("id", "aquila-worker", "worker id")
	slots := fs.Int("slots", jobs.DefaultSlots, "max concurrent leased tasks")
	once := fs.Bool("once", false, "lease at most one task and exit")
	jobID := fs.String("job", "", "lease only READY tasks for this job id")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("cli: worker: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("cli: worker: unexpected argument %q", fs.Arg(0))
	}
	if strings.TrimSpace(*jobID) != "" {
		id, err := jobs.ParseID(*jobID)
		if err != nil {
			return fmt.Errorf("cli: worker: %w", err)
		}
		*jobID = id
	}
	conn, err := worker.Dial(*addr, envToken())
	if err != nil {
		return fmt.Errorf("cli: worker: %w", err)
	}
	defer func() { _ = conn.Close() }()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := worker.RemoteOnceSlots(ctx, conn, *id, *slots, *jobID)
		if errors.Is(err, jobs.ErrNoReady) || errors.Is(err, jobs.ErrCapacity) {
			if *once {
				writef(stdout, "worker   idle\n")
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Second):
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("cli: worker: %w", err)
		}
		writef(stdout, "worker   committed  id=%s\n", *id)
		if *once {
			return nil
		}
	}
}

func envWorker() string {
	if v := strings.TrimSpace(os.Getenv("AQUILA_WORKER_URL")); v != "" {
		return v
	}
	return "127.0.0.1:8091"
}
