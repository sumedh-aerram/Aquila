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

When `AQUILA_INGEST_TOKEN` is set, `POST /v1/traces` requires header `X-Aquila-Ingest-Token`. Local Compose uses the shared demo value `aquila-local-ingest`; do not reuse it outside loopback. An empty token keeps ingest open so unit tests and a lone binary still work. The token is compared via SHA-256 so length mismatches do not short-circuit the check. `GET /v1/spans` stays unauthenticated on loopback; treat it as a local debug read.

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

`GET /v1/spans`, `GET /v1/graph`, `GET /v1/source`, `GET /v1/source/neighbors`, `GET /v1/locate`, `POST /v1/impact`, `POST /v1/runs`, `GET /v1/runs`, and `GET /v1/runs/{id}` are unauthenticated and bound to loopback with the rest of the local API. Treat them as local debug reads, not public query APIs. Source responses are names, paths, and line numbers — not file bodies. Run ids are lowercase hex; path traversal does not match. `POST /v1/runs` accepts evidence JSON (1 MiB cap), refuses `overall=pass` and `validated=true`, and does not store request bodies. Duplicate inserts with the same digest are idempotent. The table constraint `validated = FALSE` is the last line of defense. The `aquila` CLI (`status`, `observe`, `impact`, `env`, `replay`, `fault`, `plan`, `experiment`, `report`, `runs`, `ask`, `patch`, `job`, `jobs`, `worker`) is a loopback HTTP client of those same endpoints except `env`, `replay`, `fault`, the execute half of `experiment` and `worker`, and `report`, which are local. `env` copies the shop module, applies a unified diff only onto the patch tree, and writes an equivalent Compose file. It does not start containers and does not boot a foreign repo. `observe`, `impact`, `plan`, and `experiment` load the Go module in `-dir` (default cwd) on the operator machine; the Aquila control-plane module is skipped so a shop snapshot still maps shop diffs. `replay` sends operator-chosen `http`/`https` gateway URLs a workload (GET/HEAD/OPTIONS from server-span routes, `-workload` JSON the operator wrote, or `-fixture` shop smoke). Parameterized `{…}` templates and mutating methods are not taken from spans. `-workload` is a local file (64 KiB cap); bodies in that file are sent to the gateways and are not stored in evidence. They reject `file://` and do not follow cross-host redirects. `experiment` with `-base` and `-patch` is the same replay path and does not start containers. Omitting both prepares the embedded shop pair and runs `docker compose` on the host against files Aquila wrote under the pair root (project `aquila-env-<id>-{baseline,patch}`). It waits for loopback `/healthz`, always attempts `compose down --volumes`, and does not mount the Docker socket into shop containers. It does not start an operator's foreign Compose file. Pair collectors export debug only — experiment traces stay out of the live Aquila store. `experiment -out` writes a local JSON artifact (1 MiB cap) without request bodies; `validated` is always false. `experiment` also POSTs that artifact to `/v1/runs` when the API is up; a missing store prints `unrecorded` and is not a pass. `experiment -dir` also reads git HEAD of that work tree so a run started from another checkout records that SHA. `report` reads a file only, does not execute workloads, and refuses artifacts that claim pass or validated. `runs` lists or shows stored evidence or imports `-f`; it does not execute workloads. `plan` names operator steps (env, one-sided 502) but does not run them. `fault` listens on loopback only and proxies or injects against an `http`/`https` target; it rejects `file://` and userinfo, does not follow cross-host redirects, and does not mount the Docker socket. `POST /v1/impact` accepts a unified diff (1 MiB cap), does not apply it, does not read those paths from disk, and does not echo hunk text. Treat it as a local debug write. `POST /v1/jobs` records a plan DAG (1 MiB cap) with operator-supplied workload steps; `validated` cannot be true. `GET /v1/jobs` and `GET /v1/jobs/{id}` are the same class of loopback debug reads as runs. The worker gRPC listener (`AQUILA_WORKER_ADDR`, Compose publishes `127.0.0.1:8091`) uses a JSON codec; it does not execute operator env/fault steps and rejects a commit whose attempt id is not current. Workers heartbeat to extend a 15s lease; a missing heartbeat requeues the task so a later worker can commit. `AQUILA_CAS_DIR` is a local content-addressed directory (immutable after put; digest mismatch is corrupt). Action-cache hits skip a second replay of the same key. `internal/sandbox` docker-runs untrusted commands with `--network none`, `--cap-drop ALL`, `--read-only`, and never mounts the Docker socket. HTTP replay to operator gateways is still a host client. `aquila ask` only cites stored hops/binds/impact tokens. `aquila patch` prints a D1 candidate and does not write the module tree.
