# Graph and source binding

**Purpose.** `internal/graph/graph.go:Build` derives service edges and paths from same-trace parent links. `internal/source/load.go:Load` type-checks a Go module into package, file, function, import, containment, and call edges. `internal/locate/bind.go:Bind` joins spans only when explicit code function and file attributes match one unique source function.

**Interfaces.** `internal/graph/graph.go:EntryForSpan` returns a routed server ancestor only for a complete chain. `internal/source/graph.go:Neighbors` exposes one-hop source relations; `cmd/sourceindex/main.go` writes a source snapshot for the server image. `internal/api/graph.go` and `internal/api/locate.go` expose derived reads.

**Evidence.** `TestEntryForSpanRequiresCompleteAncestry`, `TestLoadShopDoesNotInventHTTPAsCalls`, and `TestBindDoesNotUseSpanName` guard against fabricated edges and bindings. `TestBindShopPaymentAuthorize` exercises the demo join.

**Gaps.** Source loading is Go-only, interface dispatch is incomplete, and missing code attributes remain unmapped. Counts describe the selected trace window. There is no persisted general graph table or numeric confidence score; provenance labels carry the current distinction. See [system graph](../SYSTEM_GRAPH.md).
