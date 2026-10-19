# Impact analysis

**Purpose.** `internal/diff/parse.go:Parse` reads unified diffs; `internal/impact/analyze.go:Analyze` maps changed lines to Go function extents, inbound callers, bound spans, and observed entry routes. `internal/impact/files.go:Classify` retains unanalyzed paths so policy cannot silently call a file-level change complete.

**Interfaces.** `internal/impact/routes.go:RuntimeRoutes` returns sorted impacted route labels. `ReplayableRoutes` limits automatic replay to concrete GET/HEAD/OPTIONS paths. `internal/impact/eval.go` scores the labeled shop D1–D6 fixtures; `internal/cli/impact.go:RunImpact` renders the result.

**Evidence.** `TestAnalyzeCallerDepth`, `TestAnalyzeUsesEntryInsteadOfNestedRoute`, and `TestAnalyzeCountsDistinctTracesAcrossBindings` cover caller inference and route attribution. `TestEvalShopDefects` covers fixture scoring.

**Gaps.** The immediate caller relation is an inference, not proof that the changed callee ran. Unknown Go files, non-Go files, and unmatched lines remain visible. Interface calls and missing telemetry reduce recall; the 500-finding cap bounds output. No unexplained risk score is produced. See [system graph](../SYSTEM_GRAPH.md) and [experiments](../EXPERIMENTS.md).
