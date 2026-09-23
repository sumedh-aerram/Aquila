# Aquila — local developer workflow
#
# Common targets:
#   make help     List targets
#   make cli      Rebuild ./bin/aquila (seconds; use while iterating)
#   make build    Build CLI and control-plane binaries
#   make test     Run unit tests with the race detector
#   make lint     Run golangci-lint
#   make up       Start local stack without rebuilding images
#   make dev      Rebuild API image, start stack, migrate
#   make down     Stop local infrastructure

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

MODULE      := github.com/sumedhaerram/aquila
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GO          ?= go
GOBIN       := $(CURDIR)/bin
AQUILA      := $(GOBIN)/aquila
COMPOSE     := docker compose -f deploy/compose/docker-compose.yaml -f deploy/compose/docker-compose.shop.yaml
GOLANGCI_VERSION ?= v2.1.6
LDFLAGS     := -X $(MODULE)/internal/version.Version=$(VERSION)
SHOP_GW     := http://127.0.0.1:18080

.PHONY: help build cli test shop-test lint fmt tidy migrate dev up down logs version shop-smoke ingest-smoke ingest-eval graph-smoke source-index source-smoke locate-smoke cli-smoke impact-smoke impact-eval env-smoke replay-eval plan-eval runs-eval attach-eval experiment-smoke job-eval job-smoke terraform-eval executor executor-test

help:
	@printf '%s\n' \
		'Aquila developer targets' \
		'' \
		'  make cli       Rebuild ./bin/aquila (seconds; use this while iterating)' \
		'  make build     Build aquila, aquila-server, sourceindex, and aquila-exec into ./bin' \
		'  make test      go test -race ./..., shop tests, and aquila-exec tests' \
		'  make shop-test race tests for the demo shop module' \
		'  make lint      golangci-lint (control plane + shop)' \
		'  make fmt       go fmt ./...' \
		'  make tidy      go mod tidy' \
		'  make migrate   Apply PostgreSQL migrations' \
		'  make up        Start control plane + shop without rebuilding images' \
		'  make dev       Rebuild API image, start stack, apply migrations' \
		'  make shop-smoke  Hit gateway health, user fetch, and checkout' \
		'  make ingest-smoke  List spans Aquila stored after shop traffic' \
		'  make ingest-eval   OTLP normalize and ingest API tests (privacy + pentest)' \
		'  make graph-smoke  Derive services, edges, and paths from stored spans' \
		'  make source-index  Parse examples/shop into out/source.json' \
		'  make source-smoke  Print the loaded shop source graph' \
		'  make locate-smoke  Bind recent spans to source functions' \
		'  make cli-smoke   aquila status, observe, ask, impact, patch' \
		'  make impact-smoke  Pipe a D1-style payment diff through aquila impact' \
		'  make impact-eval   Score labeled D1–D6 diffs (function recall)' \
		'  make env-smoke     Prepare an isolated baseline/patch pair from a D1 diff' \
		'  make replay-eval   Replay, latency, and fault tests (no fake pass)' \
		'  make plan-eval     Experiment plan, execute, and evidence-report tests (no fake pass)' \
		'  make runs-eval     Persist experiment runs in the control plane (no fake pass)' \
		'  make attach-eval   Cwd-native observe/impact and span-derived GET replay' \
		'  make experiment-smoke  Prepare, start, replay, and tear down a shop pair' \
		'  make job-eval      Job DAG, worker lease/commit, ask, and patch tests' \
		'  make job-smoke     Enqueue a fixture job against the live shop and lease it' \
		'  make terraform-eval  terraform fmt and validate (does not apply)' \
		'  make executor      Build C++ aquila-exec into ./bin' \
		'  make executor-test Native supervisor tests (rlimits, timeout; net ns is Linux)' \
		'  make down      Stop the local Compose stack' \
		'  make logs      Tail Compose logs' \
		'  make version   Print the build version string'

cli:
	@mkdir -p '$(GOBIN)'
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o '$(AQUILA)' ./cmd/aquila

build: cli executor
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o '$(GOBIN)/aquila-server' ./cmd/server
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o '$(GOBIN)/sourceindex' ./cmd/sourceindex

executor:
	$(MAKE) -C executor
	@mkdir -p '$(GOBIN)'
	cp executor/aquila-exec '$(GOBIN)/aquila-exec'

executor-test:
	$(MAKE) -C executor test

test:
	$(GO) -C examples/shop mod download
	$(GO) test -race -count=1 ./...
	$(MAKE) shop-test
	$(MAKE) executor-test

shop-test:
	$(GO) -C examples/shop test -race -count=1 ./...

lint: $(GOBIN)/golangci-lint
	'$(GOBIN)/golangci-lint' run ./...
	cd examples/shop && '$(GOBIN)/golangci-lint' -c '$(CURDIR)/.golangci.yml' run ./...

shop-smoke:
	bash examples/shop/scripts/traffic.sh

ingest-smoke: shop-smoke
	@curl -sf 'http://127.0.0.1:8080/v1/spans?limit=20'

ingest-eval:
	$(GO) test -race -count=1 ./internal/ingest ./internal/api -run 'TestNormalize|TestDecode|TestOTLP|TestSanitize|TestClip'
	$(GO) -C examples/shop test -race -count=1 ./internal/httpx ./internal/telemetry

graph-smoke: ingest-smoke
	@curl -sf 'http://127.0.0.1:8080/v1/graph?traces=20'

source-index:
	@mkdir -p out
	$(GO) run ./cmd/sourceindex -dir examples/shop -out out/source.json

source-smoke:
	@curl -sf 'http://127.0.0.1:8080/v1/source'

locate-smoke: ingest-smoke
	@curl -sf 'http://127.0.0.1:8080/v1/locate?traces=20'

cli-smoke: ingest-smoke cli
	'$(AQUILA)' status -api http://127.0.0.1:8080
	'$(AQUILA)' observe -api http://127.0.0.1:8080 -traces 20
	'$(AQUILA)' ask -api http://127.0.0.1:8080 -traces 20 checkout
	@printf '%s\n' \
		'diff --git a/examples/shop/internal/payment/handler.go b/examples/shop/internal/payment/handler.go' \
		'--- a/examples/shop/internal/payment/handler.go' \
		'+++ b/examples/shop/internal/payment/handler.go' \
		'@@ -142,7 +142,7 @@ func (h *Handler) chargeProcessor(ctx context.Context, req authorizeReq) error {' \
		' 	// INTENTIONAL DEFECT D1: new HTTP client on every authorize (DEFECTS.md).' \
		'-	client := svcclient.NewEphemeral()' \
		'+	client := svcclient.Shared()' \
		' 	return svcclient.PostJSON(ctx, client, h.processorURL+"/charge", map[string]any{' \
		' 		"checkout_id":  req.CheckoutID,' \
		' 		"amount_cents": req.AmountCents,' \
	| '$(AQUILA)' impact -api http://127.0.0.1:8080 -traces 20
	'$(AQUILA)' patch -dir examples/shop -f internal/pair/testdata/d1.diff -api http://127.0.0.1:8080

impact-smoke: ingest-smoke cli
	@printf '%s\n' \
		'diff --git a/examples/shop/internal/payment/handler.go b/examples/shop/internal/payment/handler.go' \
		'--- a/examples/shop/internal/payment/handler.go' \
		'+++ b/examples/shop/internal/payment/handler.go' \
		'@@ -142,7 +142,7 @@ func (h *Handler) chargeProcessor(ctx context.Context, req authorizeReq) error {' \
		' 	// INTENTIONAL DEFECT D1: new HTTP client on every authorize (DEFECTS.md).' \
		'-	client := svcclient.NewEphemeral()' \
		'+	client := svcclient.Shared()' \
		' 	return svcclient.PostJSON(ctx, client, h.processorURL+"/charge", map[string]any{' \
		' 		"checkout_id":  req.CheckoutID,' \
		' 		"amount_cents": req.AmountCents,' \
	| '$(AQUILA)' impact -api http://127.0.0.1:8080 -traces 20

impact-eval:
	$(GO) test -race -count=1 ./internal/impact -run 'TestEval|TestScore'

env-smoke: cli
	@rm -rf out/env-smoke
	'$(AQUILA)' env -shop examples/shop -out out/env-smoke -f internal/pair/testdata/d1.diff

replay-eval:
	$(GO) test -race -count=1 ./internal/replay ./internal/fault
	$(GO) test -race -count=1 ./internal/cli ./cmd/aquila -run 'TestRunReplay|TestRunFault|TestCommandTimeout|TestRunHelp'

plan-eval:
	$(GO) test -race -count=1 ./internal/pair ./internal/plan ./internal/evidence
	$(GO) test -race -count=1 ./internal/cli ./cmd/aquila -run 'TestRunPlan|TestRunExperiment|TestRunReport|TestCommandTimeout|TestRunHelp'

runs-eval:
	$(GO) test -race -count=1 ./internal/runs ./internal/gitrev ./internal/evidence
	$(GO) test -race -count=1 ./internal/api -run 'TestCreateRun|TestGetRun'
	$(GO) test -race -count=1 ./internal/cli ./cmd/aquila -run 'TestRunExperimentRecords|TestRunRuns|TestRunHelp'

attach-eval:
	$(GO) test -race -count=1 ./internal/replay -run 'TestFromSpans|TestShopFixture|TestReadFile'
	$(GO) test -race -count=1 ./internal/api -run 'TestListSpans'
	$(GO) test -race -count=1 ./internal/cli -run 'TestRunImpact|TestLoadTarget|TestReportFromLocal|TestRunReplay|TestRunObserve'

experiment-smoke: cli
	@mkdir -p out
	'$(AQUILA)' experiment -fixture -n 1 -f internal/pair/testdata/d1.diff -out out/evidence.json

job-eval:
	$(GO) test -race -count=1 ./internal/investigate ./internal/rewrite ./internal/jobs ./internal/worker ./internal/cas ./internal/action ./internal/sandbox ./internal/nexec
	$(GO) test -race -count=1 ./internal/api -run 'TestCreateJob'
	$(GO) test -race -count=1 ./internal/cli ./cmd/aquila -run 'TestRunAsk|TestRunWorkflow|TestRunPatch|TestRunJob|TestRunHelp|TestCommandTimeout'

job-smoke: cli
	'$(AQUILA)' job -fixture -n 1 -base '$(SHOP_GW)' -patch '$(SHOP_GW)' -f internal/pair/testdata/d1.diff
	'$(AQUILA)' worker -once
	'$(AQUILA)' worker -once
	'$(AQUILA)' worker -once
	'$(AQUILA)' worker -once
	'$(AQUILA)' worker -once
	'$(AQUILA)' worker -once
	'$(AQUILA)' jobs

terraform-eval:
	terraform fmt -check -recursive deploy/terraform
	terraform -chdir=deploy/terraform init -backend=false -input=false
	terraform -chdir=deploy/terraform validate

fmt:
	$(GO) fmt ./...
	$(GO) -C examples/shop fmt ./...

tidy:
	$(GO) mod tidy

migrate:
	$(COMPOSE) run --rm migrate

dev:
	$(COMPOSE) up -d --build
	$(COMPOSE) run --rm migrate
	$(COMPOSE) restart otel-collector

up:
	$(COMPOSE) up -d
	$(COMPOSE) run --rm migrate
	$(COMPOSE) restart otel-collector

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f --tail=200

version:
	@printf '%s\n' '$(VERSION)'

$(GOBIN)/golangci-lint:
	@mkdir -p '$(GOBIN)'
	GOBIN='$(GOBIN)' $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)
