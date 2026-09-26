GO           ?= go
WAILS        ?= wails
GOLANGCI_LINT?= golangci-lint
FRONTEND     := frontend

# Wails v2 defaults its cgo pkg-config flags to webkit2gtk-4.0, which is absent
# on current Arch, Fedora and Ubuntu releases; those ship webkit2gtk-4.1. The
# build fails with a confusing "Package webkit2gtk-4.0 was not found" unless
# this tag is set. Override with `make build WAILS_TAGS=` on a system that
# genuinely only provides 4.0.
WAILS_TAGS   ?= webkit2_41

# The Wails CLI is installed with `go install`, and GOBIN is redirected into
# the mise-managed Go tree, so the binary is not necessarily on PATH.
GOBIN_DIR    := $(shell $(GO) env GOBIN 2>/dev/null)
ifeq ($(strip $(GOBIN_DIR)),)
GOBIN_DIR    := $(shell $(GO) env GOPATH)/bin
endif

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
