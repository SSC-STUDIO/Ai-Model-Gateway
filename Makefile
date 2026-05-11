# AI Model Gateway - Makefile
# Unified build, test, lint, and bundle targets.

SHELL := /bin/bash
.DEFAULT_GOAL := help

# Version injection
VERSION ?= $(shell cat VERSION 2>/dev/null || echo dev)
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
  -X ai-model-gateway/internal/version.ProductVersion=$(VERSION) \
  -X ai-model-gateway/internal/version.BuildCommit=$(GIT_COMMIT) \
  -X ai-model-gateway/internal/version.BuildDate=$(BUILD_DATE)

# Binaries
BINARIES := aigw gatewayd controld telemetryd gateway-cli
BIN_DIR       := bin
INSTALL_PREFIX ?= /usr/local/bin

# Platforms
PLATFORMS := linux/amd64 linux/arm64 darwin/arm64 windows/amd64

# Go flags
GOFLAGS := -trimpath
GOTESTFLAGS := -timeout 10m -coverprofile=coverage.out -covermode=atomic

# ──────────────────────────────────────────────
# Build
# ──────────────────────────────────────────────

.PHONY: build
build: $(BINARIES) ## Build all binaries for the current platform

$(BINARIES): %:
	@mkdir -p $(BIN_DIR)
	go build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$@ ./cmd/$@

.PHONY: build-cross
build-cross: ## Cross-compile for all release platforms
	@mkdir -p $(BIN_DIR)
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} ext=""; \
		if [ "$$GOOS" = "windows" ]; then ext=".exe"; fi; \
		for bin in $(BINARIES); do \
			echo "Building $$GOOS/$$GOARCH/$$bin"; \
			GOOS=$$GOOS GOARCH=$$GOARCH go build $(GOFLAGS) -ldflags '$(LDFLAGS)' \
				-o "$(BIN_DIR)/$${bin}-$${GOOS}-$${GOARCH}$${ext}" ./cmd/$$bin || exit 1; \
		done; \
	done

# ──────────────────────────────────────────────
# Install
# ──────────────────────────────────────────────

.PHONY: install
install: build ## Install binaries to /usr/local/bin
	@for bin in $(BINARIES); do \
		echo "Installing $$bin -> $(INSTALL_PREFIX)/$$bin"; \
		cp "$(BIN_DIR)/$$bin" "$(INSTALL_PREFIX)/$$bin"; \
	done
	@echo "Installed $(words $(BINARIES)) binaries to $(INSTALL_PREFIX)"

# ──────────────────────────────────────────────
# Test
# ──────────────────────────────────────────────

.PHONY: test
test: ## Run all tests with coverage
	go test $(GOTESTFLAGS) ./...

.PHONY: test-race
test-race: ## Run tests with race detector (no coverage)
	go test -timeout 10m -race ./...

.PHONY: test-short
test-short: ## Run tests in short mode
	go test -short ./...

.PHONY: coverage
coverage: test ## Run tests and open coverage report
	@go tool cover -func=coverage.out | tail -1

# ──────────────────────────────────────────────
# Lint / Format
# ──────────────────────────────────────────────

.PHONY: fmt
fmt: ## Format Go source files
	gofmt -w $(shell find . -name '*.go' -not -path './.git/*' -not -path './node_modules/*')

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run --timeout=5m

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: vuln
vuln: ## Run govulncheck
	go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

.PHONY: check
check: fmt vet lint test ## Run all checks (fmt, vet, lint, test)

# ──────────────────────────────────────────────
# Bundle
# ──────────────────────────────────────────────

.PHONY: bundle
bundle: build ## Build all binaries and create aigw bundle
	$(BIN_DIR)/aigw bundle build -root . -out aigw-manifest.json

.PHONY: bundle-verify
bundle-verify: bundle ## Build bundle and verify it
	$(BIN_DIR)/aigw bundle verify -root . -manifest aigw-manifest.json

# ──────────────────────────────────────────────
# Docker
# ──────────────────────────────────────────────

.PHONY: docker
docker: ## Build Docker image locally
	docker build --target runtime -t ai-model-gateway:$(VERSION) .

.PHONY: docker-multiarch
docker-multiarch: ## Build and push multi-arch Docker image (requires GHCR login)
	docker buildx build --platform linux/amd64,linux/arm64 \
		--target runtime \
		-t ghcr.io/$(shell git remote get-url origin | sed 's/.*[:/]\([^/]*\)\/\([^.]*\).*/\1\/\2/'):$(VERSION) \
		-t ghcr.io/$(shell git remote get-url origin | sed 's/.*[:/]\([^/]*\)\/\([^.]*\).*/\1\/\2/'):latest \
		--push .

# ──────────────────────────────────────────────
# Frontend
# ──────────────────────────────────────────────

.PHONY: frontend
frontend: ## Build admin frontend
	cd web/admin && npm ci && npm run build

.PHONY: frontend-test
frontend-test: ## Run admin frontend tests
	cd web/admin && npm ci && npm test

# ──────────────────────────────────────────────
# Clean
# ──────────────────────────────────────────────

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) coverage.out aigw-manifest.json dist/

# ──────────────────────────────────────────────
# Help
# ──────────────────────────────────────────────

.PHONY: help
help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
