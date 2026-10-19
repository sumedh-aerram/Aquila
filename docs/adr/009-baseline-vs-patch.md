# ADR-009: Equivalent shop pair from Git trees

Date: 2026-10-18

Status: Accepted+Implemented

## Context

A response from one build cannot establish a before/after difference.

## Decision

Capture baseline and patch module trees, materialize exact blobs, and start separate shop Compose projects with the same embedded definition (`internal/gitrev/snapshot.go:SnapshotDiffAt`, `internal/pair/prepare.go:Prepare`).

## Consequences

Automatic pair construction is shop-specific. External URLs remain declared and cannot earn built validation. Pair image tags and host load are not pinned. `TestMaterializeExact` covers bytes.
