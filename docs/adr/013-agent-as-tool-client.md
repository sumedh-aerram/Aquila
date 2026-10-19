# ADR-013: Deterministic investigation before model actions

Date: 2026-10-18

Status: Accepted, not yet implemented

## Context

Natural-language investigation and candidate output can sound authoritative without executed evidence.

## Decision

Keep `ask` as deterministic fact search and `run` as explicit investigation, plan, shop candidate, and experiment (`internal/investigate/search.go:Search`, `internal/cli/workflow.go:RunWorkflow`). Any future model-based client must consume structured observations and leave validation to policy.

## Consequences

There is no model client, persisted tool-call catalog, or general patch agent in this revision. The current shop rewrite templates live in `internal/rewrite/shop.go`; they do not generalize to arbitrary repositories.
