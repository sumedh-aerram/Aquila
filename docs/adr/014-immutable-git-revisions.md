# ADR-014: Immutable Git revisions for experiments

Date: 2026-10-18

Status: Accepted+Implemented

## Context

A mutable working tree or ambiguous commit label cannot reproduce the source supplied to a comparison.

## Decision

Resolve one baseline HEAD, capture patch bytes in a temporary index, retain a synthetic commit under a tree-named ref, and materialize only supported blob modes (`internal/gitrev/snapshot.go:SnapshotWorktreeAt`, `internal/gitrev/snapshot.go:Pin`, `internal/gitrev/materialize.go:Materialize`).

## Consequences

A dirty patch can validate after an exact built snapshot; a dirty baseline cannot. Unsupported symlinks, attributes, or transformations are refused. No snapshot ref garbage collector exists. `TestSnapshotDoesNotTouchIndex` covers the index boundary.
