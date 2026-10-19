# Pair construction and replay

**Purpose.** `internal/gitrev/snapshot.go:SnapshotWorktreeAt` or `SnapshotDiffAt` records a module tree without modifying the user index. `internal/gitrev/materialize.go:Materialize` writes exact Git blob bytes. `internal/pair/prepare.go:Prepare` generates equivalent shop environments; `internal/pair/compose.go:Up` starts and health-checks both sides. `internal/replay/run.go:Run` sends the same workload to each gateway.

**Interfaces.** `internal/cli/plan.go:RunExperiment` orchestrates pair startup, replay, comparison, and teardown. `internal/replay/file.go:ReadFile` accepts an operator workload; `internal/replay/workload.go:FromSpans` selects safe concrete server routes. `internal/replay/compare.go:Compare` reports match/differ/incomplete. `internal/replay/latency.go:Summarize` measures successful samples. `internal/plan/fault.go:probeFault` performs the optional one-sided status probe.

**Evidence.** `TestSnapshotDoesNotTouchIndex`, `TestMaterializeExact`, `TestUpStartsBothAndDownsOnHealthFailure`, `TestCompareTransportErrorIsIncompleteNotMatch`, and `TestPairUpFromTrees` cover boundaries; the last requires Docker.

**Gaps.** Automatic builds support the bundled shop only. A foreign app supplies two running URLs, which stay declared and unvalidated. Images are tagged rather than digest-pinned, and host load is not controlled. Replayed POSTs require operator files or the shop fixture. See [experiments](../EXPERIMENTS.md).
