# ADR-004: Lease attempt fencing

Date: 2026-10-18

Status: Accepted; implemented. The attempt ID is the commit guard; the numeric fence is recorded but not checked on commit

## Context

An expired worker can return after replacement work has completed.

## Decision

Assign a new attempt ID and increment a task fence on each lease; commit checks the current attempt (`internal/jobs/postgres.go:Lease`, `internal/jobs/enqueue.go:applyCommit`).

## Consequences

The numeric fence is recorded but not sent as a separate commit guard. The current task payload retains no full attempt history. `TestKilledWorkerTaskRecoversAndStaleCommitIsRejected` proves the replacement sequence with an in-memory store and real worker process.
