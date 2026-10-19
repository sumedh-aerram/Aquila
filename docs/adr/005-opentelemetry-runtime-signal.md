# ADR-005: OpenTelemetry as runtime input

Date: 2026-10-18

Status: Accepted+Implemented

## Context

Service paths must be observed rather than inferred from source alone.

## Decision

Accept OTLP through a Collector and retain only normalized span metadata (`deploy/compose/otel/config.yaml`, `internal/ingest/otlp.go:NormalizeReport`).

## Consequences

Missing instrumentation and incomplete windows reduce the graph. Metrics are exported to Prometheus but do not form SLO graph joins. `TestNormalizeKeepsRouteAndDropsBody` covers retained fields.
