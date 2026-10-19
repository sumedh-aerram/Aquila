# CLI and API

**Purpose.** `cmd/aquila/main.go:run` dispatches commands. `internal/cli/observe.go:RunObserve`, `impact.go:RunImpact`, `plan.go:RunExperiment`, `report.go:RunReport`, `runs.go:RunRuns`, and `worker.go:RunWorker` own the main operator paths. `cmd/server/main.go` composes `internal/api/server.go:NewServer` with PostgreSQL span/run/job stores.

**Interfaces.** `aquila --help` prints the full command list. The commands dispatched in `cmd/aquila/main.go:run` are `version`, `help`, `status`, `observe`, `impact`, `env`, `replay`, `fault`, `plan`, `experiment`, `report`, `runs`, `ask`, `run`, `patch`, `job`, `jobs`, `attaches`, and `worker`; `env`, `replay`, and `fault` expose the pair, workload, and fault-injection steps individually. `ask` is deterministic fact search; `patch` and the candidate part of `run` target shop D1–D6 (`internal/cli/workflow.go:RunWorkflow`, `internal/rewrite/shop.go`). The API routes are registered in `internal/api/server.go:NewServer`.

**Evidence.** `TestRunObservePrintsHopAndBinding`, `TestRunImpactPrintsDirectAndLikely`, `TestRunExperimentDirtyWorktreeValidates`, `TestRunExperimentWritesEvidenceJSON`, and `TestRunWorkflowPlanOnly` cover command behavior.

**Gaps.** A foreign app needs operator gateways and cannot earn built validation. `ask` does not call an LLM; generic patch generation, PR integration, and a web dashboard are not present. The CLI's top-level help contains abbreviated explanations; the [local demo](../runbooks/local-demo.md) shows a concrete path.
