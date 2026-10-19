# CI runbook

The workflow is `.github/workflows/ci.yml`. It runs on pull requests and pushes to `main`, with read-only contents permission. A job result proves only the checks it actually completed; artifact upload and hosted runner success need a hosted run.

| Job | Checks and limits |
| --- | --- |
| `build-test-lint` | Go format/tidy, lint, vulnerability scan, workflow lint, PostgreSQL migration, guarded root/shop race tests, native test, build. Requires Docker for the sandbox test and a migrated PostgreSQL service. |
| `compose-config` | Both Compose files parse with `docker compose config -q`; no service starts. |
| `terraform` | Terraform format/validate; no apply or cloud deployment. |
| `integration` | Docker pair and sandbox tests under a no-skip wrapper. |
| `eval` | Impact, plan, runs, jobs, replay, attach, ingest evaluation targets; `job-eval` includes the Docker sandbox test. |
| `e2e` | Start demo, positive D1 evidence, negative health-response evidence, saved report rendering and logs, then teardown. Positive jq requires `validated=true` and answered checkout; negative jq requires `validated=false` and a behavior failure. |

The test wrapper `.github/scripts/go-test.sh` pipes Go JSON to `.github/scripts/check-test-skips.py`. It rejects malformed/empty/incomplete streams, failures, and unexpected skips. The general Go job allows only named Docker skips in `.github/test-skip-allowlist.txt`; PostgreSQL skips cannot be allowlisted. Integration passes `-` for no allowances. The guard does not require a specified named test to pass and does not reject an unused allowlist entry (`.github/scripts/check-test-skips.py`, `.github/scripts/test-skip-guard.py`). Keep changes to the allowlist narrow and explain why a skip is unavoidable in the general job; add a no-skip integration execution for that test.

## Reproduce non-Docker gates locally

From a checkout with Go 1.26.9, Make, a C++20 compiler, and dependencies available:

```bash
go mod tidy -diff
go -C examples/shop mod tidy -diff
make lint
python3 .github/scripts/test-skip-guard.py
make build
go test -race -count=1 -skip 'TestRunEcho|TestIntegration|TestPairUpFromTrees|TestPostgres' ./...
make shop-test
make executor-test
make impact-eval replay-eval plan-eval runs-eval attach-eval ingest-eval
```

Last verified: 2026-10-18 at commit 08334eb; all commands above exited 0. The explicit `-skip` command is a local subset; it is **not** the CI gate and does not prove Docker or PostgreSQL coverage. The evaluation command omits `job-eval` because that target runs `internal/sandbox` tests requiring Docker. Test names and flags are in `Makefile` and the colocated `*_test.go` files.

## Reproduce integration and end-to-end gates

With an isolated Docker/Compose stack and migrated test database, you can run the workflow targets and inspect their JSON logs:

```bash
make ci-test
make pair-integration
make job-eval
make dev
make cli
make shop-smoke
./bin/aquila experiment -fixture -n 20 -f internal/pair/testdata/d1.diff -out e2e-d1.json
jq -e -f .github/scripts/assert-e2e-positive.jq e2e-d1.json
./bin/aquila experiment -fixture -n 20 -f internal/pair/testdata/negative-healthz.diff -out e2e-negative.json
jq -e -f .github/scripts/assert-e2e-negative.jq e2e-negative.json
make down
```

The same sequence, with `AQUILA_TEST_POSTGRES_URL` set to a separate migrated database, also exited 0 on 2026-10-18 at commit 08334eb, and both jq assertions printed `true`. `make ci-test` needs `AQUILA_TEST_POSTGRES_URL` aimed at a **separate migrated test database**; the default reconnect test can otherwise use the local demo port. Do not point it at a shared demo database. `make dev` and e2e use the live stack. The exact hosted job also runs migration, Compose config validation, and upload steps as defined in the workflow; do not treat this local list as an identical hosted runner.

The Terraform job uses Terraform 1.11.4 and the `terraform-eval` Make target. It validates configuration only; see `deploy/terraform/README.md`. Vulnerability and workflow lint steps install pinned tools in CI. A failing vulnerability scan is a real gate failure, not a documentation exception.
