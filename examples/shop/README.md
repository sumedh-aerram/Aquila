# Shop demo

Instrumented e-commerce microservices used as Aquila's runtime target.

```
gateway → checkout → users
                  → inventory → redis / postgres
                  → payment → processor
                  → notification
```

OpenTelemetry traces export to the local Collector (`otel-collector:4317`). Request and response bodies are not recorded.

## Run

From the repository root (starts Aquila control plane **and** the shop):

```bash
make dev
make shop-smoke
```

Gateway: http://localhost:18080

```bash
curl -sf http://localhost:18080/users/user-1
curl -sf -X POST http://localhost:18080/checkout \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"user-1","items":[{"sku":"sku-widget","qty":1}]}'
```

Only the gateway is published. Processor, payment, inventory, and other internals stay on the Compose network.

## Intentional defects

See [DEFECTS.md](DEFECTS.md). These exist so Aquila has a reproducible production-shaped target, not because the services skip validation.

## Tests

```bash
make shop-test
```
