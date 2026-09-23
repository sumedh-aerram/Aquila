package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/sumedhaerram/aquila/internal/jobs"
	"github.com/sumedhaerram/aquila/internal/plan"
)

// Execute runs one leased executable task. Operator tasks are refused.
func Execute(ctx context.Context, lease jobs.Lease) (plan.StepResult, error) {
	if err := ctx.Err(); err != nil {
		return plan.StepResult{}, err
	}
	if lease.Task.Operator {
		return plan.StepResult{}, fmt.Errorf("worker: operator task")
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
	return out, nil
}

// Once leases one task, executes it, and commits. ErrNoReady is returned
// when the queue is empty.
func Once(ctx context.Context, store jobs.Store, workerID string) error {
	lease, err := store.Lease(ctx, workerID, time.Now().UTC())
	if err != nil {
		return err
	}
	res, execErr := Execute(ctx, lease)
	fail := ""
	if execErr != nil {
		fail = execErr.Error()
		res = plan.StepResult{ID: lease.Task.PlanID, Kind: lease.Task.Kind, Verdict: "incomplete"}
	}
	_, err = store.Commit(ctx, lease.Task.ID, lease.Attempt, res, fail)
	return err
}
