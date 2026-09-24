package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sumedhaerram/aquila/internal/netguard"
	"github.com/sumedhaerram/aquila/internal/plan"
	"github.com/sumedhaerram/aquila/internal/replay"
)

const (
	StatusPending  = "pending"
	StatusRunning  = "running"
	StatusComplete = "complete"
	StatusFailed   = "failed"
	StatusCanceled = "canceled"

	StatePending   = "pending"
	StateReady     = "ready"
	StateLeased    = "leased"
	StateSucceeded = "succeeded"
	StateFailed    = "failed"
	StateSkipped   = "skipped"

	idLen           = 16
	leaseTTL        = 15 * time.Second
	DefaultSlots    = 1
	MaxSlots        = 8
	maxSteps        = 20
	maxBody         = 8 << 10
	DefaultList     = 20
	MaxList         = 50
	maxServiceName  = 128
	DefaultDeadline = 10 * time.Minute
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
	// ErrInvalidID is returned when a job id is not lowercase hex.
	ErrInvalidID = errors.New("jobs: invalid job id")
	// ErrBusy is returned when another unfinished job occupies a gateway host.
	ErrBusy = errors.New("jobs: gateway occupied")
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

// Job is one experiment DAG. Validated is earned after required tasks succeed.
type Job struct {
	ID          string    `json:"id"`
	Created     time.Time `json:"created_at"`
	Status      string    `json:"status"`
	Service     string    `json:"service,omitempty"`
	Baseline    string    `json:"baseline"`
	Patch       string    `json:"patch"`
	BaselineSHA string    `json:"baseline_sha,omitempty"`
	Dirty       bool      `json:"dirty,omitempty"`
	Deadline    time.Time `json:"deadline,omitempty"`
	Canceled    bool      `json:"canceled,omitempty"`
	Impacted    []string  `json:"impacted,omitempty"`
	Direct      int       `json:"direct,omitempty"`
	Workload    []Step    `json:"workload"`
	Plan        plan.DAG  `json:"plan"`
	Tasks       []Task    `json:"tasks"`
	Validated   bool      `json:"validated"`
}

// CreateOpts is the input for enqueueing a DAG.
type CreateOpts struct {
	Baseline    string          `json:"baseline"`
	Patch       string          `json:"patch"`
	Service     string          `json:"service,omitempty"`
	BaselineSHA string          `json:"baseline_sha,omitempty"`
	Dirty       bool            `json:"dirty,omitempty"`
	Deadline    time.Time       `json:"deadline,omitempty"`
	Workload    replay.Workload `json:"workload"`
	Plan        plan.DAG        `json:"plan"`
	Impacted    []string        `json:"impacted,omitempty"`
	Direct      int             `json:"direct,omitempty"`
}

type createWire struct {
	Baseline    string       `json:"baseline"`
	Patch       string       `json:"patch"`
	Service     string       `json:"service,omitempty"`
	BaselineSHA string       `json:"baseline_sha,omitempty"`
	Dirty       bool         `json:"dirty,omitempty"`
	Deadline    time.Time    `json:"deadline,omitempty"`
	Workload    workloadWire `json:"workload"`
	Plan        plan.DAG     `json:"plan"`
	Impacted    []string     `json:"impacted,omitempty"`
	Direct      int          `json:"direct,omitempty"`
}

type workloadWire struct {
	Steps []Step `json:"steps"`
}

// MarshalJSON keeps request bodies on the wire; replay.Step omits them so
// trace-derived workloads never serialize bodies.
func (o CreateOpts) MarshalJSON() ([]byte, error) {
	return json.Marshal(createWire{
		Baseline:    o.Baseline,
		Patch:       o.Patch,
		Service:     o.Service,
		BaselineSHA: o.BaselineSHA,
		Dirty:       o.Dirty,
		Deadline:    o.Deadline,
		Workload:    workloadWire{Steps: workloadOf(o.Workload)},
		Plan:        o.Plan,
		Impacted:    o.Impacted,
		Direct:      o.Direct,
	})
}

// UnmarshalJSON is the inverse of MarshalJSON.
func (o *CreateOpts) UnmarshalJSON(raw []byte) error {
	var w createWire
	if err := json.Unmarshal(raw, &w); err != nil {
		return err
	}
	*o = CreateOpts{
		Baseline:    w.Baseline,
		Patch:       w.Patch,
		Service:     w.Service,
		BaselineSHA: w.BaselineSHA,
		Dirty:       w.Dirty,
		Deadline:    w.Deadline,
		Workload:    replayWorkload(w.Workload.Steps),
		Plan:        w.Plan,
		Impacted:    w.Impacted,
		Direct:      w.Direct,
	}
	return nil
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
	JobID string
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
	List(ctx context.Context, limit int, service string) ([]Job, error)
	Lease(ctx context.Context, w Worker, now time.Time) (Lease, error)
	Heartbeat(ctx context.Context, taskID, attemptID, workerID string, now time.Time) error
	Commit(ctx context.Context, taskID, attemptID string, result plan.StepResult, fail string) (Task, error)
	RequeueExpired(ctx context.Context, now time.Time) (int, error)
	Cancel(ctx context.Context, id string) (Job, error)
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

func clipService(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxServiceName {
		s = s[:maxServiceName]
	}
	return s
}

// ParseID returns a lowercase hex job id or ErrInvalidID.
func ParseID(raw string) (string, error) {
	id, ok := normalizeID(raw)
	if !ok {
		return "", ErrInvalidID
	}
	return id, nil
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
	if err := netguard.CheckURL(opts.Baseline); err != nil {
		return fmt.Errorf("jobs: baseline: %w", err)
	}
	if err := netguard.CheckURL(opts.Patch); err != nil {
		return fmt.Errorf("jobs: patch: %w", err)
	}
	if len(opts.Plan.Steps) == 0 {
		return fmt.Errorf("jobs: empty plan")
	}
	if len(opts.Workload.Steps) == 0 {
		return fmt.Errorf("jobs: empty workload")
	}
	if !opts.Deadline.IsZero() && !opts.Deadline.After(time.Now().Add(-time.Second)) {
		return fmt.Errorf("jobs: deadline has passed")
	}
	if len(opts.Workload.Steps) > maxSteps {
		return fmt.Errorf("jobs: too many workload steps")
	}
	if len(opts.Impacted) > maxSteps {
		return fmt.Errorf("jobs: too many impacted routes")
	}
	for _, r := range opts.Impacted {
		if len(r) > 512 || strings.ContainsAny(r, "\r\n\x00") {
			return fmt.Errorf("jobs: invalid impacted route")
		}
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
