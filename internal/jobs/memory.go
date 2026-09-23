package jobs

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sumedhaerram/aquila/internal/plan"
)

// Memory is an in-process Store for tests.
type Memory struct {
	mu    sync.Mutex
	byID  map[string]Job
	order []string
}

// NewMemory returns an empty Memory store.
func NewMemory() *Memory {
	return &Memory{byID: make(map[string]Job)}
}

// Create implements Store.
func (m *Memory) Create(ctx context.Context, opts CreateOpts) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	job, err := buildJob(opts)
	if err != nil {
		return Job{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byID == nil {
		m.byID = make(map[string]Job)
	}
	m.byID[job.ID] = cloneJob(job)
	m.order = append(m.order, job.ID)
	return cloneJob(job), nil
}

// Get implements Store.
func (m *Memory) Get(ctx context.Context, id string) (Job, error) {
	if err := ctx.Err(); err != nil {
		return Job{}, err
	}
	id, ok := normalizeID(id)
	if !ok {
		return Job{}, fmt.Errorf("jobs: invalid id")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.byID[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	return cloneJob(job), nil
}

// List implements Store. Newest first.
func (m *Memory) List(ctx context.Context, limit int) ([]Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	limit = clipLimit(limit)
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Job, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, cloneJob(m.byID[id]))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Created.Equal(out[j].Created) {
			return out[i].ID > out[j].ID
		}
		return out[i].Created.After(out[j].Created)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// RequeueExpired implements Store.
func (m *Memory) RequeueExpired(ctx context.Context, now time.Time) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requeueExpiredLocked(now), nil
}

func (m *Memory) requeueExpiredLocked(now time.Time) int {
	n := 0
	for id, job := range m.byID {
		changed := false
		for i := range job.Tasks {
			if requeue(&job.Tasks[i], now) {
				n++
				changed = true
			}
		}
		if changed {
			settle(job.Tasks)
			job.Status = jobStatus(job.Tasks)
			m.byID[id] = job
		}
	}
	return n
}

// Lease implements Store.
func (m *Memory) Lease(ctx context.Context, w Worker, now time.Time) (Lease, error) {
	if err := ctx.Err(); err != nil {
		return Lease{}, err
	}
	workerID, err := w.id()
	if err != nil {
		return Lease{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = m.requeueExpiredLocked(now)
	if countLeased(m.jobs(), workerID) >= w.slots() {
		return Lease{}, ErrCapacity
	}
	for _, id := range m.order {
		job := m.byID[id]
		for i := range job.Tasks {
			t := &job.Tasks[i]
			if t.State != StateReady || t.Operator {
				continue
			}
			t.State = StateLeased
			t.AttemptID = newID()
			t.Fence++
			t.WorkerID = workerID
			t.LeaseUntil = now.Add(leaseTTL)
			job.Status = jobStatus(job.Tasks)
			m.byID[id] = job
			return Lease{
				Job:     cloneJob(job),
				Task:    job.Tasks[i],
				N:       t.N,
				Fence:   t.Fence,
				Attempt: t.AttemptID,
			}, nil
		}
	}
	return Lease{}, ErrNoReady
}

func (m *Memory) jobs() []Job {
	out := make([]Job, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.byID[id])
	}
	return out
}

// Heartbeat implements Store.
func (m *Memory) Heartbeat(ctx context.Context, taskID, attemptID, workerID string, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tid, ok := normalizeID(taskID)
	if !ok {
		return fmt.Errorf("jobs: invalid id")
	}
	aid, ok := normalizeID(attemptID)
	if !ok {
		return fmt.Errorf("jobs: invalid attempt")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, job := range m.byID {
		for i := range job.Tasks {
			if job.Tasks[i].ID != tid {
				continue
			}
			if err := applyHeartbeat(&job.Tasks[i], aid, workerID, now); err != nil {
				return err
			}
			m.byID[id] = job
			return nil
		}
	}
	return ErrNotFound
}

// Commit implements Store.
func (m *Memory) Commit(ctx context.Context, taskID, attemptID string, result plan.StepResult, fail string) (Task, error) {
	if err := ctx.Err(); err != nil {
		return Task{}, err
	}
	tid, ok := normalizeID(taskID)
	if !ok {
		return Task{}, fmt.Errorf("jobs: invalid id")
	}
	aid, ok := normalizeID(attemptID)
	if !ok {
		return Task{}, fmt.Errorf("jobs: invalid attempt")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, job := range m.byID {
		for i := range job.Tasks {
			if job.Tasks[i].ID != tid {
				continue
			}
			if err := applyCommit(&job.Tasks[i], aid, result, fail); err != nil {
				return Task{}, err
			}
			settle(job.Tasks)
			job.Status = jobStatus(job.Tasks)
			job.Validated = false
			m.byID[id] = job
			return job.Tasks[i], nil
		}
	}
	return Task{}, ErrNotFound
}

func cloneJob(j Job) Job {
	cp := j
	cp.Tasks = append([]Task(nil), j.Tasks...)
	cp.Workload = append([]Step(nil), j.Workload...)
	return cp
}
