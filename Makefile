GO           ?= go
FRONTEND     := frontend

# Wails v2 defaults its cgo pkg-config flags to webkit2gtk-4.0, which is absent
# on current Arch, Fedora and Ubuntu releases; those ship webkit2gtk-4.1. The
# build fails with a confusing "Package webkit2gtk-4.0 was not found" unless
# this tag is set. Override with `make build WAILS_TAGS=` on a system that
# genuinely only provides 4.0.
WAILS_TAGS   ?= webkit2_41

# The Wails CLI and golangci-lint are installed with `go install`, and GOBIN is
# redirected into the mise-managed Go tree, so the binaries are not necessarily
# on PATH. Prefer a tool that is on PATH — a system package or a pinned
# install is the one the user meant — and fall back to GOBIN, or to GOPATH/bin
# when GOBIN is unset, which is where `go install` puts them by default. Without
# this, `make lint` and `make build` fail with "No such file or directory" on
# exactly the machines that ran `make tools` successfully.
GOBIN_DIR    := $(shell $(GO) env GOBIN 2>/dev/null)
ifeq ($(strip $(GOBIN_DIR)),)
GOBIN_DIR    := $(patsubst %/,%,$(shell $(GO) env GOPATH 2>/dev/null))/bin
endif
# An empty GOPATH above yields "/bin", which is worse than no prefix at all.
ifneq ($(GOBIN_DIR),/bin)
else
GOBIN_DIR    :=
endif

# When GOBIN_DIR is empty — $(GO) itself may be a mise shim that is not on PATH
# in a stripped environment — prefixing it would turn every tool into a bogus
# absolute path like /golangci-lint, so leave the bare name and let the recipe
# report the missing tool itself.
tool = $(shell command -v $(1) 2>/dev/null || echo $(if $(GOBIN_DIR),$(GOBIN_DIR)/,)$(1))

WAILS        ?= $(call tool,wails)
GOLANGCI_LINT?= $(call tool,golangci-lint)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

## ---------------------------------------------------------------- toolchain

.PHONY: tools
tools: ## Install the Wails CLI and golangci-lint
	$(GO) install github.com/wailsapp/wails/v2/cmd/wails@v2.14.0
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0

.PHONY: tidy
tidy: ## Reconcile go.mod / go.sum
	$(GO) mod tidy

## ------------------------------------------------------------------- checks

.PHONY: fmt
fmt: ## Format Go sources
	gofmt -s -w .

.PHONY: fmt-check
fmt-check: ## Fail if Go sources are unformatted
	@unformatted=$$(gofmt -s -l . | grep -v '^frontend/' || true); \
	if [ -n "$$unformatted" ]; then \
		echo "unformatted files:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	$(GOLANGCI_LINT) run

.PHONY: test
test: ## Run the Go test suite, including the pty smoke tests
	$(GO) test ./...

.PHONY: test-race
test-race: ## Run the Go test suite under the race detector
	$(GO) test -race ./...

.PHONY: test-pty
test-pty: ## Run only the pty smoke tests
	$(GO) test -v -run 'TestPTY' ./internal/pty/

.PHONY: check
check: fmt-check vet lint test ## Run every check CI runs

## --------------------------------------------------------------------- build

.PHONY: build
build: ## Build the desktop application
	$(WAILS) build -tags $(WAILS_TAGS)

.PHONY: dev
dev: ## Run the desktop application with hot reload
	$(WAILS) dev -tags $(WAILS_TAGS)

.PHONY: frontend-install
frontend-install: ## Install frontend dependencies
	cd $(FRONTEND) && npm install

.PHONY: frontend-build
frontend-build: ## Type-check and bundle the frontend
	cd $(FRONTEND) && npm run build

.PHONY: clean
clean: ## Remove build output
	rm -rf build/bin $(FRONTEND)/dist
