# Execution model

Two execution paths coexist: local CLI experiments can build an embedded shop pair, while queued workers execute HTTP steps against declared gateways. They share plan/replay and validation code. Queued workers do not build trees or earn built validation (`internal/cli/plan.go:RunExperiment`, `internal/worker/execute.go:ExecuteWith`, `internal/jobs/enqueue.go:buildJob`; `TestJobValidationRequiresBuiltRevisions`).

## Jobs, readiness, and capacity

A job stores its DAG and tasks together as JSONB in `aquila.jobs`. Only the current attempt ID, worker ID, deadline, lease expiry, fence counter, and result are retained per task; there is no full attempt-history table (`internal/jobs/store.go:Job`, `internal/jobs/store.go:Task`, `migrations/000004_jobs.up.sql`).

Tasks progress through pending → ready → leased → succeeded/failed. Operator tasks are skipped and never leased. `internal/jobs/enqueue.go:settle` promotes a pending task when dependencies are succeeded **or skipped**. Execution success means the worker returned a result; a `differ` result can be stored on a succeeded task. Validation evaluates semantic results separately (`TestCreateReadiesEnv`, `TestCreateJobSkipsOperator`, `TestSmokeJobCannotValidate`).

PostgreSQL leasing locks all active job rows in creation order, checks the worker's declared slot count, and selects ready tasks subject to per-service quotas. Slot count defaults to 1 and is capped at 8. The stock CLI executes one task at a time even if more slots are declared. There is no CPU/memory accounting or persistent worker-capacity registry (`internal/jobs/postgres.go:Lease`, `internal/jobs/quota.go`, `internal/cli/worker.go:RunWorker`; `TestTwoWorkersLeaseDistinctTasks`, `TestLeaseSkipsServiceAtLeasedCap`).

Creation serializes gateway occupancy checks with an advisory transaction lock. An unfinished job occupying a gateway hostname conflicts with another such job; ports on that host do not create independent occupancy. This is coarse conflict control, not environment identity or tenant isolation (`internal/jobs/occupancy.go`, `TestGatewayOccupancy`, `TestSameJobMayShareHost`).

## Lease, heartbeat, and fencing

A lease lasts 15 seconds. Workers heartbeat every 5 seconds. The server reaps on startup and every second. Expired tasks return to ready with the prior attempt identity cleared; a new lease generates a new attempt and increments `Fence` (`internal/jobs/store.go:leaseTTL`, `internal/worker/execute.go:heartbeatEvery`, `internal/jobs/reaper.go:Loop`; `TestHeartbeatExtendsLease`).

`Commit` checks that the task is leased and its attempt ID matches. The numeric fence is recorded but not a separate commit parameter. Old attempts cannot overwrite a replacement attempt's result. Commit and heartbeat do not independently compare wall-clock expiry; reaping changes the authoritative state (`internal/jobs/enqueue.go:applyCommit`, `internal/jobs/enqueue.go:applyHeartbeat`, `internal/worker/rpc.go:CommitRequest`).

`TestStaleAttemptCannotCommit` covers stale A after reassignment before B commits. `TestGRPCRejectsStaleAttempt` exercises RPC reassignment. `TestKilledWorkerTaskRecoversAndStaleCommitIsRejected` kills a real worker, completes replacement work, then rejects A, using an in-memory store and advanced expiry clock; see [invariants](INVARIANTS.md). These tests do not establish full durable attempt history.

## Retries, cancellation, and recovery

Lease expiry permits at-least-once execution; replayed mutations can execute again. There is no configurable retry/backoff policy or retry budget beyond the job deadline. An explicit execution error commits a failed task, rather than scheduling a retry (`internal/worker/execute.go:Once`, `internal/jobs/enqueue.go:requeue`).

Jobs default to a 10-minute deadline. Deadline reaping fails unfinished tasks and clears leases. Repeated cancellation is idempotent; it skips pending/ready tasks, while an already leased task can continue and cancellation prevents new leases. It does not guarantee remote process termination (`internal/jobs/earned.go:applyCancel`, `internal/jobs/earned.go:failDeadline`; `TestCancelStopsLeases`, `TestDeadlineFailsReady`).

PostgreSQL state survives a client reconnect (`internal/jobs/recover_test.go:TestPostgresSurvivesReconnect`). The similarly named in-memory `TestControllerRestartKeepsJob` only reads the same store; it is not a process-crash test. The reaper ignores its returned errors, so persisted jobs need operator visibility during a database outage (`internal/jobs/reaper.go:Loop`).

## Protocol and result storage

The server exposes Lease, Heartbeat, and Commit on gRPC with a manually registered JSON codec. There are no generated worker protobuf messages. `AQUILA_API_TOKEN` authenticates RPCs when set, but the client uses insecure transport credentials; use a trusted local network or external transport protection (`internal/worker/rpc.go:Register`, `internal/worker/server.go:Dial`; `TestGRPCRequiresTokenWhenSet`).

Stored runs are separate from queued jobs. Run IDs derive from artifact digest and support identical inserts. Job IDs are random; job creation has no idempotency key (`internal/runs/store.go`, `internal/runs/postgres.go:Insert`, `internal/jobs/enqueue.go:newID`; `TestCreateRunIdempotent`).

## CAS and action cache

`AQUILA_CAS_DIR` enables a directory-backed SHA-256 store and action index. `Put` hashes content; existing bytes and every `Get` are checked. Corruption returns `ErrCorrupt` (`internal/cas/cas.go:Put`, `internal/cas/cas.go:Get`; `TestCorruptRefused`). This is local filesystem storage, not MinIO/GCS or a remote artifact API.

Only behavior and tests are cacheable. Env, latency, and concurrency always execute. Key version 3 includes kind, repeats, serialized workload, health path, and job scope; it uses tree scope only if both revisions are built. Public job creation forces declared revisions, so ordinary jobs reuse within a job retry and cannot share a tree cache across jobs. A behavior match can still depend on mutable gateway state; the key does not model that state (`internal/action/cache.go:FromLease`, `internal/action/cache.go:Cacheable`; `TestCacheReusedOnRetry`, `TestCacheNotSharedAcrossJobs`). This is result caching, not a deterministic build cache.

## Separate execution helpers

The OCI runner defaults to non-root, dropped capabilities, no-new-privileges, read-only root, no network, 64 MiB memory, 0.25 CPU, 32 PIDs, and a 30-second command context. `Spec.Network=host` is permitted explicitly. It mounts no Docker socket. Context expiry kills the Docker client and does not establish container cleanup by itself (`internal/sandbox/oci.go:Args`, `internal/sandbox/oci.go:Run`; `internal/sandbox/oci_test.go`).

The C++20 supervisor forks/execs, creates a child process group, applies optional rlimits, polls waitpid, and kills the process group on wall timeout. Linux `--net none` uses a network namespace; non-Linux refuses it. `setrlimit` errors are ignored. It has no filesystem sandbox, cgroups, or general credential isolation. The Go wrapper exchanges a result JSON file (`executor/src/exec.cpp`, `executor/tests/exec_test.cpp`, `internal/nexec/nexec.go:Run`).

Neither helper automatically wraps current-module impacted tests or HTTP workers. Generic untrusted-source execution remains incomplete; see [security](SECURITY.md).
