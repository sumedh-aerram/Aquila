# ADR-012: C++20 process supervisor

Date: 2026-10-18

Status: Accepted; implemented as a standalone helper, not wired into workers or replay

## Context

Wall deadlines and process-group cleanup need a separate native subprocess boundary.

## Decision

Keep orchestration in Go and provide `aquila-exec` for fork/exec, optional rlimits, timeout and result JSON (`executor/src/exec.cpp`, `internal/nexec/nexec.go:Run`).

## Consequences

It is not a container runtime, has no cgroups, and is not automatically used by replay or impacted tests. Linux network namespace requests fail on macOS (`executor/tests/exec_test.cpp`).
