# Build and run helpers plus local equivalents of the checks in
# .github/workflows/ci.yml. `make ci` runs everything CI does.

BIN := circular

# Keep in step with the version pinned in CI. `go run` builds it with the
# toolchain from go.mod, as CI's `install-mode: goinstall` does, so it
# understands the newest language version. Override GOLANGCI_LINT to use a
# binary you already have.
GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.DEFAULT_GOAL := build

.PHONY: help build run demo ci tidy-check vet test lint fmt clean

help: ## List targets
	@awk -F':.*## ' '/^[a-z-]+:.*## /{printf "  %-11s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the ./circular binary
	go build -o $(BIN) ./cmd/circular

run: ## Serve live deals (STORE=524 or ZIP=08902, or set the CIRCULAR_ env vars)
	go run ./cmd/circular $(if $(STORE),-shoprite-store $(STORE)) $(if $(ZIP),-zip $(ZIP))

demo: ## Serve offline with the bundled sample deals
	go run ./cmd/circular -offline -deals-file testdata/sample_deals.json

ci: tidy-check build vet test lint ## Run every CI check

tidy-check: ## Fail if go.mod needs `go mod tidy`
	go mod tidy -diff

vet: ## Run go vet
	go vet ./...

test: ## Run tests with the race detector
	go test -race -count=1 ./...

lint: ## Run golangci-lint with .golangci.yml
	$(GOLANGCI_LINT) run

fmt: ## Format code with gofmt and goimports
	$(GOLANGCI_LINT) fmt

clean: ## Remove the built binary
	rm -f $(BIN)
