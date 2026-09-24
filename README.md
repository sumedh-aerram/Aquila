# Aquila

**Know what your backend change will break before you push it.**

[![CI](https://github.com/sumedh-aerram/Aquila/actions/workflows/ci.yml/badge.svg)](https://github.com/sumedh-aerram/Aquila/actions/workflows/ci.yml)

You edit a handler. Tests pass. Review looks fine. Then production shows a route you forgot about returning 500s.

Aquila closes that gap on your laptop. It:

- reads your **uncommitted `git diff`** and the **OpenTelemetry traces** your services already emit
- names every live route that runs the code you changed
- replays real requests against the **old build and the new build side by side**
- says `validated` only when those requests reached your change and nothing got worse

No production access, no PR, no LLM.

## What it caught on a real codebase

We pointed Aquila at a private production Go API (32 packages, 660 functions) that wasn't written for Aquila, and ran two real commits through it. Service, route, and function names below are renamed; every number is from the run.

**1. It mapped the blast radius from traces, not guesses.**

```text
$ aquila observe -service api
locate  provenance=code_attrs
  spans 60  bound 60  unmapped 0
  bind  api  WeeklyReportHandler   internal/http/report_handlers.go:121
  bind  api  DistributionHandler   internal/http/stats_handlers.go:88
  bind  api  UsageHandler          internal/http/report_handlers.go:232
  ...

$ aquila impact -service api
impact  files=4  direct=12  likely=0  runtime=8  unobserved=6
```

Every runtime span was tied to a function and line. Of 12 changed functions, 8 run on live routes and 6 were never seen running. Aquila lists those 6 separately instead of pretending they're covered.

**2. It proved a refactor changed nothing.**

```text
$ aquila experiment -service api -health /health -workload workload.json \
    -base http://127.0.0.1:29898 -patch http://127.0.0.1:29899
experiment  overall=match  steps=6  skipped=0
  behavior       match
    GET    /reports/daily?…       500/500  match
    GET    /score/history?…       200/200  match
    ...                                          (14 requests, all match)
  latency        samples
    GET    /reports/daily?…       base n=20  median=6.7ms  p95=8.3ms  patch n=20  median=6.2ms  p95=7.2ms
    GET    /reports/weekly?…      base n=20  median=6.7ms  p95=8.7ms  patch n=20  median=6.5ms  p95=7.9ms
    ...
  concurrency    match
    notes      baseline_errors=48/112 patch_errors=48/112
coverage   impacted=3  exercised=3  missed=0
validated  true
```

**3. It caught a behavior change, and found a bug in the fix.**

```text
$ aquila experiment -service api ... -patch http://127.0.0.1:29900
experiment  overall=differ  steps=6  skipped=0
  behavior       differ
    GET  /reports/daily?…        500/404  differ  status
    GET  /reports/weekly?…       500/404  differ  status
    GET  /score?…                500/404  differ  status
    GET  /stats/average?…        500/404  differ  status
    GET  /stats/distribution?…   500/404  differ  status
    GET  /summary?…              500/500  match
  concurrency    differ
    notes      baseline_errors=48/112 patch_errors=8/112,patch errors less under concurrency
validated  false
```

The fix meant to turn "no data" 500s into 404s. Five routes changed as intended. `/summary` is on the blast radius, but it still returned 500: its error string didn't match the fix's check. The side-by-side replay made the miss obvious. Aquila reports `differ` rather than `validated` because only you can decide whether a behavior change is the right one.

**4. It refused to call bad runs safe.** Every one of these came back `validated false`, with the reason printed:

| What went wrong | What Aquila printed |
| --- | --- |
| The patch server was down | `incomplete` · `coverage exercised=0` |
| The workload never touched the changed routes | `missed 7/7` · validated cannot be earned |
| Requests had no credentials, so every route returned 401 | `auth_rejected: handler did not run` |
| The patch was 1.5s slower | `latency_shift GET /health 0.2ms -> 1501.8ms` |
| A worker was killed with `kill -9` mid-task | requeued after the 15s lease, finished by another worker at fence 2 |

## Try it in 3 minutes

Needs Go 1.25+ and Docker. This runs Aquila plus a bundled seven-service demo shop, all on localhost.

```bash
git clone https://github.com/sumedh-aerram/Aquila.git && cd Aquila
make dev && make cli && make shop-smoke      # control plane, demo shop, some traffic

./bin/aquila observe -service gateway -traces 20
./bin/aquila impact  -service gateway -traces 20 -f internal/pair/testdata/d1.diff
make experiment-smoke                        # boots baseline + patch shop, replays, writes out/evidence.json
./bin/aquila report out/evidence.json
```

`experiment-smoke` uses one sample per route, so it prints `validated false` on purpose. Use `-n 20` or more for a real verdict. `make down` when you are done.

## Use it on your app

Your service must export OTLP to `127.0.0.1:4317` and set `code.function.name` / `code.file.path` on spans (that is how spans bind to functions).

```bash
cd ~/src/your-go-service

aquila observe -service your-api          # what runs, bound to which functions
# edit code. don't commit.
aquila impact  -service your-api          # blast radius of `git diff HEAD`

# run the old build on :9001 and your edited build on :9002, then:
aquila experiment -service your-api \
  -base http://127.0.0.1:9001 -patch http://127.0.0.1:9002 \
  -health /health -workload workload.json -out evidence.json
aquila report evidence.json
```

`workload.json` is the requests to replay. Headers can pull from env vars so tokens never sit in files:

```json
{"steps": [
  {"method": "GET", "path": "/health"},
  {"method": "GET", "path": "/user/me", "headers": {"Authorization": "Bearer ${MY_TOKEN}"}}
]}
```

Leave out `-workload` and Aquila replays the GET routes it saw in traces.

## When is it `validated`?

`match` only means the two builds answered the same. `validated true` needs **all** of:

1. every required step succeeded: health probe, behavior, latency, plus concurrency and impacted `go test` when planned
2. on a **clean, recorded git SHA** (a dirty tree can be analyzed, never validated)
3. latency with **n ≥ 20** samples per route
4. at least one request **reached a route that runs your changed code**, and both builds answered it (401/403 on both sides doesn't count)
5. no route got **more than 2× and ≥ 5ms slower**

The server re-checks these rules on every stored run and rejects evidence that claims more than it earned.

## How it's different

| | Tells you before push | Uses real runtime paths | Runs old vs new side by side |
| --- | --- | --- | --- |
| Unit tests | yes | no, only paths you thought to test | no |
| Code review | yes | no | no |
| APM / tracing dashboards | no, after deploy | yes | no |
| **Aquila** | **yes** | **yes** | **yes** |

Aquila isn't a production monitoring backend. It reads the traces your services already send to localhost.

![Aquila impact: changed function, its caller, and the runtime path that reaches it](assets/impact.svg)

## Commands

| Daily | |
| --- | --- |
| `observe` | Routes that ran, which function each one hit, your dirty files |
| `impact` | Blast radius of `git diff HEAD` (or `-f diff`) |
| `experiment` | Replay on `-base` and `-patch`, compare behavior, latency, errors, coverage |
| `report` | Markdown from an evidence file |
| `ask "…"` | Observed facts matching a question (no LLM, not a patch) |

| Team / CI | |
| --- | --- |
| `runs` | Stored evidence history in Postgres |
| `job` / `jobs` / `worker` | Durable experiment DAG executed by gRPC workers with leases and fencing |
| `plan` / `run` | Print the experiment plan, or investigate + plan + experiment in one go |
| `status` / `attaches` | Control-plane health, which services are sending traces |

<details>
<summary>What is done, and what is not</summary>

**Done and tested on this tree:**

- observe → impact → plan → experiment → report on a foreign Go module
- route coverage and latency-shift gates on `validated`
- durable jobs across multiple workers, with leases, heartbeats, and fencing (a stale worker can't commit)
- live `kill -9` worker recovery
- per-service lease quota
- content-addressed cache
- OCI sandbox and a C++20 process supervisor
- Prometheus/Grafana for Aquila itself
- a Terraform module (validated in CI, never applied)

**Not built:**

- LLM agents
- patch generation for arbitrary apps (`patch` only knows the demo shop's defects)
- Python function mapping (Python services show routes, not functions)
- GitHub PR integration
- a web dashboard
- a live cloud deploy

</details>

<details>
<summary>Operator notes</summary>

- If traces contain more than one `service.name`, pass `-service` so another app cannot drown yours.
- Don't bind the app under test to `:8080` (Aquila's API).
- OTLP goes to `127.0.0.1:4317` from your host, not `otel-collector:4317` (that name only resolves inside Docker).
- Jobs refuse workloads with headers, so credentials never land in the job table. Run authenticated experiments with `aquila experiment` locally.
- `AQUILA_API_TOKEN`, when set, guards the HTTP API and the worker gRPC port. Local Compose leaves it empty and binds everything to `127.0.0.1`.
- `AQUILA_LEASED_TASKS_PER_SERVICE` caps how many tasks one service holds at once.
- Ingest never stores request or response bodies. See [docs/SECURITY.md](docs/SECURITY.md).

| Service | URL |
| --- | --- |
| Aquila API | http://127.0.0.1:8080/healthz |
| Demo shop | http://127.0.0.1:18080/healthz |
| Grafana | http://127.0.0.1:13000 (`admin` / `aquila`, local only) |
| OTLP | `:4317` gRPC, `:4318` HTTP |

</details>

## Develop

```bash
make build && make test && make lint
```

## License

Source in this repository is for the Aquila project. Licensing will be declared before a public release.
