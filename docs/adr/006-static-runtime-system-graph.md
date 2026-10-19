# ADR-006: Typed source and observed runtime joins

Date: 2026-10-18

Status: Accepted+Implemented

## Context

Static calls miss traffic and traces miss unexecuted code.

## Decision

Join Go source nodes, explicit span code attributes, and complete parent ancestry; retain provenance (`internal/source/load.go:Load`, `internal/locate/bind.go:Bind`, `internal/graph/graph.go:EntryForSpan`).

## Consequences

The model is Go-only and in memory. Immediate caller routes are labeled inferred. No numeric confidence model exists. `TestAnalyzeCallerDepth` covers this limit.
