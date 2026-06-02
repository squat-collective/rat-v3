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

.PHONY: check verify lint build gen-sdks gen-images gen-check compile-sdks conformance composition validate-manifests bench core-test core-test-podman breaking help

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
