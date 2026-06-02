# RAT v3 — contracts toolchain (Phase 0).
#
# Everything runs in containers (podman preferred, docker fallback) per the
# project's container-only rule — nothing is installed on the host. The proto
# source of truth is contracts/proto/**; SDKs are generated into contracts/sdks/.
#
# Common targets:
#   make check        # FAST: buf lint only (the per-commit gate — seconds)
#   make verify       # FULL: lint + build + sdk freshness + sdk compile (pre-push/CI)
#   make gen-sdks     # regenerate contracts/sdks/<lang>/ from contracts/proto
#   make lint build   # individual buf steps

CONTRACTS := contracts
BUF_IMAGE ?= docker.io/bufbuild/buf:1.47.2
GO_IMAGE  ?= docker.io/library/golang:1.25
PY_IMAGE  ?= docker.io/library/python:3.12

# Container runtime detection (podman first).
RUNTIME := $(shell command -v podman 2>/dev/null || command -v docker 2>/dev/null)
ifeq ($(notdir $(RUNTIME)),podman)
  RUNFLAGS := --userns=keep-id
else
  RUNFLAGS :=
endif

# buf in a container, cwd = contracts/.
BUF := $(RUNTIME) run --rm $(RUNFLAGS) -e HOME=/tmp -e XDG_CACHE_HOME=/tmp/.cache \
       -v "$(CURDIR)/$(CONTRACTS):/workspace:Z" -w /workspace $(BUF_IMAGE)

.PHONY: check verify lint build gen-sdks gen-images gen-check compile-sdks conformance composition context-carriage data-dev-local data-dev-remote data-dev-remote-down data-dev-strategy data-dev-gateway data-dev-vsix validate-manifests bench core-test core-serve-smoke ratctl-smoke rat-image platform-up platform-run platform-down core-test-podman breaking help

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
	  awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

## --- fast gate (per-commit) --------------------------------------------------
check: lint ## FAST per-commit gate — buf lint only (seconds)

## --- full verification (pre-push / CI) ---------------------------------------
verify: lint build gen-check compile-sdks core-test ## FULL check — lint + build + sdk fresh + compile + core tests

lint: ## buf lint the protos
	@echo ">> buf lint"
	@$(BUF) lint

build: ## buf build (compile the proto module graph)
	@echo ">> buf build"
	@$(BUF) build

gen-sdks: ## Regenerate contracts/sdks/<lang>/ from contracts/proto (ADR-018: local plugins where available)
	@scripts/gen-sdks.sh

gen-images: ## Build the local connectionless codegen toolchain images (ADR-018)
	@for df in $(CONTRACTS)/codegen/Dockerfile.*; do \
	  lang=$${df##*.}; \
	  echo ">> building rat-codegen-$$lang"; \
	  $(RUNTIME) build -t rat-codegen-$$lang -f $$df $(CONTRACTS)/codegen; \
	done

gen-check: ## Fail if committed SDKs are stale vs contracts/proto
	@scripts/gen-sdks.sh --check

compile-sdks: ## Compile the generated Go SDK (proves it builds)
	@echo ">> go build contracts/sdks/go"
	@$(RUNTIME) run --rm $(RUNFLAGS) -e HOME=/tmp -e GOTOOLCHAIN=local \
	  -v "$(CURDIR)/$(CONTRACTS)/sdks/go:/sdk:Z" -w /sdk $(GO_IMAGE) \
	  sh -c 'go build ./... && echo "OK: Go SDK compiles"'

## --- conformance suite (0f) --------------------------------------------------
conformance: ## Run EVERY reference plugin against its golden vectors → pass/fail matrix
	@scripts/conformance.sh

## --- cross-axis composition (0i / ADR-003 cross-combination gate) -------------
composition: ## Boot catalog+engine+format together; run the strategy across 4 ADR-003 combos
	@scripts/composition.sh

## --- keystone context-carriage conformance (PU-2, ADR-017) --------------------
context-carriage: ## Cross-run the 2 context-carriage references (Go + Python) on shared vectors
	@scripts/context-carriage.sh

## --- data-dev plane local end-to-end (EXPLORATORY, experiments/data-dev-plane) ---
data-dev-local: ## Boot DuckLake catalog + DuckDB-ML engine; run transform→embed→search locally
	@scripts/data-dev-local.sh

data-dev-remote: ## Boot MinIO+Postgres; run the pipeline remote (S3 data, Postgres metadata, vended creds)
	@scripts/data-dev-remote.sh

data-dev-remote-down: ## Tear down the MinIO+Postgres data-dev remote stack
	@scripts/data-dev-remote.sh --down

data-dev-strategy: ## Run the incremental-embed ELT strategy (2 runs + idempotent replay)
	@scripts/data-dev-strategy.sh

data-dev-gateway: ## Serve the data-dev gateway (the VS Code extension's backend) on :8787
	@scripts/data-dev-gateway.sh

data-dev-vsix: ## Package the vscode-rat extension into an installable .vsix
	@scripts/data-dev-vsix.sh

## --- manifest validation (ADR-011 / the static half of `rat plugin validate`) -
validate-manifests: ## Validate example manifests vs envelope + per-kind schemas; assert the INVALID corpus is rejected
	@$(RUNTIME) run --rm -v "$(CURDIR)":/work:Z -v rat-pipcache:/root/.cache/pip -w /work \
	  $(PY_IMAGE) bash -c 'pip install -q --root-user-action=ignore jsonschema pyyaml >/dev/null 2>&1 && python scripts/validate-manifests.py'

bench: ## Per-RPC latency benchmark: core-mediated gateway overhead vs direct (0f)
	@$(RUNTIME) run --rm $(RUNFLAGS) -v "$(CURDIR)":/work:Z -v rat-gocache:/go/pkg/mod \
	  -w /work/examples/bench/latency-go $(GO_IMAGE) go run . $(N)

## --- spike core (Phase 1, ADR-013/014) ---------------------------------------
core-test: ## Build + vet + test the spike core (core/) in a container
	@echo ">> go build + vet + test ./core/..."
	@$(RUNTIME) run --rm $(RUNFLAGS) -e HOME=/tmp -e GOTOOLCHAIN=local -e GOSUMDB=off -e GOFLAGS=-mod=mod \
	  -v "$(CURDIR):/work:Z" -v rat-gocache:/go/pkg/mod -w /work/core \
	  $(GO_IMAGE) sh -c 'go build ./... && go vet ./... && go test ./...'

core-serve-smoke: ## ADR-019 Phase A: `rat serve` boots a plugin, routes (C5)+denies+drains (containerized)
	@echo ">> rat serve: route + deny + SIGTERM-drain smoke (core/cmd/rat)"
	@$(RUNTIME) run --rm $(RUNFLAGS) -e HOME=/tmp -e GOTOOLCHAIN=local -e GOSUMDB=off -e GOFLAGS=-mod=mod \
	  -v "$(CURDIR):/work:Z" -v rat-gocache:/go/pkg/mod -w /work/core \
	  $(GO_IMAGE) sh -c 'go test ./cmd/rat/ -run TestServe -v -count=1'

ratctl-smoke: ## ADR-019: ratctl (standalone client) drives a live `rat serve` gateway — routes + denies
	@echo ">> ratctl: client → orchestrator (route + C5 deny) smoke (core/cmd/ratctl)"
	@$(RUNTIME) run --rm $(RUNFLAGS) -e HOME=/tmp -e GOTOOLCHAIN=local -e GOSUMDB=off -e GOFLAGS=-mod=mod \
	  -v "$(CURDIR):/work:Z" -v rat-gocache:/go/pkg/mod -w /work/core \
	  $(GO_IMAGE) sh -c 'go test ./cmd/ratctl/ -run TestRatctl -v -count=1'

rat-image: ## ADR-019: build the rat control-plane daemon image (run `rat serve` in a container)
	@echo ">> building rat/serve:dev (core/Dockerfile)"
	@$(RUNTIME) build -f core/Dockerfile -t rat/serve:dev .
	@echo ">> built rat/serve:dev — run it with:  $(notdir $(RUNTIME)) run --rm -p 7777:7777 rat/serve:dev"

## --- the data platform bundle (ADR-020) --------------------------------------
platform-up: rat-image ## ADR-020 S1: bring up the always-on data platform stack (Postgres+MinIO+engine+catalog+rat serve)
	@echo ">> platform: $(notdir $(RUNTIME)) compose up — the always-on stack (rat serve attaches to the plugins)"
	@$(RUNTIME) compose -f platform/compose.yaml up -d

platform-run: ## ADR-020 S1: run the medallion once through the rat serve gateway (bronze→silver→gold)
	@$(RUNTIME) compose -f platform/compose.yaml run --rm runner

platform-down: ## tear the data platform stack down (and its volumes)
	@$(RUNTIME) compose -f platform/compose.yaml down -v

## --- podman deployment-runtime LIVE full-profile proof (D1 / ADR-016 §4) ------
# Drives a REAL `podman run` (nested) under the full I9 profile and asserts the kernel
# enforced every control. Needs Go + podman together (the testimage/) and a privileged
# container for nested podman; kept OUT of `core-test`/`verify` (the plain go image has
# no podman, so the test SKIPs there). Run this explicitly to close D1.
core-test-podman: ## Live full-profile proof for the podman deployment-runtime (privileged; nested podman)
	@echo ">> podman runtime: live full-profile launch (nested, privileged)"
	@$(RUNTIME) build -t rat-go-podman:test core/deploymentruntime/testimage
	@$(RUNTIME) run --rm --privileged -e HOME=/tmp -e GOTOOLCHAIN=local -e GOSUMDB=off \
	  -e GOFLAGS=-mod=mod -e GOPATH=/go -e CGO_ENABLED=0 -e RAT_PODMAN_TEST=1 \
	  -v "$(CURDIR):/work:Z" -v rat-gocache:/go/pkg/mod -w /work/core \
	  rat-go-podman:test sh -c 'go test ./deploymentruntime/ ./composition/ -run Podman -v -count=1'

breaking: ## Fail on a breaking proto change vs the sealed baseline (branch main)
	@echo ">> buf breaking vs main"
	@$(RUNTIME) run --rm $(RUNFLAGS) -e HOME=/tmp -e XDG_CACHE_HOME=/tmp/.cache \
	  -v "$(CURDIR):/workspace:Z" -w /workspace $(BUF_IMAGE) \
	  breaking contracts --against '.git#branch=main,subdir=contracts'
