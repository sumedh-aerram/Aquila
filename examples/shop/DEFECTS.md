# Shop demo — known defects

These are **intentional** production-shaped problems for Aquila to discover.
Do not "fix" them as part of ordinary cleanup. They are the demo's ground truth.

| ID | Service | Defect | Runtime symptom |
| --- | --- | --- | --- |
| D1 | payment | New `http.Client` constructed on every `/authorize` | Checkout p95 dominated by payment; no connection reuse |
| D2 | users | N+1 address queries in `Store.GetUser` | Extra Postgres round-trips on every user fetch |
| D3 | inventory | `inventory_events` has no index on `sku` | Extra seq scans on reserve |
| D4 | checkout | Retry payment 5 times on any error, no backoff | Downstream amplification under faults |
| D5 | checkout → notification | Synchronous notify on the checkout critical path | Added tail latency |
| D6 | inventory | Process-wide Redis lock `inventory:global` | Lock contention under concurrency |

D4 still exists: checkout retries `/authorize` five times with no backoff, which amplifies load when the processor is actually failing. Payment records are unique per `checkout_id`, so a retry after success does not create a second charge.
