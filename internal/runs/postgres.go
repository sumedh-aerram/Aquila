package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sumedhaerram/aquila/internal/evidence"
)

// Postgres stores experiment runs in aquila.runs.
type Postgres struct {
	pool *pgxpool.Pool
}

// NewPostgres returns a Store backed by pool. pool must be non-nil.
func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

const insertSQL = `
INSERT INTO aquila.runs (
    id, recorded_at, baseline_sha, dirty, workload_digest, artifact_digest, overall, validated, artifact
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, FALSE, $8
)
ON CONFLICT (id) DO NOTHING
RETURNING id
`

const getSQL = `
SELECT id, recorded_at, baseline_sha, dirty, workload_digest, artifact_digest, overall, validated, artifact
FROM aquila.runs
WHERE id = $1
`

const listSQL = `
SELECT id, recorded_at, baseline_sha, dirty, workload_digest, artifact_digest, overall, validated
FROM aquila.runs
ORDER BY recorded_at DESC, id DESC
LIMIT $1
`

// Insert implements Store. Duplicate ids with the same digest are idempotent.
func (p *Postgres) Insert(ctx context.Context, rec Record) (Record, error) {
	if p == nil || p.pool == nil {
		return Record{}, fmt.Errorf("postgres runs is not configured")
	}
	prepared, err := prepareInsert(rec)
	if err != nil {
		return Record{}, err
	}
	raw, err := json.Marshal(prepared.Artifact)
	if err != nil {
		return Record{}, fmt.Errorf("runs: %w", err)
	}
	var id string
	err = p.pool.QueryRow(ctx, insertSQL,
		prepared.ID, prepared.Recorded, prepared.BaselineSHA, prepared.Dirty,
		prepared.WorkloadDigest, prepared.ArtifactDigest, prepared.Overall, raw,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := p.Get(ctx, prepared.ID)
		if getErr != nil {
			return Record{}, getErr
		}
		if existing.ArtifactDigest != prepared.ArtifactDigest {
			return Record{}, fmt.Errorf("runs: id collision")
		}
		return existing, nil
	}
	if err != nil {
		return Record{}, fmt.Errorf("insert run: %w", err)
	}
	return prepared, nil
}

// Get implements Store.
func (p *Postgres) Get(ctx context.Context, id string) (Record, error) {
	if p == nil || p.pool == nil {
		return Record{}, fmt.Errorf("postgres runs is not configured")
	}
	id, ok := NormalizeID(id)
	if !ok {
		return Record{}, fmt.Errorf("runs: invalid id")
	}
	var rec Record
	var raw []byte
	err := p.pool.QueryRow(ctx, getSQL, id).Scan(
		&rec.ID, &rec.Recorded, &rec.BaselineSHA, &rec.Dirty,
		&rec.WorkloadDigest, &rec.ArtifactDigest, &rec.Overall, &rec.Validated, &raw,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("get run: %w", err)
	}
	if rec.Validated {
		return Record{}, fmt.Errorf("runs: stored validated claim")
	}
	var a evidence.Artifact
	if err := json.Unmarshal(raw, &a); err != nil {
		return Record{}, fmt.Errorf("runs: %w", err)
	}
	if err := evidence.Check(a); err != nil {
		return Record{}, err
	}
	rec.Artifact = a
	rec.Recorded = rec.Recorded.UTC()
	return rec, nil
}

// List implements Store. Artifact bodies are omitted.
func (p *Postgres) List(ctx context.Context, limit int) ([]Record, error) {
	if p == nil || p.pool == nil {
		return nil, fmt.Errorf("postgres runs is not configured")
	}
	limit = clipLimit(limit)
	rows, err := p.pool.Query(ctx, listSQL, limit)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	out := make([]Record, 0, limit)
	for rows.Next() {
		var rec Record
		if err := rows.Scan(
			&rec.ID, &rec.Recorded, &rec.BaselineSHA, &rec.Dirty,
			&rec.WorkloadDigest, &rec.ArtifactDigest, &rec.Overall, &rec.Validated,
		); err != nil {
			return nil, fmt.Errorf("list runs: %w", err)
		}
		if rec.Validated {
			return nil, fmt.Errorf("runs: stored validated claim")
		}
		rec.Recorded = rec.Recorded.UTC()
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	return out, nil
}

var _ Store = (*Postgres)(nil)
