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

OTLP ingest (`POST /v1/traces`) persists that same allowlisted metadata in PostgreSQL. Request and response bodies are not stored even if a client sends them as span attributes. Query strings are stripped from `http.route`, `url.path` / `http.target`, and span names before persist.

When `AQUILA_INGEST_TOKEN` is set, `POST /v1/traces` requires header `X-Aquila-Ingest-Token`. Local Compose uses the shared demo value `aquila-local-ingest`; do not reuse it outside loopback. An empty token keeps ingest open so unit tests and a lone binary still work.

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

`GET /v1/spans`, `GET /v1/graph`, `GET /v1/source`, `GET /v1/source/neighbors`, `GET /v1/locate`, and `POST /v1/impact` are unauthenticated and bound to loopback with the rest of the local API. Treat them as local debug reads, not public query APIs. Source responses are names, paths, and line numbers — not file bodies. The `aquila` CLI (`status`, `observe`, `impact`, `env`, `replay`, `fault`) is a loopback HTTP client of those same endpoints except `env`, `replay`, and `fault`, which are local. `env` copies the shop module, applies a unified diff only onto the patch tree, and writes an equivalent Compose file. `replay` sends operator-chosen `http`/`https` gateway URLs a workload (span-derived gateway routes or `-fixture`); it rejects `file://` and does not follow cross-host redirects. It does not apply a patch, does not start containers, and does not mount the Docker socket. `fault` listens on loopback only and proxies or injects against an `http`/`https` target; it rejects `file://` and userinfo, does not follow cross-host redirects, and does not mount the Docker socket. `POST /v1/impact` accepts a unified diff (1 MiB cap), does not apply it, does not read those paths from disk, and does not echo hunk text. Treat it as a local debug write.
