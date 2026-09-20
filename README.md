# Aquila

**Prove the patch against the running system.**

CI sees tests. Agents see git. Production sees traces. Those are different worlds, and that is why a green PR still ships a slow checkout.

Aquila is the control plane that joins them. It reconstructs how a distributed backend actually behaves from OpenTelemetry, maps that behavior onto source, computes the blast radius of a change, and validates the patch with baseline vs patch experiments. If a required experiment did not run, the change is not validated.

```
$ aquila ask "reduce checkout p95 latency"
```

That is the product: investigate runtime evidence, change the code, run an equivalent environment twice (baseline and patch), report what the traces did.

## Why it exists

A coding agent can rewrite `payment.authorize`, pass unit tests, and still leave the live path worse. An observability backend can show you the path and will not ship a patch. Aquila is the layer in between: **runtime truth in, evidence out.**

It is for containerized Go and Python services on Docker Compose or Kubernetes, instrumented with OpenTelemetry. HTTP/gRPC, Postgres, Redis. Narrow so the model can be real.

## What it does

1. **Observe** — ingest traces, rebuild services, endpoints, and the request paths that actually ran.
2. **Locate** — attach those paths to functions and files.
3. **Impact** — given a git diff, name the services, routes, tests, and runtime paths in the blast radius. Categorical (direct / likely / possible / unobserved), not a fake risk score.
4. **Change** — apply a focused patch.
5. **Experiment** — minimum useful DAG: replay, performance, faults, concurrency, impacted tests. Same workload, two revisions.
6. **Evidence** — pass only if the required runs executed. Skip, timeout, and infra failure are not success.

Git is source of truth. OpenTelemetry is runtime truth. Postgres is Aquila state. The model never is.

## Demo

The in-repo application is `examples/shop/`: gateway, users, checkout, inventory, payment, processor, notification. It is instrumented, it has documented production-shaped defects, and it is the system Aquila is pointed at.

```bash
git clone https://github.com/sumedh-aerram/Aquila.git
cd Aquila
make dev
make ingest-smoke
```

Requires Go 1.25+, Docker, and Compose. That command starts Aquila, Postgres, the OpenTelemetry Collector, Prometheus, Grafana, and the shop.

```bash
curl -sf http://127.0.0.1:8080/healthz
curl -sf http://127.0.0.1:18080/users/user-1
curl -sf 'http://127.0.0.1:8080/v1/spans?limit=20'
make down
```

Shop layout and defects: [examples/shop/README.md](examples/shop/README.md), [examples/shop/DEFECTS.md](examples/shop/DEFECTS.md).

## How it works

```
                    git                         traces (OTLP)
                      │                               │
                      ▼                               ▼
                 source graph                    span store
                      │                               │
                      └──────────►  runtime graph  ◄──┘
                                      │
                         impact  →  patch  →  experiment DAG
                                      │
                         baseline ∥ patch   (isolated, equivalent)
                                      │
                                   evidence
```

Shop services export OTLP/gRPC to the collector. The collector forwards OTLP/HTTP to Aquila. Aquila persists allowlisted span metadata (service, route, parent, duration, status). Request bodies and query strings are dropped. Collector retries upsert on `(trace_id, span_id)` — at-least-once ingest, not exactly-once.

Experiments run as durable tasks on Linux workers with leases and fencing: a stale attempt cannot commit. Workloads are sandboxed. Aquila does not mount the host Docker socket into generated-code containers and does not put host credentials in the experiment environment.

Ingest privacy and execution bounds: [docs/SECURITY.md](docs/SECURITY.md).

## Stack

| | |
| --- | --- |
| Control plane | Go |
| Durable state | PostgreSQL |
| Runtime signal | OpenTelemetry (OTLP) |
| Local demo | Docker Compose |
| Shop | Go microservices, Postgres, Redis |
| Artifacts | content-addressed object storage |
| Workers | gRPC, sandboxed OCI |

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

Published ports bind to loopback.

## License

Source in this repository is for the Aquila project. Licensing will be declared before a public release.
