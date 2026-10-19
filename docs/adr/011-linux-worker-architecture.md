# ADR-011: Worker RPC and queue

Date: 2026-10-18

Status: Accepted+Implemented

## Context

Jobs need durable state and workers need a way to lease tasks after failures.

## Decision

Use PostgreSQL job payloads and a gRPC Lease/Heartbeat/Commit service (`internal/jobs/postgres.go`, `internal/worker/rpc.go:Register`).

## Consequences

The codec is JSON, not generated protobuf. The native supervisor has Linux-specific network isolation, but ordinary HTTP workers are Go processes and do not materialize revisions.
