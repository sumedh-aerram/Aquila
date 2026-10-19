# Experiments and validation

An experiment produces `match`, `differ`, or `incomplete`; `validated` is a separate decision computed by `internal/validate/decide.go:Decide`. A successful process exit is not a validation verdict: `internal/cli/plan.go:RunExperiment` returns an error for incomplete execution, but a completed difference or policy blocker may still exit successfully. CI therefore inspects evidence JSON.

## Immutable pair construction

For a local shop build, Aquila resolves HEAD once, captures either the selected diff or tracked and nonignored untracked module files into a temporary Git index, writes a snapshot commit, pins its ref, and materializes exact blob bytes and executable modes. The user's index is not changed. Baseline and patch record `commit`, module `tree`, repository-relative `path`, `dirty`, and `source` (`internal/gitrev/snapshot.go:Revision`, `internal/cli/revision.go`; `TestSnapshotUsesResolvedBaseline`, `TestSnapshotDoesNotTouchIndex`, `TestMaterializeExact`).

`internal/pair/prepare.go:Prepare` derives a pair ID from the two trees and writes equivalent embedded Compose definitions. Separate projects have independent database volumes and networks; gateway ports default to 18180/18280. Successful startup marks revisions `built`. An uncommitted patch can validate; the baseline must be clean (`TestDecideDirtyPatchValidates`, `TestDecideDirtyBaselineRev`). This controls the shop configuration, not arbitrary host load or image-tag changes.

Symlinks, submodules, transforming attributes/filters, byte-changing CRLF conversion, and unsupported repository settings are refused for builds. Capture/materialization have an 80 MiB cap. Investigation can print a snapshot refusal and fall back to a plain diff. URL experiments keep partial declared metadata and cannot validate (`internal/gitrev/materialize.go:Materialize`, `internal/gitrev/snapshot.go:checkAttributes`; `TestSnapshotRefusesSubmoduleSymlink`, `TestSnapshotTextAttributes`, `TestRevisionRefusalVisible`).

External Go modules require both operator gateway URLs. Those revisions are `declared` even when the operator knows the commits. Workers also use declared gateways and do not build revisions (`internal/jobs/enqueue.go:buildJob`; `TestDecideDeclaredBlocks`, `TestJobValidationRequiresBuiltRevisions`).

## Workloads and steps

| Kind | Execution and result | Source and regression tests |
| --- | --- | --- |
| Env | GET configured health path on both gateways; `prepared` only when both return 2xx | `internal/replay/healthz.go:Healthz`; `TestHealthzBothOK`, `TestHealthzRejectsDown` |
| Behavior | First replay's statuses and bodies compared; volatile JSON keys stripped recursively; opaque bodies compared after whitespace trimming | `internal/replay/compare.go:Compare`; `TestCompareMatchIgnoresVolatileIDs`, `TestCompareDetectsExtraJSONField` |
| Latency | Repeated workload; per-request min, nearest-rank median, p95/p99; `samples` verdict | `internal/replay/latency.go:Summarize`; `TestSummarizeKnownRanks`, `TestSummarizeWithholdsPercentilesWhenNSmall` |
| Concurrency | Parallel repeated workloads on each side; compare counts of transport errors, status 0, and 5xx; either increase or decrease is `differ` | `internal/plan/fault.go:compareBurst`; `TestCompareBurstIsSymmetric` |
| Fault status | One-sided loopback proxy injects 502 into a patch request; baseline must distinguish it; `prepared` is a probe, not a patch verdict | `internal/plan/fault.go:probeFault`; `internal/replay/fault_test.go:TestReplayThroughInjected502Differs` |
| Impacted tests | Required when changed Go packages exist; `go test` in current module, not separate baseline/patch tests | `internal/plan/tests.go:WithTests`, `internal/plan/tests.go:runTests`; `TestWithTestsAddsStepWhenPackagesExist`, `TestExecuteConcurrencyAndTests` |

The planner always creates env, behavior, and latency. It adds concurrency (default 8 workloads per side) and an optional fault probe when runtime findings exist. Tests are added separately. Even if the planner omits concurrency, shared validation still requires a successful concurrency result (`internal/plan/plan.go:FromImpact`, `internal/validate/decide.go:coreKinds`; `TestDecideRequiresConcurrencyWhenPlanned`).

Trace-derived replay uses concrete GET/HEAD/OPTIONS server routes; mutating requests require an operator workload or the shop fixture. A multi-service window requires `-service` for span-derived replay. The operator file supports bodies and environment-expanded allowlisted headers. Local evidence stores header names only; queued jobs reject headers but persist supplied bodies (`internal/replay/workload.go`, `internal/replay/file.go`, `internal/replay/headers.go`, `internal/cli/job.go`; `TestFromSpansSkipsPOSTAndTemplates`, `TestArtifactKeepsHeaderNamesNotValues`).

## Decision thresholds

`internal/validate/decide.go:Decide` enforces the following; these are policy constants, not measured benchmark results.

| Requirement | Blocker or interpretation | Test |
| --- | --- | --- |
| Clean baseline, both trees nonempty and `built` | `dirty_baseline`, `unverified_revision` | `TestDecideDirtyBaselineRev`, `TestDecideDeclaredBlocks` |
| Distinct trees and normalized gateway URLs; both URLs present | `same_target`, `missing_target` | `TestDecideSameTree`, `TestDecideSameTarget` |
| Overall `match` | `not_match`; an intentional bug-fix difference still requires review | `TestDecideRejectsDifferAndSmokeN` |
| Exactly one result per kind; successful env/behavior/latency/concurrency and extra required executable steps | `duplicate_step`, `step_missing`, `step_failed` | `TestDecideDuplicateKinds`, `TestDecideSkippedCoreStepNotRequired` |
| At least 20 samples on each side of every latency row | `latency_n`; actual result counts, not requested plan N | `TestDecideLatencyNIgnoresPlanN` |
| Patch median not simultaneously >2× baseline and ≥5 ms slower | `latency_shift` | `TestDecideBlockedByLatencyShift` |
| At least one function-bound change, no unanalyzed changed paths | `no_function_binding`, `unanalyzed_change` | `TestDecideUnanalyzedNonGo` |
| At least one impacted route and all impacted routes exercised | `no_runtime_route`, `route_missed` | `TestDecideNeedsRuntimeJoinForChangedCode`, `TestDecideAllRoutesRequired` |

Latency collection excludes transport/missing-status/missing-duration observations; an HTTP error status with a duration can still be a sample. Percentiles are withheld below 20; latency shifts block validation even though latency does not make the overall comparison `differ` (`internal/replay/latency.go:successfulDurations`, `internal/plan/run.go:overall`). Auth refusal on both sides yields `incomplete`, not covered success (`TestCompareAuthRejectedOnBothSidesIsIncomplete`).

Observed and caller-inferred route coverage are both accepted, with provenance printed. Coverage is route-level and does not prove a branch ran. No policy check requires every changed function to have its own observed route (`internal/validate/decide.go:changeBlockers`, `internal/validate/decide.go:coverageBasis`; `TestDecideCoverageBasis`).

## Evidence and reproducibility

Current writes use `aquila.evidence.v3`. Workload metadata, plan, classified changes, result, coverage, revisions, and blockers are digest-covered. Bodies and header values are omitted. JSON artifact size is capped at 1 MiB. Query values in workload paths may remain. Reports render stored evidence without re-running requests (`internal/evidence/artifact.go:Artifact`, `internal/cli/report.go:RunReport`; `TestBuildV3DigestStable`, `TestCheckV3RejectsForgedBlockers`).

V1 history is readable but receives `legacy_schema`; v2 has unverified baseline/patch revisions. Older v3 records without optional trace counts preserve their original digest (`TestDecodeLegacyV1Validated`, `TestDecodeM1V2NoRevisions`, `TestDecodeHistoricalV3WithoutTraceCounts`).

The API verifies consistency, not execution provenance: a client's `built` label is not independently attested. Stored metadata supports investigation, but reproducibility also needs retained Git objects, image identities, workload bodies/seeds supplied separately, and equivalent runtime conditions. See [security](SECURITY.md), [benchmark policy](BENCHMARKS.md), and [local demo](runbooks/local-demo.md).
