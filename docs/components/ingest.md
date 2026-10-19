# Trace ingestion

**Purpose.** The Collector forwards OTLP traces to `POST /v1/traces`. `internal/api/ingest.go:handleOTLPTraces` authorizes and decodes a bounded request; `internal/ingest/otlp.go:NormalizeReport` retains span IDs, parent links, service, route, method, status, duration, and explicit code attributes. `internal/ingest/postgres.go:UpsertSpans` writes normalized spans to `aquila.spans` (`migrations/000002_spans.up.sql`).

**Interfaces.** `internal/ingest/span.go:Store` offers upsert, list, recent trace-window, and attach listing. `internal/ingest/window.go:SelectWindow` removes health-only traces and filters by `service.name`. `internal/api/server.go:NewServer` exposes span, attach, graph, and locate reads. The demo emits spans from `examples/shop/internal/httpx/otel.go` through `deploy/compose/otel/config.yaml`.

**Evidence.** `TestNormalizeKeepsRouteAndDropsBody`, `TestOTLPTracesPartialSuccessOnInvalidIDs`, and `TestSelectWindowServiceIgnoresNewerOtherAttach` cover privacy, rejection accounting, and windows. PostgreSQL tests in `internal/ingest/postgres_test.go` need a migrated test database.

**Gaps.** Traces are sampled by a bounded recent window; the database is a metadata store, not a full observability backend. No metric-to-SLO graph join exists. Invalid spans can be partially rejected. [System graph](../SYSTEM_GRAPH.md) explains the downstream interpretation.
