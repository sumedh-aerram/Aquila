package worker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sumedhaerram/aquila/internal/action"
	"github.com/sumedhaerram/aquila/internal/jobs"
	"github.com/sumedhaerram/aquila/internal/plan"
)

// Execute runs one leased executable task. Operator tasks are refused.
func Execute(ctx context.Context, lease jobs.Lease) (plan.StepResult, error) {
	return ExecuteWith(ctx, lease, cacheFromEnv())
}

// ExecuteWith runs lease, using cache when non-nil.
func ExecuteWith(ctx context.Context, lease jobs.Lease, cache *action.Cache) (plan.StepResult, error) {
	if err := ctx.Err(); err != nil {
		return plan.StepResult{}, err
	}
	if !lease.Job.Deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, lease.Job.Deadline)
		defer cancel()
	}
	if lease.Task.Operator {
		return plan.StepResult{}, fmt.Errorf("worker: operator task")
	}
	key := action.FromLease(lease)
	if cache != nil {
		hit, ok, err := cache.Get(ctx, key)
		if err != nil {
			return plan.StepResult{}, err
		}
		if ok {
			return hit, nil
		}
	}
	n := lease.N
	if n < 1 {
		n = 1
	}
	dag := plan.DAG{Steps: []plan.Step{{
		ID:       lease.Task.PlanID,
		Kind:     lease.Task.Kind,
		Required: true,
		N:        n,
	}}}
	ev, err := plan.Execute(ctx, dag, lease.Job.Baseline, lease.Job.Patch, jobs.Workload(lease.Job))
	if err != nil {
		return plan.StepResult{}, err
	}
	if len(ev.Steps) == 0 {
		return plan.StepResult{}, fmt.Errorf("worker: empty result")
	}
	out := ev.Steps[0]
	if out.Verdict == "pass" || out.Verdict == "validated" {
		return plan.StepResult{}, fmt.Errorf("worker: result cannot be a pass")
	}
	if cache != nil {
		if err := cache.Put(ctx, key, out); err != nil {
			return plan.StepResult{}, err
		}
	}
	return out, nil
}

func cacheFromEnv() *action.Cache {
	root := strings.TrimSpace(os.Getenv("AQUILA_CAS_DIR"))
	if root == "" {
		return nil
	}
	c, err := action.Open(root)
	if err != nil {
		return nil
	}
	return c
}

const heartbeatEvery = 5 * time.Second

// Once leases one task, executes it, and commits. ErrNoReady is returned
// when the queue is empty.
func Once(ctx context.Context, store jobs.Store, workerID string) error {
	lease, err := store.Lease(ctx, jobs.Worker{ID: workerID}, time.Now().UTC())
	if err != nil {
		return err
	}
	beatCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go heartbeat(beatCtx, store, lease.Task.ID, lease.Attempt, workerID)
	res, execErr := Execute(ctx, lease)
	cancel()
	fail := ""
	if execErr != nil {
		fail = execErr.Error()
		res = plan.StepResult{ID: lease.Task.PlanID, Kind: lease.Task.Kind, Verdict: "incomplete"}
	}
	_, err = store.Commit(ctx, lease.Task.ID, lease.Attempt, res, fail)
	return err
}

func heartbeat(ctx context.Context, store jobs.Store, taskID, attempt, workerID string) {
	ticker := time.NewTicker(heartbeatEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			_ = store.Heartbeat(ctx, taskID, attempt, workerID, now.UTC())
		}
	}
}
