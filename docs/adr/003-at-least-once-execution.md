# ADR-003: At-least-once task execution

Date: 2026-10-18

Status: Accepted+Implemented

## Context

A worker may disappear after acting on a target while its result is uncommitted.

## Decision

Requeue expired leases and permit duplicate execution (`internal/jobs/reaper.go:Loop`, `internal/jobs/enqueue.go:requeue`). Accept only the current attempt at commit.

## Consequences

Mutating workloads must tolerate retries or use idempotency keys. This does not promise exactly-once side effects. `TestHeartbeatExtendsLease` and `TestStaleAttemptCannotCommit` cover the basic transition.
