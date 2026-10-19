# Contributing

Aquila is a Go control plane, a separate Go shop module, and a C++20 process supervisor. Read the [documentation index](docs/README.md), [architecture](docs/ARCHITECTURE.md), [invariants](docs/INVARIANTS.md), and [security boundary](docs/SECURITY.md) before changing an experiment path. Use a Go 1.26.9-compatible toolchain, GNU Make, a C++20 compiler, and Docker Compose for full integration work (`go.mod`, `examples/shop/go.mod`, `Makefile`, `executor/Makefile`).

## Developer loop

From the repository root, these commands check formatting, build, lint, and the non-Docker subset:

```bash
go mod tidy -diff
go -C examples/shop mod tidy -diff
make build
make lint
go test -race -count=1 -skip 'TestRunEcho|TestIntegration|TestPairUpFromTrees|TestPostgres' ./...
make shop-test
make executor-test
```

The skip-filtered suite is local feedback only. Full `make test`, `make ci-test`, `make pair-integration`, and end-to-end checks need an isolated Docker stack and a migrated PostgreSQL test database. See the [CI runbook](docs/runbooks/ci.md). Do not weaken or delete a failing test to obtain a green result.

## Style and change shape

Use Conventional Commits: `type(scope): imperative summary`, lowercase, no trailing period, at most 72 characters. Typical types are `feat`, `fix`, `docs`, `refactor`, `perf`, `test`, `chore`, and `ci`. Keep bodies focused on why and validation. Go code uses `gofmt`, singular package names, direct errors with `%w`, early returns, exported-name documentation, and table-driven behavior tests. Keep changes as the smallest complete increment; do not add empty phase directories or generic `utils` packages. Mechanical gates are `make lint`, root/shop tests, and the workflow in `.github/workflows/ci.yml`.

## Extending experiments

A new executable step needs a stable kind and plan dependency in `internal/plan/plan.go:FromImpact`, execution and a structured result in `internal/plan/run.go:Execute`, policy handling in `internal/validate/decide.go:Decide`, evidence serialization in `internal/evidence/artifact.go:Artifact`, and worker behavior if jobs may lease it (`internal/worker/execute.go:ExecuteWith`). Add tests that prove failure, skip, timeout, and successful completion cannot be confused (`TestDecideSkippedCoreStepNotRequired`, `TestExecuteIncompleteWhenPatchDown`).

A new blocker needs a stable code in `internal/validate/decide.go`, a deterministic subject/order, tests in `internal/validate/decide_test.go`, and explanations in [experiments](docs/EXPERIMENTS.md) and [failure model](docs/FAILURE_MODEL.md). Never claim a patch validated from a planned or missing step. Preserve existing evidence schema/digest compatibility tests if metadata changes (`TestDecodeHistoricalV3WithoutTraceCounts`).

## Pull requests

Describe the concrete trigger, behavior before/after, evidence, and remaining limits. Report exact commands and exits. Mark Docker/PostgreSQL checks that did not run; do not call an upload or CI run successful without observing it. Changes that alter the validation decision need positive and negative cases, plus the relevant invariant row. Use a linear history and the [merging runbook](docs/runbooks/merging.md). A maintainer handles staging, commits, pushes, and merges.
