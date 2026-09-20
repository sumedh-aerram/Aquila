# Aquila

**Runtime CI for distributed backends.**

Your tests passed. Checkout is still slow. The agent that wrote the patch never saw a trace.

Aquila sits on the running system. It records how requests actually move (OpenTelemetry), maps that path back to code, and is being built to keep a patch off `main` until baseline vs patch experiments say the behavior you cared about did not regress.

Git is source. Traces are runtime. Aquila is the join.

## Run it

Go 1.25+, Docker, Compose:

```bash
git clone https://github.com/sumedh-aerram/Aquila.git
cd Aquila
make dev
make ingest-smoke
```

That boots the control plane, an OpenTelemetry Collector, and `examples/shop/` — seven instrumented services with documented production-shaped defects. Smoke traffic, then list what Aquila kept:

```bash
curl -sf 'http://127.0.0.1:8080/v1/spans?limit=20'
```

You should see `gateway`, `checkout`, `payment`, shared `trace_id`s, parent links, routes, statuses, durations. No request bodies. No query strings. That JSON is observed, not generated.

```bash
make down
```

## Why you would use this

Repo-only agents (and most PR bots) see files, tests, and a terminal. That is necessary. It is not enough to change `payment.authorize`.

The question after a checkout patch is not “did CI go green?” It is:

> Did the live path change — and can you measure it?

Datadog will show you the path. It will not patch the code or run an equivalent baseline vs patch. A coding agent will patch the code. It will not replay the path. Aquila is the layer that is supposed to do both, with a hard rule: **if a required experiment did not run, the patch is not validated.**

V1 is Go/Python services on Compose or Kubernetes, instrumented with OpenTelemetry. Narrow on purpose.

## What's here today

| | |
| --- | --- |
| Shop exam app | `examples/shop/` — gateway, users, checkout, inventory, payment, processor, notification. Defects D1–D6 stay; they are the ground truth, not leftovers. |
| OTLP ingest | Collector → `POST /v1/traces` → Postgres `aquila.spans`. Allowlisted metadata only. |
| Local debug read | `GET /v1/spans` on loopback. |
| Control plane | `/healthz`, `/readyz`, Compose, race tests, CI. |

```
shop  --OTLP/gRPC-->  collector  --OTLP/HTTP-->  aquila-server  -->  postgres
```

This is not a telemetry backend. We do not store payloads. We keep the facts later phases need: who called whom, on which route, for how long.

Shop layout and defects: [examples/shop/README.md](examples/shop/README.md), [examples/shop/DEFECTS.md](examples/shop/DEFECTS.md). Ingest privacy: [docs/SECURITY.md](docs/SECURITY.md).

## Where this is going

The loop, in order. Nothing after ingest is claimed as working until it runs on your machine.

```
observe the running system
        → impact of a git diff
        → focused patch
        → experiment DAG
        → baseline ∥ patch
        → evidence (or an explicit fail)
```

| | Status |
| --- | --- |
| Ingest live traces | **now** |
| Service graph and runtime paths | next |
| `aquila observe` | after the graph |
| Impact engine | after observe |
| Baseline/patch experiments | after impact |
| Workers, leases, fencing | after the local loop works |

`aquila ask` is the intended surface, not a shipped CLI. There are no benchmark numbers in this README because none have been measured.

## Local URLs

| | |
| --- | --- |
| Aquila | http://127.0.0.1:8080/healthz |
| Shop gateway | http://127.0.0.1:18080/healthz |
| Grafana | http://127.0.0.1:13000 (`admin` / `aquila`) |
| Prometheus | http://127.0.0.1:9090 |
| OTLP | `127.0.0.1:4317` (gRPC), `:4318` (HTTP) |

Ports bind to loopback. Do not publish this stack.

```bash
make build
make test
make lint
./bin/aquila version
```

## License

Source in this repository is for the Aquila project. Licensing will be declared before a public release.
