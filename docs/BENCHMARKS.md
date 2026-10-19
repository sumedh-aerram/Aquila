# Benchmarks

No reproducible benchmark results are published yet. The README's anonymized case study and SVGs preserve historical recorded output. The public tree does not include the application's source, raw run transcripts, hardware record, and complete configuration needed to reproduce those measurements; they are not a current performance baseline or a claim that this revision was exercised on that application.

The shop D1–D6 fixtures are functional evaluation cases, not performance benchmarks (`internal/impact/eval_test.go:TestEvalShopDefects`, `examples/shop/DEFECTS.md`). Latency constants and sample gates are policy, not measured speedups (`internal/replay/latency.go:Shifted`, `internal/validate/decide.go:Decide`).

## Publication record

Every future benchmark must include:

| Field | Required record |
| --- | --- |
| Hardware | CPU/architecture, memory, OS, virtualization, Docker resource limits |
| Software | Aquila commit, application baseline/patch trees, toolchain, image digests |
| Configuration | Compose/config files, quotas, service filters, health path, network topology |
| Workload | Requests, safe fixture inputs/seeds, concurrency, duration, error accounting |
| Workers | Count, declared slots, placement, relevant limits |
| Samples | Counts per route and side, aggregation method, omitted/failed samples |
| State | Warm/cold build and cache state, initial database state, reset procedure |
| Commands | Exact invocations, exit codes, environment overrides without secrets |
| Raw output | Machine-readable evidence plus raw logs, with provenance and redaction policy |

Keep baseline and patch conditions equivalent, disclose failures/timeouts/skips, and retain immutable inputs. Report variability across repeats; do not turn a single local median into a production guarantee. Performance experiments must execute rather than reuse cached results (`internal/action/cache.go:Cacheable`; `TestCacheSkipsNondeterministicKinds`).

The [local demo runbook](runbooks/local-demo.md) is a functional acceptance procedure. Running it does not supply the missing benchmark publication record.
