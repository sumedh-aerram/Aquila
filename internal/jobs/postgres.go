package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sumedhaerram/aquila/internal/plan"
)

// Postgres stores jobs in aquila.jobs.
type Postgres struct {
	pool *pgxpool.Pool
}

// NewPostgres returns a Store backed by pool. pool must be non-nil.
func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

const insertJobSQL = `
INSERT INTO aquila.jobs (id, created_at, status, baseline, patch, baseline_sha, dirty, validated, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, FALSE, $8)
`

const getJobSQL = `
SELECT payload FROM aquila.jobs WHERE id = $1
`

const listJobSQL = `
SELECT payload FROM aquila.jobs
ORDER BY created_at DESC, id DESC
LIMIT $1
`

const lockJobsSQL = `
SELECT id, payload FROM aquila.jobs
WHERE status IN ('pending', 'running')
ORDER BY created_at ASC, id ASC
FOR UPDATE
`

const updateJobSQL = `
UPDATE aquila.jobs SET status = $2, payload = $3, validated = FALSE WHERE id = $1
`

// Create implements Store.
func (p *Postgres) Create(ctx context.Context, opts CreateOpts) (Job, error) {
	if p == nil || p.pool == nil {
		return Job{}, fmt.Errorf("postgres jobs is not configured")
	}
	job, err := buildJob(opts)
	if err != nil {
		return Job{}, err
	}
	raw, err := json.Marshal(job)
	if err != nil {
		return Job{}, fmt.Errorf("jobs: %w", err)
	}
	_, err = p.pool.Exec(ctx, insertJobSQL, job.ID, job.Created, job.Status, job.Baseline, job.Patch, job.BaselineSHA, job.Dirty, raw)
	if err != nil {
		return Job{}, fmt.Errorf("insert job: %w", err)
	}
	return job, nil
}

// Get implements Store.
func (p *Postgres) Get(ctx context.Context, id string) (Job, error) {
	id, ok := normalizeID(id)
	if !ok {
		return Job{}, fmt.Errorf("jobs: invalid id")
	}
	var raw []byte
	err := p.pool.QueryRow(ctx, getJobSQL, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("get job: %w", err)
	}
	return decodeJob(raw)
}

// List implements Store.
func (p *Postgres) List(ctx context.Context, limit int) ([]Job, error) {
	limit = clipLimit(limit)
	rows, err := p.pool.Query(ctx, listJobSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("list jobs: %w", err)
		}
		job, err := decodeJob(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

// RequeueExpired implements Store.
func (p *Postgres) RequeueExpired(ctx context.Context, now time.Time) (int, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("jobs: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	n, err := expireTx(ctx, tx, now)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("jobs: %w", err)
	}
	return n, nil
}

// Lease implements Store.
func (p *Postgres) Lease(ctx context.Context, w Worker, now time.Time) (Lease, error) {
	workerID, err := w.id()
	if err != nil {
		return Lease{}, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Lease{}, fmt.Errorf("jobs: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := expireTx(ctx, tx, now); err != nil {
		return Lease{}, err
	}
	found, err := lockJobs(ctx, tx)
	if err != nil {
		return Lease{}, err
	}
	all := make([]Job, 0, len(found))
	for _, r := range found {
		all = append(all, r.job)
	}
	if countLeased(all, workerID) >= w.slots() {
		if err := tx.Commit(ctx); err != nil {
			return Lease{}, fmt.Errorf("jobs: %w", err)
		}
		return Lease{}, ErrCapacity
	}
	for _, r := range found {
		job := r.job
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
			raw, err := json.Marshal(job)
			if err != nil {
				return Lease{}, fmt.Errorf("jobs: %w", err)
			}
			if _, err := tx.Exec(ctx, updateJobSQL, job.ID, job.Status, raw); err != nil {
				return Lease{}, fmt.Errorf("jobs: %w", err)
			}
			if err := tx.Commit(ctx); err != nil {
				return Lease{}, fmt.Errorf("jobs: %w", err)
			}
			return Lease{Job: job, Task: job.Tasks[i], N: t.N, Fence: t.Fence, Attempt: t.AttemptID}, nil
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Lease{}, fmt.Errorf("jobs: %w", err)
	}
	return Lease{}, ErrNoReady
}

type jobRow struct {
	id  string
	job Job
}

func lockJobs(ctx context.Context, tx pgx.Tx) ([]jobRow, error) {
	rows, err := tx.Query(ctx, lockJobsSQL)
	if err != nil {
		return nil, fmt.Errorf("jobs: %w", err)
	}
	defer rows.Close()
	var found []jobRow
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, fmt.Errorf("jobs: %w", err)
		}
		job, err := decodeJob(raw)
		if err != nil {
			return nil, err
		}
		found = append(found, jobRow{id: id, job: job})
	}
	return found, rows.Err()
}

// Heartbeat implements Store.
func (p *Postgres) Heartbeat(ctx context.Context, taskID, attemptID, workerID string, now time.Time) error {
	tid, ok := normalizeID(taskID)
	if !ok {
		return fmt.Errorf("jobs: invalid id")
	}
	aid, ok := normalizeID(attemptID)
	if !ok {
		return fmt.Errorf("jobs: invalid attempt")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("jobs: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	found, err := lockJobs(ctx, tx)
	if err != nil {
		return err
	}
	for _, r := range found {
		job := r.job
		for i := range job.Tasks {
			if job.Tasks[i].ID != tid {
				continue
			}
			if err := applyHeartbeat(&job.Tasks[i], aid, workerID, now); err != nil {
				return err
			}
			raw, err := json.Marshal(job)
			if err != nil {
				return fmt.Errorf("jobs: %w", err)
			}
			if _, err := tx.Exec(ctx, updateJobSQL, job.ID, job.Status, raw); err != nil {
				return fmt.Errorf("jobs: %w", err)
			}
			return tx.Commit(ctx)
		}
	}
	return ErrNotFound
}

// Commit implements Store.
func (p *Postgres) Commit(ctx context.Context, taskID, attemptID string, result plan.StepResult, fail string) (Task, error) {
	tid, ok := normalizeID(taskID)
	if !ok {
		return Task{}, fmt.Errorf("jobs: invalid id")
	}
	aid, ok := normalizeID(attemptID)
	if !ok {
		return Task{}, fmt.Errorf("jobs: invalid attempt")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Task{}, fmt.Errorf("jobs: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, lockJobsSQL)
	if err != nil {
		return Task{}, fmt.Errorf("jobs: %w", err)
	}
	var jobs []Job
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return Task{}, fmt.Errorf("jobs: %w", err)
		}
		job, err := decodeJob(raw)
		if err != nil {
			rows.Close()
			return Task{}, err
		}
		jobs = append(jobs, job)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Task{}, err
	}
	for _, job := range jobs {
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
			raw, err := json.Marshal(job)
			if err != nil {
				return Task{}, fmt.Errorf("jobs: %w", err)
			}
			if _, err := tx.Exec(ctx, updateJobSQL, job.ID, job.Status, raw); err != nil {
				return Task{}, fmt.Errorf("jobs: %w", err)
			}
			if err := tx.Commit(ctx); err != nil {
				return Task{}, fmt.Errorf("jobs: %w", err)
			}
			return job.Tasks[i], nil
		}
	}
	return Task{}, ErrNotFound
}

func expireTx(ctx context.Context, tx pgx.Tx, now time.Time) (int, error) {
	rows, err := tx.Query(ctx, lockJobsSQL)
	if err != nil {
		return 0, fmt.Errorf("jobs: %w", err)
	}
	n := 0
	var jobs []Job
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return 0, fmt.Errorf("jobs: %w", err)
		}
		job, err := decodeJob(raw)
		if err != nil {
			rows.Close()
			return 0, err
		}
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
			jobs = append(jobs, job)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, job := range jobs {
		raw, err := json.Marshal(job)
		if err != nil {
			return 0, fmt.Errorf("jobs: %w", err)
		}
		if _, err := tx.Exec(ctx, updateJobSQL, job.ID, job.Status, raw); err != nil {
			return 0, fmt.Errorf("jobs: %w", err)
		}
	}
	return n, nil
}

func decodeJob(raw []byte) (Job, error) {
	var job Job
	if err := json.Unmarshal(raw, &job); err != nil {
		return Job{}, fmt.Errorf("jobs: %w", err)
	}
	if job.Validated {
		return Job{}, fmt.Errorf("jobs: validated must be false")
	}
	return job, nil
}
