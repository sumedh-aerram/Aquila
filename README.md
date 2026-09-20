# Aquila

**Production-aware agentic software engineer.**

Aquila understands how a distributed application behaves at runtime, determines the blast radius of a proposed change, applies the patch, and validates it against production-shaped experiments before presenting evidence.

Repository-only coding agents see source, tests, and terminal output. That is necessary. It is not sufficient. They cannot answer the question that actually matters after a change to checkout, payment, or inventory:

> Did this patch change runtime behavior in a way we can measure — and is that change safe?

Aquila is built to answer that question with artifacts, not vibes.

## What Aquila does

Given an engineering objective such as `reduce checkout p95 latency`, Aquila:

1. Reconstructs the **running system** from OpenTelemetry traces, service topology, and source.
2. Maps latency, errors, and dependencies back to **code**.
3. Computes an interpretable **impact radius** — services, endpoints, runtime paths, tests, SLOs.
4. Applies a **focused patch**.
5. Builds an **experiment DAG**: baseline vs patch, under normal load, concurrency, and faults.
6. Executes those experiments in isolated environments.
7. Emits a **validation report** grounded in executed evidence.

If a required experiment failed, timed out, or never ran, Aquila does not mark the patch validated.

## Why this exists

Current coding agents primarily understand:

- code
- repository structure
- documentation
- tests
- terminal output

Aquila additionally understands:

- runtime traces
- service topology
- request paths
- latency distributions
- error behavior
- database interactions
- dependency relationships
- deployment topology
- resource behavior
- historical experiments

The product loop is therefore not *prompt → patch → tests → “looks good”*. It is:

```
USER REQUEST
     │
     ▼
repository + running system
     │
     ▼
Living Systems Graph
     │
     ▼
impact analysis → patch plan → code change
     │
     ▼
experiment plan
     │
     ▼
distributed execution (baseline ∥ patch)
     │
     ▼
evidence engine → PR report
```

## The four systems

### Living Systems Graph

A continuously updated model connecting source to runtime:

repository → services → endpoints → functions → spans → request paths → dependencies → databases → tests → metrics → SLOs

Edges carry provenance and confidence. Incomplete mappings stay incomplete. Aquila does not invent a call graph to look finished.

### Impact Engine

Given a Git diff, Aquila expands outward through static structure and observed runtime paths. Output is categorical and inspectable: **direct**, **likely**, **possible**, **unobserved**. Not a synthetic 94.238% risk score.

### Experiment Engine

Aquila generates the minimum useful experiment set that could actually falsify the change: behavioral replay, performance comparison, fault injection, concurrency, and impacted tests. Workloads are derived from sanitized trace templates and explicit fixtures — not raw production bodies.

### Distributed Execution Fabric

Experiments run as persistent DAGs on Linux workers with leases, fencing tokens, content-addressed artifacts, and sandboxed execution. The control plane owns scheduling semantics. Kubernetes is a deployment target, not a substitute for the execution model.

## Target domain

V1 is intentionally narrow so the model can be real:

| Supported now (design) | Explicitly later |
| --- | --- |
| Docker Compose, Kubernetes | Serverless |
| Go and Python services | Java, Node/TypeScript |
| HTTP / gRPC APIs | Kafka and broader queues |
| PostgreSQL, Redis | Complex multi-cluster Kubernetes |
| OpenTelemetry traces, Prometheus metrics | Full observability backend replacement |

Aquila is not an IDE, a ChatGPT clone, a GitHub Actions replacement, a Kubernetes replacement, Datadog, Sentry, a container runtime, or a generic multi-agent framework.

Center of gravity: **understand → modify → experimentally validate running software.**

## Architecture

```
                    AQUILA CONTROL PLANE
                      API / Agent
                          │
                    Experiment Planner
                          │
                       DAG Engine
                          │
                       Scheduler
                          │
                     Lease Manager
                          │
                      PostgreSQL
                          │
           ┌──────────────┼──────────────┐
           ▼              ▼              ▼
        worker-01      worker-02      worker-N
           │              │              │
        sandbox        sandbox        sandbox
```

Source of truth is split on purpose:

| System | Authority |
| --- | --- |
| Git | source code |
| OpenTelemetry | observed runtime |
| PostgreSQL | Aquila coordination and derived graph |
| Object storage | immutable artifacts |
| LLM | reasoning assistant — never system state |

## Reliability guarantees

Aquila treats experiment execution as distributed systems work, not a shell script with extra steps.

- At-least-once task execution with fenced result commitment. Not exactly-once.
- A task has at most one authoritative attempt. Stale workers cannot commit.
- Dependencies must succeed before a dependent task becomes `READY`.
- Successful CAS objects are immutable; digest must match content.
- Controller restart cannot destroy durable execution state.
- Infrastructure failures and user-code failures stay distinguishable.
- Cancellation is idempotent.
- A patch cannot be reported validated if required experiments failed or never executed.

The full set lives in [docs/INVARIANTS.md](docs/INVARIANTS.md).

## Quick start

Requirements: Go 1.25+, Docker, and Docker Compose.

```bash
git clone https://github.com/sumedhaerram/aquila.git
cd aquila

make build
make test
make lint

make dev
```

`make dev` starts PostgreSQL, the OpenTelemetry Collector, Prometheus, Grafana, the Aquila API, and the instrumented `examples/shop/` microservices.

Local Grafana login is `admin` / `aquila` (anonymous viewer is also enabled).

| Service | URL |
| --- | --- |
| Aquila API | http://localhost:8080/healthz |
| Shop gateway | http://localhost:18080/healthz |
| PostgreSQL (Aquila) | localhost:15432 |
| Grafana | http://localhost:13000 |
| Prometheus | http://localhost:9090 |
| OTLP gRPC / HTTP | localhost:4317 / 4318 |

```bash
curl -sf http://localhost:8080/healthz
curl -sf http://localhost:8080/readyz
curl -sf http://localhost:18080/users/user-1
make shop-smoke
./bin/aquila version
```

Stop the stack with `make down`.

## Status

Control-plane health API, local Compose, and an instrumented shop demo (`examples/shop/`) are in place. Shop traces export through the collector into Aquila (`POST /v1/traces`, inspect with `GET /v1/spans`). Write ingest is token-gated in Compose. Aquila is not a telemetry backend; it keeps normalized span metadata only.

## Flagship workflow

This is the product surface Aquila is being built to deliver:

```text
$ aquila ask "reduce checkout p95 latency"

Investigating CheckoutService

Current latency     p50 91ms    p95 472ms    p99 1.31s
Critical path       PaymentService 292ms of p95

Observed            payment.authorize on 87.2% of checkout traces
Location            services/payment/client.go:118
Finding             HTTP client constructed on the request path

AQUILA VALIDATION
                    baseline     patch
p95                 472ms        258ms
error rate          .31%         .30%
mismatches          0 / 2,814
impacted tests      148 / 148

Experiment result   PASS
Create pull request? [Y/n]
```

Figures in that sketch are illustrative of the UX, not measured results.

Shop layout and intentional defects: [examples/shop/README.md](examples/shop/README.md), [examples/shop/DEFECTS.md](examples/shop/DEFECTS.md). Runtime privacy and sandbox defaults: [docs/SECURITY.md](docs/SECURITY.md).

## License

Source in this repository is provided for the Aquila project. Licensing will be declared explicitly before a public release.
