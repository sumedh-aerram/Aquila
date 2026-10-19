# ADR-007: Bounded OCI helper

Date: 2026-10-18

Status: Accepted; implemented as a standalone helper, not wired into workers or replay

## Context

Arbitrary commands need limits before execution.

## Decision

Provide a separate Docker-based runner with non-root user, dropped capabilities, read-only root, default no network, and CPU/memory/PID/time bounds (`internal/sandbox/oci.go:Args`).

## Consequences

The helper is not the general shop pair or HTTP replay boundary. Current-module impacted tests run on the host (`internal/plan/tests.go:runTests`). Stronger isolation is future work.
