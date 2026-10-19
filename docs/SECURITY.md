# Security boundaries

Aquila currently targets trusted local development. It accepts telemetry, diffs, workload files, and client-authored evidence, and can build application trees or send HTTP requests. These inputs must not be treated as safe merely because a validation policy accepted their metadata. General execution of arbitrary untrusted repositories is not fully isolated.

## Threat model and entry points

| Boundary | Implemented control | Residual limit and reference |
| --- | --- | --- |
| HTTP API and metrics | Optional `AQUILA_API_TOKEN`; Bearer or X-Aquila-Token; constant-time hashed comparison | Empty token permits access; health/ready/version public; `internal/api/auth.go:withAuth`, `TestAPIRequiresTokenWhenConfigured` |
| OTLP ingestion | Separate optional `AQUILA_INGEST_TOKEN`, allowlisted fields, 4 MiB payload cap including decompression, bounded batches | Local Collector receiver is unauthenticated; demo token is public; `internal/api/ingest.go:readOTLPBody`, `TestOTLPTracesRejectsOversizedBody` |
| Worker RPC | Optional same API token on Lease/Heartbeat/Commit | Client uses insecure gRPC credentials; no built-in TLS or per-worker identity; `internal/worker/server.go:Dial`, `TestGRPCRequiresTokenWhenSet` |
| Target URLs/dials | Reject userinfo, metadata hosts, link-local/multicast/unspecified IPs; recheck dialed IP | Loopback/private networks remain allowed; not a general egress sandbox; `internal/netguard/netguard.go`, `internal/netguard/netguard_test.go` |
| Revision snapshots | Temporary index, exact Git blobs, safe paths, reject symlinks/submodules/transforms | Git filters/settings refusal limits portability; refs have no GC policy; `internal/gitrev/materialize.go`, `TestSnapshotRefusesSubmoduleSymlink`, `TestSnapshotRefusesFilters` |
| Evidence uploads | Schema/digest checks, decision recomputation, current schema on writes | Client execution and built labels are not independently attested; `internal/evidence/artifact.go:Check`, `TestCheckV3RejectsForgedBlockers` |

Compose publishes API, worker, Collector, database, metrics, and Grafana ports only on loopback (`deploy/compose/docker-compose.yaml`). The standalone server defaults to `:8080`, which is not loopback-only (`internal/config/config.go:DefaultServerAddr`). Configure address/token explicitly before exposing a standalone binary.

## Execution boundaries

The embedded shop pair uses separate projects, networks, volumes, non-root application containers, dropped capabilities, no-new-privileges, and application memory/CPU limits. It does not mount the Docker socket. It does not provide a read-only root, PID limit, complete environment scrubbing, image-digest pinning, or bounded egress for all pair services (`internal/pair/compose.yaml`, `internal/pair/compose.go:dockerCompose`). Host Docker credentials/environment may be inherited by the Docker client.

`internal/sandbox/oci.go:Args` is a separate OCI runner with non-root, read-only root, no-new-privileges, dropped capabilities, default no network, CPU/memory/PID limits, and a command deadline. It permits explicit host networking. No arbitrary volume mount option is provided. Killing the Docker client on timeout does not prove workload container removal (`internal/sandbox/oci_test.go`).

The native supervisor is a separate process helper with optional rlimits and Linux network namespace isolation. It is not a filesystem/container boundary; resource-limit setup errors are ignored (`executor/src/exec.cpp`). macOS refuses network namespace requests (`executor/tests/exec_test.cpp`). Neither helper wraps current `go test` execution in `internal/plan/tests.go:runTests`. Source loading and impacted tests execute developer toolchains on the host with inherited environment. Only inspect and test repositories you trust until those paths gain an enforced untrusted-code boundary.

## Data handling

OTLP normalization retains route/method, status, duration, service, parent links, and code attributes. It drops body/secret attributes, strips URL userinfo/query/fragment, and strips terminal escape sequences. Invalid IDs are rejected; partial batches report OTLP rejected spans (`internal/ingest/otlp.go:NormalizeReport`, `internal/ingest/sanitize.go`; `TestNormalizeKeepsRouteAndDropsBody`, `TestSanitizeTextStripsTerminalSequences`, `TestOTLPTracesPartialSuccessOnInvalidIDs`). The upstream Collector/debug pipeline and any existing telemetry backend have their own retention policy.

Local workload files can send request bodies and environment-expanded allowlisted headers. Evidence excludes request/response bodies and header values, but workload paths can retain query values. Do not put secrets in paths, diffs, log messages, or fixture bodies (`internal/replay/file.go`, `internal/replay/headers.go`, `internal/evidence/artifact.go:Request`; `TestArtifactKeepsHeaderNamesNotValues`).

Queued jobs reject headers in the CLI, but their JSON wire model omits unknown headers rather than providing credential storage. Jobs **do persist operator-supplied request bodies** as task input, up to 8 KiB each. Keep those bodies free of secrets (`internal/cli/job.go`, `internal/jobs/store.go:Step`, `internal/jobs/store.go:CreateOpts`). Evidence digests omit bodies/header values, so they are not a complete commitment to authenticated replay inputs.

Service names/filters are labels, not tenant identities. Attaches aggregate a shared span table. No tenant authorization boundary, encryption-at-rest policy, deletion/retention service, or server-side execution attestation is implemented (`internal/ingest/postgres.go:ListAttaches`, `internal/jobs/enqueue.go:buildJob`).

## CI trust boundary

Pull-request CI executes submitted code, tests, Dockerfiles, and build tooling. The workflow uses `pull_request`, `contents: read`, commit-pinned actions, and checkout with persisted credentials disabled. It has no deployment or production-secret step (`.github/workflows/ci.yml`). Docker availability in a CI runner is host privilege; trusted runner separation remains required. A vulnerability gate is a check at execution time, not a standing assertion that dependencies are vulnerability-free.

Test logs, evidence, and Compose logs are uploaded on failures as well as successes. Review outputs for sensitive values before adding authenticated workloads to CI. The workflow uses artifact action defaults rather than an explicit retention period at this revision. See [CI runbook](runbooks/ci.md).

## Local-only configuration

The shop has unauthenticated user/checkout endpoints. Grafana enables anonymous viewers and uses the public demo admin credentials. PostgreSQL/Redis passwords and ingest tokens in Compose are development defaults. Do not expose this stack as a shared/public service (`deploy/compose/docker-compose.yaml`, `deploy/compose/docker-compose.shop.yaml`). Terraform configures secret references and internal Cloud Run ingress, but does not establish a verified live deployment or sandbox fleet (`deploy/terraform/main.tf`).
