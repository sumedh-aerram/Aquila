# Local shop demo

This procedure starts the control plane and bundled seven-service shop, sends checkout traffic, then compares a D1 payment change. It is a functional demo. The commands after `make dev` require Docker.

Last verified: 2026-10-18 at commit 08334eb (macOS arm64, Go 1.26.9, Docker Compose against a running stack): every command below exited 0, and the D1 jq assertion printed `true`. Implementation paths: `Makefile`, `deploy/compose/docker-compose.yaml`, `deploy/compose/docker-compose.shop.yaml`, `internal/cli/plan.go:RunExperiment`.

## Prerequisites

Use a clean checkout of this repository. Install Go matching `go.mod` (currently 1.26.9), Docker Engine/Desktop with Compose v2, GNU Make, Git, curl, jq, and a C++20 compiler. Allow local ports 8080, 8091, 15432, 18080, 18180, 18280, 4317, 4318, 9090, and 13000. The pair also creates separate Compose projects and volumes. From the repository root, check the Go toolchain and build the local CLI:

```bash
go version
make cli
./bin/aquila --help
```

These three commands do not need Docker. `make cli` writes only the ignored binary under `bin/` (`Makefile:cli`).

## Start and observe

Run the following from the repository root when the Docker stack is available:

```bash
make dev
make shop-smoke
./bin/aquila status
./bin/aquila observe -service gateway -traces 20
./bin/aquila impact -service gateway -traces 20 -f internal/pair/testdata/d1.diff
```

`make dev` rebuilds images, starts the stack, applies migrations, and restarts the Collector. `make shop-smoke` restocks the demo and sends traffic; stock reset preserves checkout/payment history (`Makefile:dev`, `Makefile:shop-smoke`, `examples/shop/scripts/traffic.sh`). API health is on `127.0.0.1:8080/healthz`; the shop gateway is on `127.0.0.1:18080/healthz`. `observe` should show service paths and explicit source bindings; `impact` should distinguish direct, caller, runtime, and unobserved findings (`internal/cli/observe.go:RunObserve`, `internal/cli/impact.go:RunImpact`; `TestRunObservePrintsHopAndBinding`, `TestRunImpactPrintsDirectAndLikely`). Trace counts depend on the current window.

## Compare D1

```bash
./bin/aquila experiment -fixture -n 20 -f internal/pair/testdata/d1.diff -out out/evidence.json
jq -e -f .github/scripts/assert-e2e-positive.jq out/evidence.json
./bin/aquila report out/evidence.json
```

The fixture includes `POST /checkout`, which cannot be derived as a safe replay from spans alone. The CLI captures two Git trees, starts equivalent isolated shop projects, replays behavior and latency, checks concurrency and impacted tests when planned, writes v3 evidence, and tears down the pair (`internal/replay/workload.go:ShopFixture`, `internal/cli/plan.go:RunExperiment`; `TestRunExperimentAnalyzesBuiltSnapshot`). Inspect `result.steps`, `coverage`, `blockers`, `baseline_rev`, and `patch_rev`. The jq assertion requires a validated result with an answered checkout request. A command exit of 0 by itself is not a validation verdict (`.github/scripts/assert-e2e-positive.jq`, `internal/validate/decide.go:Decide`). The source change is an immutable snapshot; it does not edit the live checkout.

If the pair cannot start, first inspect port availability and Docker/Compose logs. If a snapshot is refused, read the reported Git attribute/path reason; do not infer a built validation from a URL-only fallback. `internal/pair/compose.go:Up` and `internal/gitrev/snapshot.go:checkAttributes` define those failures.

## Stop

```bash
make down
```

This stops the live Compose project. It preserves its named volumes; pair teardown removes pair volumes (`Makefile:down`, `internal/pair/compose.go:Down`). `make up` restarts the stack and reapplies migrations.
