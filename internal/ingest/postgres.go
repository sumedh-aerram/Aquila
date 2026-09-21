package ingest

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres stores spans in aquila.spans.
type Postgres struct {
	pool *pgxpool.Pool
}

// NewPostgres returns a Store backed by pool. pool must be non-nil.
func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{pool: pool}
}

const upsertSQL = `
INSERT INTO aquila.spans (
    trace_id, span_id, parent_span_id, service_name, span_name, span_kind,
    status_code, http_method, http_route, http_status, code_function, code_file,
    start_time, duration_ns
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11, $12,
    $13, $14
)
ON CONFLICT (trace_id, span_id) DO UPDATE SET
    parent_span_id = EXCLUDED.parent_span_id,
    service_name = EXCLUDED.service_name,
    span_name = EXCLUDED.span_name,
    span_kind = EXCLUDED.span_kind,
    status_code = EXCLUDED.status_code,
    http_method = EXCLUDED.http_method,
    http_route = EXCLUDED.http_route,
    http_status = EXCLUDED.http_status,
    code_function = EXCLUDED.code_function,
    code_file = EXCLUDED.code_file,
    start_time = EXCLUDED.start_time,
    duration_ns = EXCLUDED.duration_ns,
    received_at = now()
`

// UpsertSpans implements Store.
func (p *Postgres) UpsertSpans(ctx context.Context, spans []Span) error {
	if p == nil || p.pool == nil {
		return fmt.Errorf("postgres ingest is not configured")
	}
	if len(spans) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, s := range spans {
		var httpStatus any
		if s.HTTPStatus > 0 {
			httpStatus = s.HTTPStatus
		}
		batch.Queue(upsertSQL,
			s.TraceID, s.SpanID, s.ParentSpanID, s.ServiceName, s.Name, s.Kind,
			s.StatusCode, s.HTTPMethod, s.HTTPRoute, httpStatus, s.CodeFunction, s.CodeFile,
			s.StartTime, s.DurationNS,
		)
	}
	br := p.pool.SendBatch(ctx, batch)
	defer func() { _ = br.Close() }()
	for range spans {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("upsert span: %w", err)
		}
	}
	return nil
}

const listSQL = `
SELECT trace_id, span_id, parent_span_id, service_name, span_name, span_kind,
       status_code, http_method, http_route, COALESCE(http_status, 0),
       code_function, code_file, start_time, duration_ns
  FROM aquila.spans
 WHERE ($1 = '' OR trace_id = $1)
   AND ($2 = '' OR service_name = $2)
 ORDER BY start_time DESC
 LIMIT $3
`

// ListSpans implements Store.
func (p *Postgres) ListSpans(ctx context.Context, q ListQuery) ([]Span, error) {
	if p == nil || p.pool == nil {
		return nil, fmt.Errorf("postgres ingest is not configured")
	}
	rows, err := p.pool.Query(ctx, listSQL, q.TraceID, q.Service, normalizeLimit(q.Limit))
	if err != nil {
		return nil, fmt.Errorf("list spans: %w", err)
	}
	defer rows.Close()
	out := make([]Span, 0)
	for rows.Next() {
		var s Span
		if err := rows.Scan(
			&s.TraceID, &s.SpanID, &s.ParentSpanID, &s.ServiceName, &s.Name, &s.Kind,
			&s.StatusCode, &s.HTTPMethod, &s.HTTPRoute, &s.HTTPStatus,
			&s.CodeFunction, &s.CodeFile, &s.StartTime, &s.DurationNS,
		); err != nil {
			return nil, fmt.Errorf("scan span: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

const traceWindowSQL = `
WITH recent AS (
    SELECT trace_id
      FROM aquila.spans
     GROUP BY trace_id
     ORDER BY MAX(start_time) DESC
     LIMIT $1
)
SELECT s.trace_id, s.span_id, s.parent_span_id, s.service_name, s.span_name, s.span_kind,
       s.status_code, s.http_method, s.http_route, COALESCE(s.http_status, 0),
       s.code_function, s.code_file, s.start_time, s.duration_ns
  FROM aquila.spans s
  JOIN recent r ON r.trace_id = s.trace_id
 ORDER BY s.start_time ASC, s.span_id ASC
 LIMIT $2
`

// ListTraceWindow implements Store.
func (p *Postgres) ListTraceWindow(ctx context.Context, maxTraces int) ([]Span, error) {
	if p == nil || p.pool == nil {
		return nil, fmt.Errorf("postgres ingest is not configured")
	}
	rows, err := p.pool.Query(ctx, traceWindowSQL, normalizeTraceWindow(maxTraces), maxWindowSpans)
	if err != nil {
		return nil, fmt.Errorf("list trace window: %w", err)
	}
	defer rows.Close()
	out := make([]Span, 0)
	for rows.Next() {
		var s Span
		if err := rows.Scan(
			&s.TraceID, &s.SpanID, &s.ParentSpanID, &s.ServiceName, &s.Name, &s.Kind,
			&s.StatusCode, &s.HTTPMethod, &s.HTTPRoute, &s.HTTPStatus,
			&s.CodeFunction, &s.CodeFile, &s.StartTime, &s.DurationNS,
		); err != nil {
			return nil, fmt.Errorf("scan span: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

var _ Store = (*Postgres)(nil)
