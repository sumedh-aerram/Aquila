# Security

Generated code and experiment workloads are untrusted.

## Execution

Workloads must run with bounded CPU, memory, PIDs, time, filesystem, and network. Prefer non-root, read-only root filesystem, temporary writable workspace, dropped capabilities, `no-new-privileges`, and network disabled unless the experiment requires it.

Never mount `/var/run/docker.sock` into arbitrary generated-code workloads. Never expose host or cloud credentials (including metadata endpoints) to experiment workloads. No privileged containers. Artifact paths and CAS digests must be validated.

gVisor or an equivalent stronger boundary is a later option. It does not block V1.

## Telemetry privacy

Defaults:

- request body capture **off**
- response body capture **off**
- secret-value capture **off**

Store route templates, durations, status codes, service names, and span relationships. Richer capture is explicit opt-in.

OTLP ingest (`POST /v1/traces`) persists that same allowlisted metadata in PostgreSQL. Request and response bodies are not stored even if a client sends them as span attributes. Query strings, fragments, and URL userinfo are stripped from `http.route`, `url.path`, `http.target`, `url.full` / `http.url`, and span names before persist. Trace and span IDs that are not 16/8 bytes are dropped. Invalid parent IDs are omitted rather than guessed. Oversized bodies are `413`. Unknown Content-Type is `415`. Gzip is accepted (`Content-Encoding`). A batch that contains invalid or excess spans still stores the valid prefix and returns OTLP `partial_success.rejected_spans` — that is not a pass and not a silent drop.

When `AQUILA_INGEST_TOKEN` is set, `POST /v1/traces` requires header `X-Aquila-Ingest-Token`. Local Compose uses the shared demo value `aquila-local-ingest`; do not reuse it outside loopback. An empty token keeps ingest open so unit tests and a lone binary still work. The token is compared via SHA-256 so length mismatches do not short-circuit the check. When `AQUILA_API_TOKEN` is set, `/v1/*` except traces and `/metrics` require `Authorization: Bearer` or `X-Aquila-Token`. `/healthz`, `/readyz`, and `/version` stay public for probes. An empty API token keeps the local loopback API open. Cloud Run Terraform sets both tokens.

## Pen test (2026-09) and residual risk

Fixed:

- **Worker gRPC:** the listener requires `AQUILA_API_TOKEN` when it is set; a missing or wrong token is `Unauthenticated`.
- **Replay, fault proxy, and job create:**
  - They refuse link-local, multicast, and unspecified addresses and metadata hostnames.
  - They check both the URL and the dialed IP, so DNS rebinding cannot reach them.
  - Loopback and private ranges stay allowed because local gateways live there.
- **Job URLs:** URLs with userinfo are rejected at create.
- **OTLP ingest:** strips ANSI CSI/OSC/C1 sequences before control runes.
- **`POST /v1/impact`:** returns `400` for a body with no diff file headers.
- **Jobs and workload headers:** jobs refuse workloads that carry headers, so credentials never enter the job table. Local `experiment` expands `${VAR}` in header values, and evidence keeps header names only.

Accepted for local development, not for any shared host:

- **AQ-001:** with `AQUILA_API_TOKEN` empty, the HTTP API and worker gRPC are open. Compose binds them to `127.0.0.1`. Set the token before exposing either port.
- **AQ-007:** Compose Grafana allows anonymous viewers and uses `admin` / `aquila`. Change both before exposing `:13000`.

Rows ingested before the ANSI fix keep their original names.

## Agent restrictions

The agent must never automatically:

- print credentials
- commit `.env` or other secret files
- push secrets
- disable sandboxing
- weaken tests to pass
- remove validation policies
- deploy to production

V1 may inspect, diagnose, modify a local branch, run experiments, and generate reports. Opening a PR, pushing a remote branch, and deploying require confirmation.

## Control plane

Health endpoints expose liveness and PostgreSQL reachability, not secrets. Logs are structured and must redact credentials. Local Compose passwords are development-only and must not be reused in cloud deployments.

## Shop demo residual risk

The shop is an unauthenticated local target. `GET /users/{id}` and `GET /checkout/{id}` return data without auth by design. Host-published ports bind to loopback only. Do not expose this Compose stack on a public interface.

Payment `/authorize` is idempotent on `checkout_id` so checkout's intentional retry loop (D4) cannot double-charge after a succeeded processor call.

`GET /v1/spans`, `GET /v1/attaches`, `GET /v1/graph`, `GET /v1/source`, `GET /v1/source/neighbors`, `GET /v1/locate`, `POST /v1/impact`, `POST /v1/runs`, `GET /v1/runs`, and `GET /v1/runs/{id}` are loopback debug APIs. When `AQUILA_API_TOKEN` is unset they are unauthenticated; when it is set they require the bearer token. Treat them as local debug reads, not public query APIs. Source responses are names, paths, and line numbers — not file bodies. Run ids are lowercase hex; path traversal does not match. `GET /v1/attaches` lists `service.name` counts from the shared span table; it is not a tenant list. `GET /metrics` is Prometheus text for Aquila itself (ingest and HTTP counters) and must not include the ingest token. `POST /v1/runs` accepts evidence JSON (1 MiB cap), refuses `overall=pass` and unearned `validated=true`, and does not store request bodies. Duplicate inserts with the same digest are idempotent. The table constraint `NOT validated OR overall = 'match'` is the last line of defense. The `aquila` CLI (`status`, `observe`, `impact`, `env`, `replay`, `fault`, `plan`, `experiment`, `report`, `runs`, `ask`, `run`, `patch`, `job`, `jobs`, `attaches`, `worker`) is a loopback HTTP client of those same endpoints except `env`, `replay`, `fault`, the execute half of `experiment`/`run` and `worker`, and `report`, which are local. `env` copies the shop module, applies a unified diff only onto the patch tree, and writes an equivalent Compose file. It does not start containers and does not boot a foreign repo. `observe`, `impact`, `plan`, and `experiment` load the Go module in `-dir` (default cwd) on the operator machine; the Aquila control-plane module is skipped so a shop snapshot still maps shop diffs. `replay` sends operator-chosen `http`/`https` gateway URLs a workload (GET/HEAD/OPTIONS from server-span routes, `-workload` JSON the operator wrote, or `-fixture` shop smoke). A window with more than one `service.name` cannot become span-derived steps unless `-service` is set; `service` query values are clipped to 128 bytes and bound as SQL parameters. When `-dir` has a local git diff that locates onto a replayable route, those steps are preferred and labeled `changed_lines`; if none overlap, the window is used. Parameterized `{…}` templates and mutating methods are not taken from spans. `-workload` is a local file (64 KiB cap); bodies in that file are sent to the gateways and are not stored in evidence. They reject `file://` and do not follow cross-host redirects. `experiment` with `-base` and `-patch` is the same replay path and does not start containers. Omitting both prepares the embedded shop pair **only** when `-dir` is the shop module or the Aquila control-plane tree. A foreign or Python checkout must pass `-base` and `-patch`; Aquila will not boot shop Compose as a stand-in. `-fixture` is shop checkout smoke and is rejected off the shop. The shop pair runs `docker compose` on the host against files Aquila wrote under the pair root (project `aquila-env-<id>-{baseline,patch}`). It waits for loopback `/healthz`, always attempts `compose down --volumes`, and does not mount the Docker socket into shop containers. It does not start an operator's foreign Compose file. `bin/aquila-exec` is a C++20 process supervisor (rlimits + timeout). `--net none` uses a Linux network namespace and errors on macOS instead of pretending isolation. It never mounts `docker.sock`. OCI `docker run` in `internal/sandbox` remains the container path. Pair collectors export debug only — experiment traces stay out of the live Aquila store. `experiment -out` writes a local JSON artifact (1 MiB cap) without request bodies; `validated` is earned only when required executable steps succeed on a clean recorded revision. `experiment` also POSTs that artifact to `/v1/runs` when the API is up; a missing store prints `unrecorded` and is not a pass. `experiment -dir` also reads git HEAD of that work tree so a run started from another checkout records that SHA. `report` reads a file only, does not execute workloads, and refuses artifacts that claim pass or unearned validated. `runs` lists or shows stored evidence or imports `-f`; it does not execute workloads. `plan` names env, behavior, latency, concurrency and a one-sided 502 probe when runtime paths exist, and impacted `go test` when `-dir` is a Go module with packages on disk. `fault` listens on loopback only and proxies or injects against an `http`/`https` target; it rejects `file://` and userinfo, does not follow cross-host redirects, and does not mount the Docker socket. `experiment` executes env, behavior, latency, concurrency, tests, and the 502 probe; the probe does not vote overall. `aquila ask` joins question tokens onto hops/binds/impact with provenance labels (`observed_parent`, `code_attrs`, `changed_lines`); with no question it cites the local worktree only when a diff exists. `aquila run` is that investigation plus plan, a shop rewrite candidate, and experiment. `aquila patch` prints the first matching shop rewrite (D1–D6) and does not write the module tree unless `-apply`. A GCP module lives in `deploy/terraform/` (Cloud Run + Cloud SQL). It is validated in CI and is not applied from this repository. `POST /v1/jobs` records a plan DAG (1 MiB cap) with operator-supplied workload steps; overlapping unfinished jobs on the same gateway host are `409`. `validated` is earned after required tasks succeed. `POST /v1/jobs/{id}/cancel` skips remaining READY tasks. Unfinished jobs fail after a 10m deadline. `GET /v1/jobs` and `GET /v1/jobs/{id}` are the same class of loopback debug reads as runs. `service` query values on jobs and runs are clipped to 128 bytes and bound as SQL parameters. The worker gRPC listener (`AQUILA_WORKER_ADDR`, Compose publishes `127.0.0.1:8091`) uses a JSON codec and rejects a commit whose attempt id is not current. `worker -job` leases only that DAG; a non-hex job id is `InvalidArgument`, not a global lease. Workers heartbeat to extend a 15s lease; a missing heartbeat requeues the task so a later worker can commit. `AQUILA_CAS_DIR` is a local content-addressed directory (immutable after put; digest mismatch is corrupt). Action-cache hits skip a second replay of the same key. `internal/sandbox` docker-runs untrusted commands with `--network none`, `--cap-drop ALL`, `--read-only`, and never mounts the Docker socket. `internal/nexec` invokes `aquila-exec` when `AQUILA_EXECUTOR` or `bin/aquila-exec` is present. HTTP replay to operator gateways is still a host client. The CLI sends `AQUILA_API_TOKEN` as `Authorization: Bearer` when set.
