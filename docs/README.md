# Aquila documentation

Aquila joins OpenTelemetry traces to Go source, maps a diff to runtime entry routes, and compares baseline and patch behavior. Given a code change to a containerized service, it answers: which entry routes does this touch at runtime, and did behavior or latency differ when the same replayed workload ran against the baseline and the patched build? A shared policy computes `validated` from recorded results and blockers; a command exit of 0 is not a verdict. This is a bounded observation and experiment tool, not proof that a patch is safe in production.

The built-in demo is a separate seven-service shop (`examples/shop/`) with six intentional defects, D1–D6. External applications supply their own gateways and cannot earn built validation. The control plane (API, PostgreSQL, Collector) runs locally under Docker Compose. Start with [architecture](ARCHITECTURE.md) and [experiments](EXPERIMENTS.md), then run the [local demo](runbooks/local-demo.md) if Docker is available.

## Reading order

1. [Local demo](runbooks/local-demo.md): prerequisites, traffic, D1 comparison, cleanup.
2. [Architecture](ARCHITECTURE.md): processes, storage, and request sequences.
3. [System graph](SYSTEM_GRAPH.md): facts, joins, and caller inference.
4. [Experiments](EXPERIMENTS.md): comparisons and decision thresholds.
5. [Execution](EXECUTION_MODEL.md), [invariants](INVARIANTS.md), and [failures](FAILURE_MODEL.md).
6. [Security](SECURITY.md), [benchmarks](BENCHMARKS.md), [CI](runbooks/ci.md), and [contributing](../CONTRIBUTING.md).

## Repository map

| Directory | Responsibility and entry points |
| --- | --- |
| `cmd/` | CLI (`cmd/aquila/main.go`), API (`cmd/server/main.go`), offline source indexer (`cmd/sourceindex/main.go`) |
| `internal/ingest/` | OTLP normalization, sanitization, span store, trace windows |
| `internal/graph/`, `internal/source/`, `internal/locate/` | Runtime topology, typed Go graph, explicit span-to-function joins |
| `internal/diff/`, `internal/impact/` | Diff parsing, change classification, route attribution |
| `internal/gitrev/`, `internal/pair/` | Immutable tree capture, exact materialization, shop environments |
| `internal/replay/`, `internal/fault/`, `internal/plan/` | HTTP workloads, proxy injection, experiment steps |
| `internal/validate/`, `internal/evidence/`, `internal/runs/` | Policy, digest-covered artifacts, stored history |
| `internal/jobs/`, `internal/worker/` | Durable job payloads, leasing, gRPC execution |
| `internal/cas/`, `internal/action/` | Local SHA-256 objects and retry result cache |
| `internal/sandbox/`, `internal/nexec/`, `executor/` | Separate OCI runner and native process supervisor |
| `internal/api/`, `internal/cli/` | HTTP surface and command implementations |
| `internal/config/`, `internal/storage/`, `internal/observability/`, `internal/netguard/`, `internal/version/` | Configuration, database pool, platform metrics/logs, target filtering, build version |
| `internal/investigate/`, `internal/rewrite/` | Deterministic fact search and shop-specific rewrite candidates |
| `migrations/` | Aquila PostgreSQL schema; apply in numeric order |
| `deploy/compose/` | Local control plane, Collector, dashboards, and shop deployment |
| `deploy/terraform/` | Cloud Run/Cloud SQL configuration; no deployed cloud system is established here |
| `examples/shop/` | Separate Go module with seven services, migrations, fixtures, and intentional defects |
| `.github/` | Workflow, test-stream guard, and positive/negative evidence assertions |
| `assets/` | Historical README illustrations |

Tests live beside implementation in `*_test.go`; native tests are in `executor/tests/exec_test.cpp`. There is no separate root test directory. Interfaces and known gaps are described in the [component pages](components/cli.md).

## Filing conventions

| Change | Documentation home |
| --- | --- |
| Cross-cutting design or data flow | [Architecture](ARCHITECTURE.md) |
| Subsystem interfaces and tests | [Component pages](components/ingest.md) |
| Validation semantics or blocker | [Experiments](EXPERIMENTS.md), [invariants](INVARIANTS.md), [failure model](FAILURE_MODEL.md) |
| Reproducible operator procedure | [Runbooks](runbooks/local-demo.md) |
| Security boundary or residual risk | [Security](SECURITY.md) |
| Design decision and tradeoffs | [Numbered ADRs](adr/001-postgresql-durable-state.md); retain old numbers |
| Measured performance result | [Benchmarks](BENCHMARKS.md), with raw output and environment |

Paths in code references are repository-relative, including in nested pages. `path:symbol` names the implementation; a test name identifies a regression test, not a claim that it ran in any particular environment. Planned behavior is labeled explicitly.

## Glossary

| Term | Meaning |
| --- | --- |
| Baseline | Committed module tree used as the reference build |
| Patch | Captured module tree after the selected diff or working-tree changes |
| Impacted route | Method and entry-route template attributed to a changed function or immediate caller |
| Observed coverage | Answered replay of a route attributed directly through runtime/source bindings |
| Inferred coverage | Answered replay of a route attributed through an immediate static caller; does not prove callee execution |
| Blocker | Stable policy code and optional subject explaining an unmet requirement |
| Evidence | Structured workload metadata, revisions, impact, plan, results, coverage, and digests |
| Snapshot tree | Immutable Git module tree retained by a snapshot commit/ref |
| Attempt | Current random identity assigned when a task is leased; historical attempts are not separately stored |
| D1–D6 | The shop's intentional defects (D1 per-request HTTP client in payment, D2 N+1 queries, D3 missing index, D4 unbounded checkout retries, D5 synchronous notification, D6 global inventory lock); see [`examples/shop/DEFECTS.md`](../examples/shop/DEFECTS.md) |
| Gateway | The HTTP entry point of a target application (shop: `127.0.0.1:18080`); replay and pair environments drive workloads through it |
| OTLP | OpenTelemetry Protocol; the Collector forwards spans to the Aquila API for ingestion (`internal/ingest/`) |
| DAG | Task graph of an experiment or job; a dependent task becomes ready only after its dependencies settle (`internal/jobs/`) |
| Lease | Time-limited claim a worker holds on a task; an expired lease is reaped and the task requeued |
| CAS | Content-addressed store: objects keyed by SHA-256 digest on the local filesystem (`internal/cas/`); the worker action cache maps action digests to CAS objects |
| Fencing token | Incrementing task `Fence`; current commit authorization checks `AttemptID`, not a numeric token supplied by the worker |

The exact models are in `internal/gitrev/snapshot.go:Revision`, `internal/impact/routes.go:RouteAttribution`, `internal/evidence/artifact.go:Artifact`, and `internal/jobs/store.go:Task`.

## Standing caveats

These apply throughout the documentation and are stated here once.

- **Client-attested evidence.** The server verifies the evidence digest and policy over the supplied results; it does not attest that the client actually executed the steps or built the labeled trees.
- **Inferred coverage does not prove callee execution.** A route attributed through an immediate static caller is replayed and reported as inferred, not observed.
- **Test names identify regression tests, not a run.** Some tests skip by design; see [invariants](INVARIANTS.md).
