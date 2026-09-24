# Aquila — prove a backend change is safe before you push it

**Git is source truth. OpenTelemetry is runtime truth. Evidence is earned, never claimed.**

[![CI](https://github.com/sumedh-aerram/Aquila/actions/workflows/ci.yml/badge.svg)](https://github.com/sumedh-aerram/Aquila/actions/workflows/ci.yml)

Aquila is a local control plane for changing distributed backends. It understands how your services behave at runtime, joins that behavior to the functions you just edited, and experimentally validates the change against the previous revision — on your machine, before a PR exists.

It is the missing step between “tests passed” and “I shipped.” Code review sees the hunk. Unit tests see the paths you thought to write. Dashboards see production after you already pushed. Aquila sits in the middle: **what actually ran, what this diff touches, and whether the old build and the new build still agree.**

Aquila is not a chat assistant, not an observability backend, and not a code generator. There is no LLM in this loop. `match` is not a pass. A skipped experiment is not evidence.

![Aquila loop: observe, impact, experiment, validated](assets/loop.svg)

## Measured on a real backend

We pointed Aquila at a **private production Go API** — 32 packages, 117 files, 660 functions — that was not written for this project. Two real commits ran side by side. Service, route, and function names below are renamed. Every number is from the run.

![Measured evidence: 60/60 spans bound, refactor validated, five status changes](assets/evidence.svg)

| Gate | Result |
| --- | --- |
| Spans bound to a function and line | **60 / 60** |
| Blast radius of the fix | **12** functions changed · **8** on live routes · **6** never seen running |
| Refactor vs baseline | **14 / 14** requests same status · coverage hit · **`validated true`** |
| Latency, n=20 per route × 14 routes | medians within **0.5 ms** · p95 reported (not withheld) |
| Concurrency | **48 / 112** errors on both sides of the refactor |
| Fix vs baseline | **5** routes **500 → 404** · concurrency **48 vs 8** · `validated false` |
| Handler the fix missed | `/summary` still **500 / 500** — error string did not match the check |
| Dead patch / no credentials / skipped routes | `incomplete` · `auth_rejected` · `missed 7/7` — none earned validated |
| Patch +1500 ms slower | `latency_shift 0.2ms → 1501.8ms` · blocked |
| Worker `kill -9` mid-task | requeued in **17 s** · finished at **fence 2** |

That is the product. Not a bigger feature list. A control plane that will not lie to you.

## Why this exists

A distributed backend has two truths that almost never meet on a laptop:

1. **What the types say** — callers, files, line ranges
2. **What production actually does** — hops, routes, status codes, latency

Aquila keeps them separate, then joins them only with evidence. Incomplete joins stay blank (`unmapped`, `unobserved`, `auth_rejected`). Guessing to look complete is a product bug.

![You edited one function. Types show its caller. Traces show the request path.](assets/impact.svg)

![What actually ran: one hop, counted from traces](assets/hops.svg)

## What you get

### Two-truth graph

`aquila observe` builds a living model of the running system: services, hops, routes, and — when spans carry `code.function.name` / `code.file.path` — a bind from each span to a function and line. `aquila ask` joins a question onto those facts. It does not invent an answer.

```text
$ aquila observe -service api -traces 60
runtime  traces 60  spans 60  services 1
locate   spans 60  bound 60  unmapped 0
  bind  api  WeeklyReportHandler  internal/http/report_handlers.go:121
```

### Blast radius of an uncommitted diff

`aquila impact` reads `git diff HEAD` (or `-f`). It names the functions the hunk actually landed in, typed callers, observed routes that run them, and changed functions the window never saw. You can impact a dirty tree. You cannot validate one.

```text
$ aquila impact -service api
impact  files=4  direct=12  likely=0  runtime=8  unobserved=6
```

### Experiment: old build vs new build

`aquila experiment` hits the same workload on `-base` and `-patch` and records:

| Step | What it compares |
| --- | --- |
| env | health probe on both gateways |
| behavior | status and JSON, request by request |
| latency | n=20 successful samples · median + p95 |
| concurrency | parallel error-rate (symmetric) |
| fault | one-sided 502 probe — does not vote overall |
| tests | `go test` of impacted packages in the current module |

`aquila report` turns the artifact into Markdown. `aquila runs` stores it in Postgres. The server **re-derives** `validated` and rejects a forged flag.

### `validated` is earned

`match` only means the two sides looked the same. `validated true` needs **all** of:

1. every required step succeeded
2. on a **clean, recorded git SHA**
3. latency **n ≥ 20**
4. a request **reached a route that runs the changed code**, and both sides answered it (401/403 on both is `auth_rejected`, not coverage)
5. no route got **> 2× and ≥ 5 ms slower** (`latency_shift`)

### Workers that cannot cheat

`aquila job` records a durable DAG. `aquila worker` leases READY tasks over gRPC with an attempt ID and a fencing token. A stale worker cannot commit. A dead worker is requeued after the 15s lease. Per-service quotas keep one app from starving the others.

```text
latency        leased     fence=1  attempt=c620807f…
# kill -9
latency        ready      fence=1
latency        succeeded  fence=2  attempt=9e3315ee…
  notes  latency_shift GET /health 0.2ms -> 1501.8ms  blocks validated
validated  false
```

### Fail closed

Jobs refuse workloads that carry headers (credentials never enter the job table). Gateway URLs are checked at create and at dial — no link-local, no metadata, no userinfo. Ingest never stores bodies. Forged `validated:true` and `overall:pass` are 400. Path traversal on a patch does not write.

## Try it in 3 minutes

Needs Go 1.25+ and Docker. This starts Aquila plus a bundled seven-service shop.

```bash
git clone https://github.com/sumedh-aerram/Aquila.git && cd Aquila
make dev && make cli && make shop-smoke

./bin/aquila observe -service gateway -traces 20
./bin/aquila impact  -service gateway -traces 20 -f internal/pair/testdata/d1.diff
make experiment-smoke          # baseline + patch shop, writes out/evidence.json
./bin/aquila report out/evidence.json
```

`experiment-smoke` uses n=1, so it prints `validated false` on purpose. Use `-n 20` for a real verdict. `make down` when you are done.

## Use it on your app

Export OTLP to `127.0.0.1:4317` and set `code.function.name` / `code.file.path` on spans. Do not bind the app to `:8080`.

```bash
cd ~/src/your-go-service

aquila observe -service your-api
# edit. do not commit.
aquila impact  -service your-api

# old build :9001, edited build :9002
aquila experiment -service your-api \
  -base http://127.0.0.1:9001 -patch http://127.0.0.1:9002 \
  -health /health -workload workload.json -n 20 -out evidence.json
aquila report evidence.json
```

```json
{"steps": [
  {"method": "GET", "path": "/health"},
  {"method": "GET", "path": "/user/me", "headers": {"Authorization": "Bearer ${MY_TOKEN}"}}
]}
```

`${MY_TOKEN}` expands from the environment. An unset variable is an error. Header values never reach the evidence file. Omit `-workload` and Aquila replays the GET routes it saw in traces.

## Full command surface

| Command | What it does | What we recorded on the real API |
| --- | --- | --- |
| `status` | Control-plane health | `health ok · ready` |
| `attaches` | Every `service.name` in the span store | shared table, not tenants |
| `observe` | Hops, routes, span→function binds | 13 routes · 660 functions · **60/60 bound** |
| `impact` | Blast radius of `git diff HEAD` or `-f` | files 4 · direct 12 · runtime 8 · unobserved 6 |
| `ask "…"` | Observed facts matching a question | keyword join · no LLM · not a patch |
| `plan` | Experiment DAG (does not run it) | env · behavior · latency n=20 · concurrency · fault · tests |
| `experiment` | Execute the DAG on `-base` / `-patch` | refactor `match` + **validated** · fix `differ` · dead patch `incomplete` |
| `report` | Markdown from evidence JSON | schema `aquila.evidence.v1` · git SHA · workload digest |
| `runs` | Stored evidence in Postgres | server re-derives `validated` |
| `run` | ask + plan + candidate + experiment | `-plan-only` investigates without executing |
| `patch` | Shop-only rewrite (D1–D6) | honest `no candidate` on a foreign app |
| `job` / `jobs` | Durable DAG | refuses credential headers · records impacted routes |
| `worker` | Lease and commit over gRPC | 5 tasks committed · `kill -9` recovered at fence 2 |
| `env` / `replay` / `fault` | Shop pair · two-gateway replay · loopback 502 | shop-only compose; foreign apps pass `-base`/`-patch` |

## Latency, in full

Refactor vs baseline. n=20 successful samples per route per side. Laptop loopback. Not a benchmark.

| Route | Baseline median | Baseline p95 | Refactor median | Refactor p95 |
| --- | ---: | ---: | ---: | ---: |
| GET /health | 0.3 ms | 0.4 ms | 0.4 ms | 0.5 ms |
| GET /session | 1.0 ms | 1.6 ms | 1.1 ms | 2.0 ms |
| GET /items | 1.0 ms | 1.1 ms | 1.0 ms | 1.8 ms |
| GET /items/active | 11.2 ms | 15.4 ms | 10.9 ms | 14.0 ms |
| GET /items/summary | 4.0 ms | 4.6 ms | 3.8 ms | 4.9 ms |
| GET /summary | 3.6 ms | 4.9 ms | 3.6 ms | 5.9 ms |
| GET /score | 5.7 ms | 7.9 ms | 5.4 ms | 7.4 ms |
| GET /score/history | 5.7 ms | 6.5 ms | 5.4 ms | 6.0 ms |
| GET /reports/daily | 6.7 ms | 8.3 ms | 6.2 ms | 7.2 ms |
| GET /reports/weekly | 6.7 ms | 8.7 ms | 6.5 ms | 7.9 ms |
| GET /stats/average | 7.8 ms | 9.4 ms | 7.6 ms | 9.0 ms |
| GET /usage | 3.7 ms | 4.6 ms | 3.5 ms | 5.7 ms |
| GET /stats/distribution | 5.7 ms | 6.8 ms | 5.7 ms | 9.0 ms |
| GET /catalog | 1.5 ms | 1.7 ms | 1.5 ms | 1.8 ms |

The fix run sat within 0.2 ms of these medians and differed on status, not speed.

## How it compares

| | Before you push | Uses real request paths | Old vs new, side by side | Will refuse a false pass |
| --- | --- | --- | --- | --- |
| Unit tests | yes | only what you wrote | no | no |
| Code review | yes | no | no | no |
| APM / tracing | after deploy | yes | no | no |
| **Aquila** | **yes** | **yes** | **yes** | **yes** |

<details>
<summary>What is done, and what is not</summary>

**Done and tested in this tree:** observe → impact → plan → experiment → report on a foreign Go module; coverage and latency-shift gates; durable jobs with leases, heartbeats, and fencing; live `kill -9` recovery; per-service quota; content-addressed cache; OCI sandbox; C++20 process supervisor; Prometheus/Grafana for Aquila itself; Terraform module (validated in CI, never applied).

**Not built, on purpose:** LLM agents, treating `match` as ship, patch generation for arbitrary apps, a Python source analyzer, GitHub PR integration, a web dashboard, a live cloud deploy.

</details>

<details>
<summary>Operator notes</summary>

- More than one `service.name` in the window? Pass `-service` so another attach cannot drown yours.
- OTLP from the host is `127.0.0.1:4317`, not `otel-collector:4317`.
- Jobs refuse headers. Authenticated experiments run locally with `aquila experiment`.
- `AQUILA_API_TOKEN`, when set, guards the HTTP API and worker gRPC. Compose leaves it empty and binds `127.0.0.1`.
- `AQUILA_LEASED_TASKS_PER_SERVICE` caps in-flight tasks per service.
- Ingest privacy and residual local risk: [docs/SECURITY.md](docs/SECURITY.md).

| Service | URL |
| --- | --- |
| Aquila API | http://127.0.0.1:8080/healthz |
| Demo shop | http://127.0.0.1:18080/healthz |
| Grafana | http://127.0.0.1:13000 (`admin` / `aquila`, local only) |
| OTLP | `:4317` gRPC · `:4318` HTTP |

</details>

## Develop

```bash
make build && make test && make lint
```

## License

Source in this repository is for the Aquila project. Licensing will be declared before a public release.
