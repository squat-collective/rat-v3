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

.PHONY: check verify lint build gen-sdks gen-check compile-sdks help

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
	  awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

## --- fast gate (per-commit) --------------------------------------------------
check: lint ## FAST per-commit gate — buf lint only (seconds)

## --- full verification (pre-push / CI) ---------------------------------------
verify: lint build gen-check compile-sdks ## FULL check — lint + build + sdk fresh + compile

lint: ## buf lint the protos
	@echo ">> buf lint"
	@$(BUF) lint

build: ## buf build (compile the proto module graph)
	@echo ">> buf build"
	@$(BUF) build

gen-sdks: ## Regenerate contracts/sdks/<lang>/ from contracts/proto
	@scripts/gen-sdks.sh

gen-check: ## Fail if committed SDKs are stale vs contracts/proto
	@scripts/gen-sdks.sh --check

compile-sdks: ## Compile the generated Go SDK (proves it builds)
	@echo ">> go build contracts/sdks/go"
	@$(RUNTIME) run --rm $(RUNFLAGS) -e HOME=/tmp -e GOTOOLCHAIN=local \
	  -v "$(CURDIR)/$(CONTRACTS)/sdks/go:/sdk:Z" -w /sdk $(GO_IMAGE) \
	  sh -c 'go build ./... && echo "OK: Go SDK compiles"'
