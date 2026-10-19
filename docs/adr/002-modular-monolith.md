# ADR-002: Modular Go control plane

Date: 2026-10-18

Status: Accepted+Implemented

## Context

Ingest, graph, impact, planning, and scheduling share data and policy.

## Decision

Compose one API server from internal packages (`cmd/server/main.go`, `internal/api/server.go:NewServer`). Keep package boundaries without splitting these into network services.

## Consequences

The CLI and workers remain separate processes. Deployment changes should be motivated by measured bottlenecks; no such measurement is published here.
