# Validation and evidence

**Purpose.** `internal/validate/decide.go:Decide` computes one policy decision from revisions, changed-code classification, route coverage, planned steps, and executed results. `internal/evidence/artifact.go:Build` produces v3 evidence with workload and artifact digests. `internal/evidence/coverage.go:coverageOf` records hit/missed routes and attribution; `internal/runs/postgres.go:Insert` stores the checked artifact.

**Interfaces.** `internal/evidence/artifact.go:Check` rejects unearned current-schema claims, while `Verify` reads historical artifacts. `internal/evidence/markdown.go` and `internal/cli/report.go:RunReport` render evidence without replaying. `internal/api/runs.go` exposes create/list/show.

**Evidence.** `TestDecideReportsAllBlockers`, `TestDecideAllRoutesRequired`, `TestCheckV3RejectsForgedBlockers`, `TestDecodeRejectsArtifactDigestMismatch`, and `TestCreateRunIdempotent` cover policy and integrity.

**Gaps.** Uploaded results and built labels are client-attested (see [standing caveats](../README.md#standing-caveats)). A digest detects modification, not fabrication. The artifact omits bodies and header values; some URL query values can remain. Legacy v1/v2 artifacts are readable but do not earn current validation. See [invariants](../INVARIANTS.md) and [security](../SECURITY.md).
