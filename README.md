# Aquila

Aquila is a **control plane for changing distributed backends safely**.

It sits beside a running system—not inside the request path—and holds three things most tools never put in one place: **what the code is**, **how the system actually behaves**, and **whether a proposed patch changed that behavior**. The last question is answered only by executing the same production-shaped workload against **baseline and patch** in isolated environments. Confidence is not evidence. A skipped experiment is not a pass.

```
$ git diff | aquila impact
$ git diff | aquila experiment -fixture -n 1 -out evidence.json
$ aquila report evidence.json
```

Datadog will show you the hop. Copilot will edit the file. Neither will join a git diff to the path that actually ran, then hit **the same routes** on two revisions and tell you match, differ, or incomplete — without calling that a pass.

Investigate from traces, map the blast radius onto source, print a focused candidate, run the experiment, report what the two revisions did. `aquila ask` cites observed facts only. There is no LLM in this loop.

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

V1 targets containerized Go HTTP services on Docker Compose, instrumented with OpenTelemetry. Python or other runtimes can export OTLP (hops still appear); typed impact and locate need a Go module in `-dir`. The domain is narrow so the graph can be honest.

## What it does

1. **Observe** — accept OTLP, persist service, route, parent, duration, status. No request bodies. No query strings.
2. **Locate** — parse target source into a traversable graph, then attach observed spans to functions only when instrumentation attributes uniquely match. Unmapped spans stay unmapped.
3. **Impact** — given a diff, name directly changed functions, their typed callers, and observed runtime paths when locate hits. Unobserved stays unobserved. No synthetic score.
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
make cli
make shop-smoke
./bin/aquila status
./bin/aquila observe -traces 20
./bin/aquila ask checkout
./bin/aquila patch -f internal/pair/testdata/d1.diff
git diff -- examples/shop/internal/payment/handler.go | ./bin/aquila impact -traces 20
```

Leave the stack up while you work. `make cli` rebuilds `./bin/aquila` in seconds after CLI edits. `make up` restarts Compose without rebuilding images. `make dev` after control-plane Go changes (new API image + migrations). `make down` when you are done.

`git diff | ./bin/aquila experiment -fixture -n 1 -out out/evidence.json` starts an isolated shop pair, replays, and tears it down. That is slower; skip it while iterating on observe/ask/impact. `make job-smoke` enqueues a DAG against the live shop (`-base` and `-patch` are the same gateway) and leases two tasks. Same revision is not a baseline-vs-patch experiment; it only proves lease/execute/commit.

Requires Go 1.25+, Docker, and Compose. Starts the control plane, shop Postgres, Aquila Postgres, Redis, the OpenTelemetry Collector, Prometheus, Grafana, and the shop.

```bash
curl -sf http://127.0.0.1:8080/healthz
curl -sf http://127.0.0.1:18080/users/user-1
curl -sf 'http://127.0.0.1:8080/v1/spans?limit=20'
curl -sf 'http://127.0.0.1:8080/v1/graph?traces=20'
curl -sf 'http://127.0.0.1:8080/v1/source'
curl -sf 'http://127.0.0.1:8080/v1/locate?traces=20'
```

`GET /v1/spans` is observed metadata from live shop traffic. `GET /v1/graph` is topology derived from those spans: an edge exists only when parent and child are in the window and the services differ. `GET /v1/source` is a typed parse of `examples/shop`: packages, files, functions, in-module imports, and typed calls. HTTP hops are not invented as call edges. `GET /v1/locate` binds spans to functions only when `code.function.name` and `code.file.path` uniquely match a source node. Neighbors: `GET /v1/source/neighbors?id=...`. `aquila status` and `aquila observe` print those APIs as text (`observed_parent` hops vs `code_attrs` binds, plus observed HTTP routes). `observe -dir` (default cwd) loads that Go module and binds locally; standing in this repository falls back to `GET /v1/source` so the shop snapshot still maps. A directory without `go.mod` prints `origin=none` and does not load shop source. `aquila impact` reads `git diff HEAD` in `-dir` when you do not pipe a diff — that is the local-IDE loop. Against a Go module it prints direct/likely/runtime/unobserved. Against anything else it is file-level (`origin=files`) and runtime only when `code.file.path` matches a changed path. It does not apply the patch or keep hunk bodies. Standing in this repository falls back to `POST /v1/impact` so a shop snapshot still maps shop diffs. Labeled D1–D6 diffs (`make impact-eval`) measure function recall on that engine; they are not extra shop bugs. `aquila env` copies `examples/shop` twice on the operator machine, applies the diff only to patch, and writes one compose file plus two env files (ports 18180/18280). It does not start containers, does not touch the live shop on 18080, and does not send experiment traces to the live Aquila store. `aquila replay -base … -patch … [-n 20]` sends the same workload to two gateways and reports match, differ, or incomplete. Without `-fixture`, that workload is GET/HEAD/OPTIONS from server-span routes in the trace window (parameterized `{…}` templates and POST/PUT/PATCH/DELETE are skipped; bodies never come from traces). `-workload file.json` is an operator file for mutating requests. `-fixture` is shop checkout smoke only. Match is not a pass. Median is printed from successful samples; p95 is withheld unless n≥20. There is no regression threshold. `aquila fault -target …` is a loopback reverse proxy that can delay or inject a status; point replay at that listen address. An injected 502 is a probe, not a pass. `aquila plan` turns impact into the smallest DAG Aquila can currently name (env, behavior, latency, and an operator fault when runtime paths exist). `aquila experiment` executes behavior and latency. Omit `-base`/`-patch` to prepare the shop pair, start Compose on the host, wait for `/healthz`, replay, and tear down; traces stay in the pair's debug collector, not the live store. That path does not boot Libra. Pass `-base` and `-patch` together when you already have two gateways. `-out` writes evidence JSON (`validated` is always false; request bodies are omitted). The same artifact is POSTed to `/v1/runs` when the API is up; if the store is down the CLI prints `unrecorded` and still keeps the local file. `aquila runs` lists stored evidence, `aquila runs <id>` shows one, `aquila runs -f out/evidence.json` imports a file. `aquila report out/evidence.json` renders that file as Markdown and does not re-hit gateways. `aquila ask "<question>"` prints observed hops, routes, binds, and optional impact facts (from `-f` or a dirty worktree on a foreign `-dir`) that contain a question token. It does not generate a patch or call an LLM. `aquila patch -dir examples/shop -f d1.diff` prints a D1 candidate (`NewEphemeral` → `Shared`) and does not write the tree. `aquila job -base … -patch …` records the plan as a DAG in Postgres (`env` skipped as operator; behavior/latency READY). `aquila worker -once` leases one READY task over gRPC on loopback `:8091`, runs replay, and commits with an attempt id. A stale attempt is rejected. Jobs are never `validated`. The Compose image snapshots that graph at build time; rebuild after shop source changes. Shop layout and defects: [examples/shop/README.md](examples/shop/README.md), [examples/shop/DEFECTS.md](examples/shop/DEFECTS.md).

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

Experiments are **jobs** with identity: git SHA, dirty worktree, workload digest, artifact digest. `aquila experiment` still writes `aquila.runs`. `aquila job` writes a DAG in `aquila.jobs`. `aquila worker -once` leases one READY task over loopback gRPC (`-slots` caps concurrent leases, default 1). The worker heartbeats while executing; a 15s lease without a heartbeat is requeued. A stale attempt cannot commit. `AQUILA_CAS_DIR` is a local SHA-256 object store plus an action cache so an identical replay can skip a second hit. Untrusted commands run in `docker run` with `--network none`, `--cap-drop ALL`, and no Docker socket; HTTP replay to operator gateways still runs on the host. Duplicate POSTs of the same run digest are idempotent. `validated` cannot be true. Killing the API does not drop stored jobs.

Ingest privacy and sandbox bounds: [docs/SECURITY.md](docs/SECURITY.md).

## Another checkout

Aquila sits beside the repo you are editing. There is no target profile. After
the control plane is up (`make dev` from this repository), the loop is:

```bash
export AQUILA_API_URL=http://127.0.0.1:8080   # default
cd /path/to/your/app
# process exports OTLP to 127.0.0.1:4317 (not otel-collector:4317 — that is Docker DNS)
aquila observe
# edit files in the IDE
aquila impact          # git diff HEAD in this directory; no pipe required
aquila plan
aquila experiment -base http://127.0.0.1:BASE -patch http://127.0.0.1:PATCH -out /tmp/evidence.json
aquila report /tmp/evidence.json
```

`observe` prints `origin=cwd` for a Go module, `origin=none` when there is no `go.mod` (Python and friends), and `origin=api` only when you are standing in the Aquila control-plane repo so shop diffs still map. It will not pretend a foreign app is `examples/shop`. `impact` without a piped diff reads **your** uncommitted changes. Typed callers (`direct` / `likely`) need a Go module. Otherwise findings are file-level: changed paths, unobserved until spans set `code.file.path`. Runtime joins only on that attribute — not on filename or route guesses. `ask` matches hops, **routes**, binds, and local impact tokens.

Do not bind the app under test to `:8080` (that is Aquila). ReRoute’s README uses 8080; pick another port. Omit `-base`/`-patch` and `experiment` prepares shop Compose only. Do not pass `-fixture` (shop checkout smoke). POST needs `-workload` you wrote.

The shop is the reference system and the only Compose env Aquila can prepare. Health-probe traces (`/healthz` and friends) are omitted from the window. The span store is still shared: hops from shop and from your app can appear together.

Match is not a pass. Stored runs have `validated=false`. `env` and `patch` remain shop-only.

## Stack

| | |
| --- | --- |
| Control plane | Go |
| Coordination state | PostgreSQL |
| Runtime signal | OpenTelemetry (OTLP) |
| Reference app | `examples/shop/` |
| Local environment | Docker Compose |
| Experiment execution | gRPC workers, 1 slot default, heartbeats, fencing |
| Artifacts | SHA-256 CAS on disk (`AQUILA_CAS_DIR`); runs JSON in Postgres |

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
