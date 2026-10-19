# ADR-010: Decision from structured results

Date: 2026-10-18

Status: Accepted+Implemented

## Context

A comparison verdict and a validation decision answer different questions.

## Decision

Use `internal/validate/decide.go:Decide` for ordered blockers and `internal/evidence/artifact.go:Check` for current-schema consistency.

## Consequences

Uploaded execution remains client-attested; the server checks structure and policy, not that requests actually ran. `TestDecideReportsAllBlockers` and `TestCheckV3RejectsForgedBlockers` test policy.
