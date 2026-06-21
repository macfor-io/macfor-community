# macfor - macOS Forensic Artifact Collection Tool
# Build and development targets

BINARY_NAME := macfor
BUILD_DIR := bin
GO := go
GOLANGCI_LINT := golangci-lint

# Version information (can be overridden)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

# Build flags
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)"

.PHONY: all build test lint clean fmt vet tidy coverage validate-manifests help

# Default target
all: lint test build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/macfor

# Run tests
test:
	@echo "Running tests..."
	$(GO) test -race -timeout 600s -v ./...

# Run tests with coverage
coverage:
	@echo "Running tests with coverage..."
	$(GO) test -race -coverprofile=coverage.out -covermode=atomic ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run linter
lint:
	@echo "Running linter..."
	$(GOLANGCI_LINT) run ./...

# Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

# Run go vet
vet:
	@echo "Running go vet..."
	$(GO) vet ./...

# Tidy dependencies
tidy:
	@echo "Tidying dependencies..."
	$(GO) mod tidy

# Validate all artifact manifests
validate-manifests: build
	@echo "Validating artifact manifests..."
	@./$(BUILD_DIR)/$(BINARY_NAME) manifest validate plugins/*/artifacts.yaml

# The canonical, all-plugins artifact location catalog is generated from the
# pro binary (the superset of registered plugins): run `make catalog` from
# macfor-pro/. For a community-only view, run:
#   ./bin/macfor manifest matrix --markdown

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html

# Show help
help:
	@echo "macfor build targets:"
	@echo ""
	@echo "  make build     - Build the macfor binary"
	@echo "  make test      - Run all tests"
	@echo "  make coverage  - Run tests with coverage report"
	@echo "  make lint      - Run golangci-lint"
	@echo "  make fmt       - Format code with go fmt"
	@echo "  make vet       - Run go vet"
	@echo "  make tidy      - Tidy go.mod dependencies"
	@echo "  make clean     - Remove build artifacts"
	@echo "  make validate-manifests - Validate all artifact manifests"
	@echo "  make all       - Run lint, test, and build (default)"
	@echo ""
