# Makefile for dayzctl

PROJECT := github.com/kabroxiko/dayzctl
BINARY := dayzctl
VERSION := $(shell cat pkg/version/version.txt 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X '$(PROJECT)/pkg/version.Version=$(VERSION)' \
           -X '$(PROJECT)/pkg/version.BuildTime=$(BUILD_TIME)'

# Build directories
BUILD_DIR := build
COVERAGE_DIR := coverage

# Go settings
GO := go
GOOS := linux
GOARCH := amd64
GOCMD := $(GO) build
GOTEST := $(GO) test
GOCLEAN := $(GO) clean
GOMOD := $(GO) mod
GOGET := $(GO) get
GOTIDY := $(GO) mod tidy

# Colors for output
GREEN := \033[0;32m
RED := \033[0;31m
NC := \033[0m

.PHONY: all
all: clean deps test build ## Clean, install deps, test, and build

.PHONY: build
build: deps ## Build for Linux (target)
	@echo "$(GREEN)Building $(BINARY) $(VERSION) for $(GOOS)/$(GOARCH)...$(NC)"
	@mkdir -p $(BUILD_DIR)
	GOOS=$(GOOS) GOARCH=$(GOARCH) $(GOCMD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-$(GOOS)-$(GOARCH) ./cmd/$(BINARY)
	@echo "$(GREEN)Build complete: $(BUILD_DIR)/$(BINARY)-$(GOOS)-$(GOARCH)$(NC)"

.PHONY: build-local
build-local: deps ## Build for local platform (macOS)
	@echo "$(GREEN)Building $(BINARY) $(VERSION) for local platform...$(NC)"
	@mkdir -p $(BUILD_DIR)
	$(GOCMD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-local ./cmd/$(BINARY)
	@echo "$(GREEN)Build complete: $(BUILD_DIR)/$(BINARY)-local$(NC)"

.PHONY: build-all
build-all: deps ## Build for all platforms
	@echo "$(GREEN)Building for all platforms...$(NC)"
	@mkdir -p $(BUILD_DIR)
	# Linux
	GOOS=linux GOARCH=amd64 $(GOCMD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-amd64 ./cmd/$(BINARY)
	GOOS=linux GOARCH=arm64 $(GOCMD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-linux-arm64 ./cmd/$(BINARY)
	# macOS
	GOOS=darwin GOARCH=amd64 $(GOCMD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-darwin-amd64 ./cmd/$(BINARY)
	GOOS=darwin GOARCH=arm64 $(GOCMD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-darwin-arm64 ./cmd/$(BINARY)
	# Windows
	GOOS=windows GOARCH=amd64 $(GOCMD) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY)-windows-amd64.exe ./cmd/$(BINARY)
	@echo "$(GREEN)All builds complete$(NC)"

.PHONY: install
install: build ## Install binary to /usr/local/bin
	@echo "$(GREEN)Installing to /usr/local/bin/$(BINARY)...$(NC)"
	sudo cp $(BUILD_DIR)/$(BINARY)-$(GOOS)-$(GOARCH) /usr/local/bin/$(BINARY)
	sudo chmod 755 /usr/local/bin/$(BINARY)
	@echo "$(GREEN)Installed successfully$(NC)"

.PHONY: deps
deps: ## Download and tidy dependencies
	@echo "$(GREEN)Downloading dependencies...$(NC)"
	$(GOMOD) download
	$(GOTIDY)

.PHONY: test
test: ## Run tests
	@echo "$(GREEN)Running tests...$(NC)"
	$(GOTEST) -v -race -coverprofile=$(COVERAGE_DIR)/coverage.out ./...
	$(GOTEST) -v -covermode=atomic ./...

.PHONY: test-cover
test-cover: test ## Run tests with coverage report
	@echo "$(GREEN)Generating coverage report...$(NC)"
	$(GOTEST) -coverprofile=$(COVERAGE_DIR)/coverage.out ./...
	$(GO) tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@echo "$(GREEN)Coverage report: $(COVERAGE_DIR)/coverage.html$(NC)"

.PHONY: clean
clean: ## Clean build artifacts
	@echo "$(GREEN)Cleaning...$(NC)"
	rm -rf $(BUILD_DIR)
	rm -rf $(COVERAGE_DIR)
	$(GOCLEAN)

.PHONY: fmt
fmt: ## Format code
	@echo "$(GREEN)Formatting code...$(NC)"
	$(GO) fmt ./...

.PHONY: lint
lint: ## Run linter (requires golangci-lint)
	@echo "$(GREEN)Running linter...$(NC)"
	golangci-lint run ./...

.PHONY: run
run: build-local ## Run locally
	@echo "$(GREEN)Running $(BINARY)...$(NC)"
	./$(BUILD_DIR)/$(BINARY)-local $(ARGS)

.PHONY: version
version: ## Print version
	@echo "$(BINARY) version $(VERSION)"

.PHONY: help
help: ## Show this help
	@echo "Usage: make [target]"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-15s$(NC) %s\n", $$1, $$2}'
	@echo ""

.DEFAULT_GOAL := help
