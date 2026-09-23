package jobs

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/sumedhaerram/aquila/internal/plan"
)

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func taskID(jobID string, i int) string {
	if len(jobID) < 12 {
		jobID = jobID + "000000000000"
	}
	return jobID[:12] + fmt.Sprintf("%04x", i)
}

func buildJob(opts CreateOpts) (Job, error) {
	if err := validateCreate(opts); err != nil {
		return Job{}, err
	}
	id := newID()
	now := time.Now().UTC().Truncate(time.Millisecond)
	tasks := make([]Task, 0, len(opts.Plan.Steps))
	for i, s := range opts.Plan.Steps {
		t := Task{
			ID:        taskID(id, i),
			JobID:     id,
			PlanID:    s.ID,
			Kind:      s.Kind,
			Operator:  s.Operator,
			Required:  s.Required,
			DependsOn: append([]string(nil), s.DependsOn...),
			N:         s.N,
			State:     StatePending,
		}
		if s.Operator {
			t.State = StateSkipped
		}
		tasks = append(tasks, t)
	}
	settle(tasks)
	return Job{
		ID:          id,
		Created:     now,
		Status:      jobStatus(tasks),
		Baseline:    opts.Baseline,
		Patch:       opts.Patch,
		BaselineSHA: opts.BaselineSHA,
		Dirty:       opts.Dirty,
		Workload:    workloadOf(opts.Workload),
		Plan:        opts.Plan,
		Tasks:       tasks,
		Validated:   false,
	}, nil
}

func settle(tasks []Task) {
	for range tasks {
		changed := false
		by := stateByPlan(tasks)
		for i := range tasks {
			prev := tasks[i].State
			if tasks[i].Operator && tasks[i].State == StatePending {
				tasks[i].State = StateSkipped
			} else if tasks[i].State == StatePending && depsOK(tasks[i], by) {
				tasks[i].State = StateReady
			}
			if tasks[i].State != prev {
				changed = true
			}
		}
		if !changed {
			return
		}
	}
}

func stateByPlan(tasks []Task) map[string]string {
	m := make(map[string]string, len(tasks))
	for _, t := range tasks {
		m[t.PlanID] = t.State
	}
	return m
}

func depsOK(t Task, by map[string]string) bool {
	for _, d := range t.DependsOn {
		st := by[d]
		if st != StateSucceeded && st != StateSkipped {
			return false
		}
	}
	return true
}

func jobStatus(tasks []Task) string {
	ready, leased, failed, pending := false, false, false, false
	for _, t := range tasks {
		switch t.State {
		case StateFailed:
			failed = true
		case StateReady:
			ready = true
		case StateLeased:
			leased = true
		case StatePending:
			pending = true
		}
	}
	if failed {
		return StatusFailed
	}
	if leased || ready {
		return StatusRunning
	}
	if pending {
		return StatusPending
	}
	return StatusComplete
}

func applyCommit(t *Task, attempt string, result plan.StepResult, fail string) error {
	if t.State != StateLeased {
		return ErrNotLeased
	}
	if t.AttemptID != attempt {
		return ErrStaleAttempt
	}
	if fail != "" {
		t.State = StateFailed
		t.Err = fail
		t.Result = result
		t.WorkerID = ""
		t.LeaseUntil = time.Time{}
		return nil
	}
	if result.Verdict == "pass" || result.Verdict == "validated" {
		return fmt.Errorf("jobs: result cannot be a pass")
	}
	t.State = StateSucceeded
	t.Result = result
	t.Err = ""
	t.WorkerID = ""
	t.LeaseUntil = time.Time{}
	return nil
}

func requeue(t *Task, now time.Time) bool {
	if t.State != StateLeased {
		return false
	}
	if t.LeaseUntil.IsZero() || !t.LeaseUntil.Before(now) {
		return false
	}
	t.State = StateReady
	t.AttemptID = ""
	t.WorkerID = ""
	t.LeaseUntil = time.Time{}
	return true
}
