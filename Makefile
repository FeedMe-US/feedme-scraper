.PHONY: build test run clean fixtures help

# Build variables
BINARY_NAME=feedme-scraper
BUILD_DIR=bin
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS=-ldflags "-X github.com/jackwlutz/feedme/scraper/cmd.Version=$(VERSION) -X github.com/jackwlutz/feedme/scraper/cmd.BuildTime=$(BUILD_TIME) -X github.com/jackwlutz/feedme/scraper/cmd.GitCommit=$(GIT_COMMIT)"

# Default target
help:
	@echo "FeedMe Scraper - Build Commands"
	@echo ""
	@echo "Usage:"
	@echo "  make build      Build the scraper binary"
	@echo "  make test       Run all tests"
	@echo "  make run        Run the scraper (dry-run)"
	@echo "  make fixtures   Collect test fixtures from live site"
	@echo "  make clean      Remove build artifacts"
	@echo ""
	@echo "Environment variables:"
	@echo "  SUPABASE_URL         Supabase project URL"
	@echo "  SUPABASE_SERVICE_KEY Service role key"

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) .

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run the scraper in dry-run mode
run: build
	@echo "Running scraper (dry-run)..."
	./$(BUILD_DIR)/$(BINARY_NAME) run --dry-run --verbose

# Collect fixtures from live site
fixtures: build
	@echo "Collecting fixtures..."
	./$(BUILD_DIR)/$(BINARY_NAME) collect-fixtures --output testdata/

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -rf .cache

# Run with database (requires env vars)
run-prod: build
	@echo "Running scraper with database..."
	./$(BUILD_DIR)/$(BINARY_NAME) run --verbose

# Format code
fmt:
	go fmt ./...

# Run linter
lint:
	golangci-lint run

# Download dependencies
deps:
	go mod download
	go mod tidy
