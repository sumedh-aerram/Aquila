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
