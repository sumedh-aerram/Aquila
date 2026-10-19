# Failure model

Aquila reports comparison results, policy blockers, task errors, and command errors at different levels. They are not interchangeable. `match` can coexist with `validated=false`, and a completed `differ` experiment can exit 0 (`internal/cli/plan.go:RunExperiment`, `internal/validate/decide.go:Decide`).

| Failure class | What is recorded or returned | Source and test |
| --- | --- | --- |
| User-visible behavior changes | Behavior `differ`, per-request status/body notes; `not_match`/`step_failed` block validation | `internal/replay/compare.go:Compare`; `TestCompareDetectsStatusMismatch`, `TestCompareDetectsSelectedFieldChange` |
| Transport error, timeout, missing response | `incomplete`; no answered coverage; incomplete local experiment returns command error | `internal/replay/run.go:Run`, `internal/plan/run.go:Execute`; `TestRunRecordsTransportErrorNotSuccess`, `TestExecuteIncompleteWhenPatchDown` |
| Both sides reject auth | `auth_rejected`, behavior incomplete, route missed | `internal/replay/compare.go:compareObs`; `TestCompareAuthRejectedOnBothSidesIsIncomplete`, `TestAuthRejectedRouteIsNotExercised` |
| Latency evidence missing or shifted | `latency_n` or `latency_shift`; overall may remain `match` | `internal/validate/decide.go:stepBlockers`; `TestDecideLatencyNIgnoresPlanN`, `TestDecideBlockedByLatencyShift` |
| Unknown change or no runtime join | `unanalyzed_change`, `no_function_binding`, `no_runtime_route`, `route_missed` | `internal/validate/decide.go:changeBlockers`; `TestDecideNeedsRuntimeJoinForChangedCode`, `TestDecideAllRoutesRequired` |
| Snapshot refusal | Build path returns error before startup; investigation prints refusal then plain fallback; URL experiment discloses unverified metadata | `internal/gitrev/snapshot.go:checkAttributes`, `internal/cli/revision.go`; `TestRevisionRefusalVisible`, `TestSnapshotRefusesSubmoduleSymlink` |
| Pair port conflict/startup/health failure | Error; pair teardown attempted by CLI defer; no fabricated successful evidence | `internal/pair/compose.go:Up`, `internal/cli/plan.go:RunExperiment`; `internal/pair/compose_test.go` |
| Partial pair startup | `Up` internally cleans successfully started sides only; the CLI's deferred `Down` attempts both, including the failing side | `internal/pair/compose.go:Up`, `internal/pair/compose.go:Down`; `internal/pair/compose_test.go` |
| Cleanup fails | Printed `env down failed`; cleanup error does not retroactively alter an already written artifact | `internal/cli/plan.go:RunExperiment` |
| Worker disappears | Lease expires, reaper returns task to ready; replacement receives new attempt and higher fence | `internal/jobs/reaper.go:Loop`; `TestHeartbeatExtendsLease`, `TestKilledWorkerTaskRecoversAndStaleCommitIsRejected` |
| Stale attempt returns | Current attempt mismatch → `ErrStaleAttempt` / gRPC `FailedPrecondition`; completed/nonactive tasks may return not-leased/not-found | `internal/jobs/enqueue.go:applyCommit`, `internal/worker/rpc.go:Commit`; `TestGRPCRejectsStaleAttempt` |
| Controller/database reconnect | Jobs loaded from JSONB; startup reaper expires leases; reaper errors are currently discarded | `internal/jobs/postgres.go`, `internal/jobs/reaper.go:Loop`; `TestPostgresSurvivesReconnect` |
| Worker execution error | Failed task and error text; job fails, no automatic execution-error retry | `internal/worker/execute.go:Once`, `internal/jobs/earned.go:jobDecision` |
| Deadline or cancellation | Deadline fails unfinished tasks; cancellation skips pending/ready tasks and blocks later leases; job cannot validate | `internal/jobs/earned.go:failDeadline`, `internal/jobs/earned.go:applyCancel`; `TestDeadlineFailsReady`, `TestCancelStopsLeases` |
| CAS corruption | Hard error; cache does not return corrupted object as a hit | `internal/cas/cas.go:Get`; `TestCorruptRefused` |
| Persistence unavailable | Local evidence can still be written; CLI prints `unrecorded` rather than claiming a saved run | `internal/cli/runs.go:recordRun`; `TestRunExperimentRecordsRun` |
| Invalid or forged evidence | Invalid digest/schema/claims rejected; historical schema readable under current blockers | `internal/evidence/artifact.go:Check`, `internal/evidence/artifact.go:Verify`; `TestCheckV3RejectsForgedBlockers`, `TestDecodeRejectsArtifactDigestMismatch` |

User-code test failures are `differ`; inability to start tests or cancellation is `incomplete` (`internal/plan/tests.go:runTests`). HTTP 5xx can be a behavior match on both sides and still counts as an error in concurrency. There is no comprehensive infrastructure-versus-user error enum or configurable retry taxonomy. Inspect result rows, blockers, and task errors together. See [execution](EXECUTION_MODEL.md) for retry semantics and [invariants](INVARIANTS.md) for proof gaps.
