# System graph

The current model consists of a typed Go graph, a runtime snapshot, explicit span bindings, and an impact overlay. These are joined in memory; there is no general graph database or numeric confidence field. Provenance labels distinguish observations and inferences (`internal/source/graph.go:Snapshot`, `internal/graph/graph.go:Snapshot`, `internal/locate/bind.go:Snapshot`, `internal/impact/analyze.go:Report`).

## Nodes, edges, and provenance

| Layer | Nodes and edges | Provenance and tests |
| --- | --- | --- |
| Source | Packages, files, functions; imports, contains, calls | `syntax` for containment, `types` for imports/calls; `internal/source/load.go:Load`; `TestLoadShopContainsPaymentSymbols` |
| Runtime | Services, cross-service dependencies, root-to-leaf service paths, entry routes | `observed_parent`; parent must exist in the same trace batch; `internal/graph/graph.go:Build`; `TestBuildCycleDoesNotHang` |
| Bindings | Span ID/trace ID to function source ID and declaration line | `code_attrs`; explicit function and file required; `internal/locate/bind.go:Bind`; `TestBindDoesNotUseSpanName` |
| Changes | Changed lines to function extents, unknown paths, immediate callers | `diff`, `types`; `internal/impact/analyze.go:Analyze`; `TestAnalyzeDirectAndCaller` |
| Routes | Changed source or immediate caller to root-most routed server ancestor | `trace_entry` or `trace_entry_caller`; `TestAnalyzeEntryAncestry`, `TestAnalyzeCallerDepth` |

`Load` uses `go/packages` with tests disabled and includes typed calls only within the loaded module. It omits unresolved/interface dispatch; an HTTP dependency is not invented from a URL literal. File/function matches require a unique name in a normalized file. Missing attributes, unknown files/functions, and ambiguous matches remain unmapped (`internal/locate/bind.go:bindOne`; `TestBindAmbiguousNameInFile`).

## Entry-route attribution

`internal/graph/graph.go:EntryForSpan` uses a memoized per-trace parent index. A complete ancestor chain must reach a root. Among routed server ancestors, the root-most entry is used. A payment span nested under checkout yields the gateway's checkout route, not a replay of the payment service endpoint. Missing parents, cycles, another trace, or an unrelated root cannot establish an entry (`TestEntryForSpanRequiresCompleteAncestry`, `TestAnalyzeUsesEntryInsteadOfNestedRoute`).

Changed functions with direct bindings produce observed `trace_entry` findings. A bound immediate static caller can produce inferred `trace_entry_caller` findings, with source IDs in `Via` as caller → changed function. Depth-two callers do not qualify. A route can retain both labels (`internal/impact/analyze.go:boundFindings`; `TestAnalyzeDirectCallerPreservesBothAttributions`).

Distinct trace counts are collected before finding deduplication; repeated bindings within a trace count once. Route findings deduplicate by source, route, and provenance before the 500-finding cap. These counts describe the selected span window, not production-wide frequency (`TestAnalyzeCountsDistinctTracesAcrossBindings`, `TestAnalyzeDeduplicatesBeforeFindingCap`).

## Observed versus inferred coverage

An impacted route is exercised only when a matching workload request has recorded replies on both sides and is not auth-rejected on both sides. Templates match concrete path segments; queries are ignored for route matching (`internal/validate/decide.go:Covered`, `internal/impact/routes.go:RouteMatches`; `TestCoverageNeedsReplyOnBothSides`, `TestAuthRejectedRouteIsNotExercised`).

Caller inference is accepted by validation policy and printed in CLI, stored-run, and Markdown views. If any exercised route has only caller-inferred attribution, the decision's coverage basis is `inferred`. This does not establish that the changed branch executed. Historical evidence without provenance or counts retains unknown values (`internal/evidence/coverage.go:RouteLabel`, `internal/validate/decide.go:coverageBasis`; `TestCoverageAttributionVisibleAndAccepted`, `TestRunsViewDisclosesInferredCoverage`, `TestDecodeHistoricalV3WithoutTraceCounts`).

## Observation limits

The CLI requests a default window of 20 traces, capped at 200. Store windows omit health-only traces and retain other services in traces touching the selected `service.name` (`internal/cli/observe.go`, `internal/ingest/window.go:SelectWindow`; `TestSelectWindowServiceIgnoresNewerOtherAttach`). Missing parents or capped findings reduce coverage. `unobserved` means no direct binding in this window, not unreachable code. Findings can overlap; direct/likely/runtime/unobserved counts are not a partition.

Repository identity, tests, database entities, metrics, and SLOs are not first-class joined nodes today. The source graph is Go-only; non-Go changes receive file-level treatment (`internal/impact/files.go:FromDiff`; `TestFromDiffUnknownFileUnobserved`). See [impact component](components/impact.md).
