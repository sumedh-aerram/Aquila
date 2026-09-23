package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

const (
	StatusPending  = "pending"
	StatusRunning  = "running"
	StatusComplete = "complete"
	StatusFailed   = "failed"

	StatePending   = "pending"
	StateReady     = "ready"
	StateLeased    = "leased"
	StateSucceeded = "succeeded"
	StateFailed    = "failed"
	StateSkipped   = "skipped"

	idLen        = 16
	leaseTTL     = 15 * time.Second
	DefaultSlots = 1
	MaxSlots     = 8
	maxSteps     = 20
	maxBody      = 8 << 10
	DefaultList  = 20
	MaxList      = 50
)

var (
	// ErrNotFound is returned when a job or task id does not exist.
	ErrNotFound = errors.New("jobs: not found")
	// ErrStaleAttempt is returned when a commit uses an outdated attempt id.
	ErrStaleAttempt = errors.New("jobs: stale attempt")
	// ErrNotLeased is returned when commit targets a task that is not leased.
	ErrNotLeased = errors.New("jobs: not leased")
	// ErrNoReady is returned when Lease finds no READY executable task.
	ErrNoReady = errors.New("jobs: no ready task")
	// ErrCapacity is returned when a worker already holds its slot limit.
	ErrCapacity = errors.New("jobs: worker at capacity")
)

// Step is one operator-supplied request stored for a worker. Bodies are not
// derived from traces.
type Step struct {
	Method     string `json:"method"`
	Path       string `json:"path"`
	Provenance string `json:"provenance,omitempty"`
	Body       []byte `json:"body,omitempty"`
}

// Task is one DAG node. Operator tasks are skipped, never leased.
type Task struct {
	ID         string          `json:"id"`
	JobID      string          `json:"job_id"`
	PlanID     string          `json:"plan_id"`
	Kind       string          `json:"kind"`
	Operator   bool            `json:"operator,omitempty"`
	Required   bool            `json:"required"`
	DependsOn  []string        `json:"depends_on,omitempty"`
	N          int             `json:"n,omitempty"`
	State      string          `json:"state"`
	AttemptID  string          `json:"attempt_id,omitempty"`
	Fence      int64           `json:"fence,omitempty"`
	LeaseUntil time.Time       `json:"lease_until,omitempty"`
	WorkerID   string          `json:"worker_id,omitempty"`
	Result     plan.StepResult `json:"result,omitempty"`
	Err        string          `json:"error,omitempty"`
}

// Job is one experiment DAG. Validated is always false.
type Job struct {
	ID          string    `json:"id"`
	Created     time.Time `json:"created_at"`
	Status      string    `json:"status"`
	Baseline    string    `json:"baseline"`
	Patch       string    `json:"patch"`
	BaselineSHA string    `json:"baseline_sha,omitempty"`
	Dirty       bool      `json:"dirty,omitempty"`
	Workload    []Step    `json:"workload"`
	Plan        plan.DAG  `json:"plan"`
	Tasks       []Task    `json:"tasks"`
	Validated   bool      `json:"validated"`
}

// CreateOpts is the input for enqueueing a DAG.
type CreateOpts struct {
	Baseline    string          `json:"baseline"`
	Patch       string          `json:"patch"`
	BaselineSHA string          `json:"baseline_sha,omitempty"`
	Dirty       bool            `json:"dirty,omitempty"`
	Workload    replay.Workload `json:"workload"`
	Plan        plan.DAG        `json:"plan"`
}

// Lease is a worker claim on one executable task.
type Lease struct {
	Job     Job
	Task    Task
	N       int
	Fence   int64
	Attempt string
}

// Worker is one claimant for READY tasks. Slots defaults to 1.
type Worker struct {
	ID    string
	Slots int
}

func (w Worker) id() (string, error) {
	if strings.TrimSpace(w.ID) == "" {
		return "", fmt.Errorf("jobs: worker id required")
	}
	return w.ID, nil
}

func (w Worker) slots() int {
	if w.Slots < 1 {
		return DefaultSlots
	}
	if w.Slots > MaxSlots {
		return MaxSlots
	}
	return w.Slots
}

// Store persists experiment DAGs.
type Store interface {
	Create(ctx context.Context, opts CreateOpts) (Job, error)
	Get(ctx context.Context, id string) (Job, error)
	List(ctx context.Context, limit int) ([]Job, error)
	Lease(ctx context.Context, w Worker, now time.Time) (Lease, error)
	Heartbeat(ctx context.Context, taskID, attemptID, workerID string, now time.Time) error
	Commit(ctx context.Context, taskID, attemptID string, result plan.StepResult, fail string) (Task, error)
	RequeueExpired(ctx context.Context, now time.Time) (int, error)
}

func clipLimit(n int) int {
	if n <= 0 {
		return DefaultList
	}
	if n > MaxList {
		return MaxList
	}
	return n
}

func normalizeID(raw string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if len(s) < 8 || len(s) > 64 {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", false
		}
	}
	if len(s) > idLen {
		s = s[:idLen]
	}
	return s, true
}

func validateCreate(opts CreateOpts) error {
	if strings.TrimSpace(opts.Baseline) == "" || strings.TrimSpace(opts.Patch) == "" {
		return fmt.Errorf("jobs: baseline and patch are required")
	}
	if !strings.HasPrefix(opts.Baseline, "http://") && !strings.HasPrefix(opts.Baseline, "https://") {
		return fmt.Errorf("jobs: baseline must be http or https")
	}
	if !strings.HasPrefix(opts.Patch, "http://") && !strings.HasPrefix(opts.Patch, "https://") {
		return fmt.Errorf("jobs: patch must be http or https")
	}
	if len(opts.Plan.Steps) == 0 {
		return fmt.Errorf("jobs: empty plan")
	}
	if len(opts.Workload.Steps) == 0 {
		return fmt.Errorf("jobs: empty workload")
	}
	if len(opts.Workload.Steps) > maxSteps {
		return fmt.Errorf("jobs: too many workload steps")
	}
	for _, s := range opts.Workload.Steps {
		if len(s.Body) > maxBody {
			return fmt.Errorf("jobs: body too large")
		}
	}
	return nil
}

func workloadOf(w replay.Workload) []Step {
	out := make([]Step, 0, len(w.Steps))
	for _, s := range w.Steps {
		out = append(out, Step{Method: s.Method, Path: s.Path, Provenance: s.Provenance, Body: s.Body})
	}
	return out
}

func replayWorkload(steps []Step) replay.Workload {
	w := replay.Workload{Steps: make([]replay.Step, 0, len(steps))}
	for _, s := range steps {
		w.Steps = append(w.Steps, replay.Step{Method: s.Method, Path: s.Path, Provenance: s.Provenance, Body: s.Body})
	}
	return w
}

// Workload returns the replay workload stored on j.
func Workload(j Job) replay.Workload {
	return replayWorkload(j.Workload)
}
