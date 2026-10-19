# Jobs and workers

**Purpose.** `internal/jobs/enqueue.go:buildJob` constructs task state from a plan. `internal/jobs/postgres.go:Lease` serializes leases through PostgreSQL row locks, assigns attempt IDs and increments fences. `internal/worker/rpc.go:Register` serves Lease/Heartbeat/Commit over JSON-coded gRPC; `internal/worker/execute.go:ExecuteWith` runs one task against declared gateways.

**Interfaces.** `internal/jobs/store.go:Store` has Create/Get/List/Lease/Heartbeat/Commit/RequeueExpired/Cancel. `internal/jobs/reaper.go:Loop` expires leases. `internal/action/cache.go:FromLease` keys retry result reuse; `internal/cas/cas.go:Put` stores digest-checked bytes. `internal/cli/worker.go:RunWorker` runs the CLI worker.

**Evidence.** `TestStaleAttemptCannotCommit`, `TestKilledWorkerTaskRecoversAndStaleCommitIsRejected`, `TestTwoWorkersLeaseDistinctTasks`, `TestPostgresSurvivesReconnect`, and `TestCacheReusedOnRetry` cover the major transitions; the reconnect test requires a database.

**Gaps.** Job creation has no idempotency key. Jobs store current task state, not a full attempt log. Capacity is declared slots rather than CPU/memory; workers do not build immutable revisions, so queued jobs cannot validate. The action cache is local and job-scoped for declared URLs. See [execution model](../EXECUTION_MODEL.md).
