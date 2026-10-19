# ADR-015: Entry-route attribution and immediate callers

Date: 2026-10-18

Status: Accepted+Implemented

## Context

A nested service span should be replayed through the request entry route, while direct callee spans can be absent.

## Decision

Attribute a bound span only through complete same-trace ancestry to the root-most routed server; permit one static caller hop and label it `trace_entry_caller` (`internal/graph/graph.go:EntryForSpan`, `internal/impact/analyze.go:boundFindings`).

## Consequences

Caller attribution is inference, accepted and disclosed by validation policy. It does not prove callee execution. Depth-two callers and broken ancestry produce no route. `TestAnalyzeCallerDepth` and `TestAnalyzeEntryAncestry` cover limits.
