# Aquila

Aquila is a **control plane for changing distributed backends safely**.

It sits beside a running system—not inside the request path—and holds three things most tools never put in one place: **what the code is**, **how the system actually behaves**, and **whether a proposed patch changed that behavior**. The last question is answered only by executing the same production-shaped workload against **baseline and patch** in isolated environments. Confidence is not evidence. A skipped experiment is not a pass.

```
$ aquila ask "reduce checkout p95 latency"
```

Investigate from traces, map the blast radius onto source, apply a focused change, run the experiment DAG, report what the two revisions did.

## Why a control plane

A checkout request does not live in one repository file. It crosses a gateway, an orchestrator, users, inventory, payment, a processor, a notifier, Postgres, and Redis. Latency and errors hide in **how those hops compose at runtime**. Static analysis cannot see a connection opened on every authorize. A trace store cannot tell you whether tomorrow’s diff is safe. A test suite cannot replay the path production actually takes unless you derived the workload from those traces.

Aquila is the layer that **coordinates** that work:

- **Ingest** runtime spans (OpenTelemetry) as durable, allowlisted facts.
- **Derive** services, dependencies, and request paths only from observed parent links.
- **Join** those paths to source and to a git diff (impact that a human can inspect).
- **Plan and run** experiments as jobs: equivalent environments, two git SHAs, one workload.
- **Commit results** under leases and fencing so a stale worker cannot write history.

That is control-plane work: durable state, at-least-once ingestion, incomplete data, fail-closed decisions, untrusted generated code in sandboxes. The application under test stays the source of runtime truth. Aquila does not pretend to be that application.

Git is source of truth. OpenTelemetry is runtime truth. PostgreSQL is Aquila’s coordination state. The language model may propose; it never authors system state.

V1 targets containerized Go and Python services on Docker Compose or Kubernetes, HTTP/gRPC, Postgres, Redis, instrumented with OpenTelemetry. The domain is narrow so the graph can be honest.

## What it does

1. **Observe** — accept OTLP, persist service, route, parent, duration, status. No request bodies. No query strings.
2. **Locate** — parse target source into a traversable graph, then attach observed spans to functions only when instrumentation attributes uniquely match. Unmapped spans stay unmapped.
3. **Impact** — given a diff, name affected services, endpoints, runtime paths, and tests. Direct, likely, possible, unobserved — not a synthetic score.
4. **Change** — a focused patch on that radius.
5. **Experiment** — the smallest DAG that could falsify the change: trace-derived replay, performance, faults, concurrency, impacted tests.
6. **Evidence** — a report grounded in executed jobs. Timeout, skip, and infrastructure failure are distinct from “the patch is fine.”

## The hard parts (this is the product)

**Two truths, one decision.** Source and runtime disagree. A function can exist and never run. A span can exist and lack a parent in the batch you loaded. Aquila records provenance (`observed_parent` today) and **does not invent** edges to look complete.

**At-least-once, not exactly-once.** Collectors retry. Spans upsert on `(trace_id, span_id)`. Experiments retry as new attempts with fencing tokens. Duplicate delivery must not duplicate a charge in the exam app, and must not duplicate an authoritative experiment result.

**Equivalence.** Baseline vs patch only means something if environments, workload, and resource bounds match and both revisions are recorded. “We ran it once in staging” is not that.

**Untrusted execution.** Generated patches and experiment workloads get bounded CPU, memory, PIDs, time, filesystem, and network. No host Docker socket. No host cloud credentials. The control plane schedules; the sandbox runs user code.

**Failure is data.** A worker that loses its lease does not get to land a late result. That invariant is the difference between a script and a control plane.

## Reference system

`examples/shop/` is the system Aquila is pointed at: seven instrumented services (gateway, users, checkout, inventory, payment, processor, notification) and documented production-shaped defects—connection churn, N+1 queries, unindexed event scans, retry amplification, synchronous notify on the critical path, a process-wide lock.

```bash
git clone https://github.com/sumedh-aerram/Aquila.git
cd Aquila
make dev
make graph-smoke
```

Requires Go 1.25+, Docker, and Compose. Starts the control plane, shop Postgres, Aquila Postgres, Redis, the OpenTelemetry Collector, Prometheus, Grafana, and the shop.

```bash
curl -sf http://127.0.0.1:8080/healthz
curl -sf http://127.0.0.1:18080/users/user-1
curl -sf 'http://127.0.0.1:8080/v1/spans?limit=20'
curl -sf 'http://127.0.0.1:8080/v1/graph?traces=20'
curl -sf 'http://127.0.0.1:8080/v1/source'
curl -sf 'http://127.0.0.1:8080/v1/locate?traces=20'
make down
```

`GET /v1/spans` is observed metadata from live shop traffic. `GET /v1/graph` is topology derived from those spans: an edge exists only when parent and child are in the window and the services differ. `GET /v1/source` is a typed parse of `examples/shop`: packages, files, functions, in-module imports, and typed calls. HTTP hops are not invented as call edges. `GET /v1/locate` binds spans to functions only when `code.function.name` and `code.file.path` uniquely match a source node. Neighbors: `GET /v1/source/neighbors?id=...`. The Compose image snapshots that graph at build time; rebuild after shop source changes. Shop layout and defects: [examples/shop/README.md](examples/shop/README.md), [examples/shop/DEFECTS.md](examples/shop/DEFECTS.md).

## How it is put together

```
        git                         OTLP traces
         │                               │
         ▼                               ▼
   source graph                      span store
         │                               │
         └────────►  runtime graph  ◄────┘
                         │
              impact → patch → experiment DAG
                         │
                      job queue
                         │
              ┌──────────┴──────────┐
              ▼                     ▼
         worker (baseline)     worker (patch)
              │                     │
            sandbox               sandbox
              └──────────┬──────────┘
                         ▼
                      evidence
```

Shop services export OTLP/gRPC to the collector. The collector forwards OTLP/HTTP to Aquila so checkout never waits on the control plane. Runtime topology is computed from a window of complete traces, in process. The source graph is loaded from a live module directory (`AQUILA_SOURCE_DIR`) or from a JSON snapshot (`AQUILA_SOURCE_SNAPSHOT`) so the API image does not ship a Go toolchain.

Experiments are **jobs**: identity, baseline SHA, patch SHA, workload digest, attempt, lease, result. Workers heartbeat; expired work is retaken; a stale attempt cannot commit. Kubernetes may host those workers. It does not replace the scheduler, the lease, or content-addressed artifacts.

Ingest privacy and sandbox bounds: [docs/SECURITY.md](docs/SECURITY.md).

## Stack

| | |
| --- | --- |
| Control plane | Go |
| Coordination state | PostgreSQL |
| Runtime signal | OpenTelemetry (OTLP) |
| Reference app | `examples/shop/` |
| Local environment | Docker Compose |
| Experiment execution | durable DAG, queue, leased workers, sandboxed OCI |
| Artifacts | SHA-256 content-addressed storage |

```bash
make build
make test
make lint
./bin/aquila version
```

| | |
| --- | --- |
| Aquila | http://127.0.0.1:8080/healthz |
| Shop gateway | http://127.0.0.1:18080/healthz |
| Grafana | http://127.0.0.1:13000 (`admin` / `aquila`) |
| Prometheus | http://127.0.0.1:9090 |
| OTLP | `:4317` gRPC, `:4318` HTTP |

Ports bind to loopback.

## License

Source in this repository is for the Aquila project. Licensing will be declared before a public release.
