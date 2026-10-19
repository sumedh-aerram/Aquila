# ADR-001: PostgreSQL for durable coordination

Date: 2026-10-18

Status: Accepted+Implemented

## Context

Jobs, runs, and normalized spans need durable state across API restarts.

## Decision

Keep the `aquila.spans`, `aquila.runs`, and `aquila.jobs` tables in PostgreSQL (`migrations/000002_spans.up.sql`, `migrations/000003_runs.up.sql`, `migrations/000004_jobs.up.sql`). `cmd/server/main.go` opens one pool for these stores.

## Consequences

Runtime/source graphs are derived in memory and are not separate SQL graph tables. One database simplifies local operation but still needs migrations, backup, and database availability. See `internal/storage/postgres.go`.
