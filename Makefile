.PHONY: build install clean test lint fmt vet check run help debug-logs debug-runner-logs debug-runner-status debug-processes debug-cleanup debug-all

# Build variables
BINARY_NAME := gale
BUILD_DIR := bin
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_TIME)"

# Go commands
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOVET := $(GOCMD) vet
GOFMT := gofmt
GOMOD := $(GOCMD) mod

# Default target
all: check build

## build: Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/gale

## install: Install the binary to $GOPATH/bin
install:
	@echo "Installing $(BINARY_NAME)..."
	$(GOBUILD) $(LDFLAGS) -o $(GOPATH)/bin/$(BINARY_NAME) ./cmd/gale

## clean: Remove build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out

## test: Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v -race ./internal/...

## test-cover: Run tests with coverage
test-cover:
	@echo "Running tests with coverage..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./internal/...
	@$(GOCMD) tool cover -func=coverage.out | grep total

## test-cover-html: Generate HTML coverage report
test-cover-html: test-cover
	@$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## lint: Run linters
lint: fmt vet
	@echo "Lint passed!"

## fmt: Check code formatting
fmt:
	@echo "Checking formatting..."
	@if [ -n "$$($(GOFMT) -l .)" ]; then \
		echo "Code is not formatted:"; \
		$(GOFMT) -d .; \
		exit 1; \
	fi

## fmt-fix: Fix code formatting
fmt-fix:
	@echo "Fixing formatting..."
	$(GOFMT) -w .

## vet: Run go vet
vet:
	@echo "Running go vet..."
	$(GOVET) ./...

## check: Run all checks (fmt, vet, test)
check: lint test

## mod-tidy: Tidy go modules
mod-tidy:
	@echo "Tidying modules..."
	$(GOMOD) tidy

## mod-download: Download dependencies
mod-download:
	@echo "Downloading dependencies..."
	$(GOMOD) download

## run: Build and run gale
run: build
	@./$(BUILD_DIR)/$(BINARY_NAME) $(ARGS)

## run-webhook: Build and run in webhook mode
run-webhook: build
	@./$(BUILD_DIR)/$(BINARY_NAME) webhook $(ARGS)

## run-start: Build and run in polling mode
run-start: build
	@./$(BUILD_DIR)/$(BINARY_NAME) start $(ARGS)

## docker-build: Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build -t gale:$(VERSION) .

## debug-logs: Show gale webhook server logs
debug-logs:
	@echo "=== Gale Logs (last 50 lines) ==="
	@gale logs 2>&1 | tail -50 || echo "No logs found or gale not running"

## debug-runner-logs: Show native runner logs
debug-runner-logs:
	@echo "=== Native Runner Logs ==="
	@for log in ~/.gale/native-runners/work/*/runner.log; do \
		if [ -f "$$log" ]; then \
			echo "--- $$log ---"; \
			tail -50 "$$log"; \
			echo ""; \
		fi \
	done 2>/dev/null || echo "No runner logs found"

## debug-runner-status: Show runner exit status and signals
debug-runner-status:
	@echo "=== Runner Status Debug ==="
	@gale logs 2>&1 | grep -E "(DEBUG|exited|signal|graceful|WARN|ERROR)" | tail -30 || echo "No debug info found"

## debug-processes: Show gale-related processes
debug-processes:
	@echo "=== Gale Processes ==="
	@ps aux | grep -E "(gale|Runner\.(Listener|Worker))" | grep -v grep || echo "No processes found"
	@echo ""
	@echo "=== Runner Work Directories ==="
	@ls -la ~/.gale/native-runners/work/ 2>/dev/null || echo "No work directories"

## debug-cleanup: List exited runners pending cleanup
debug-cleanup:
	@echo "=== Runner Directories ==="
	@ls -la ~/.gale/native-runners/work/ 2>/dev/null || echo "No runners"
	@echo ""
	@echo "=== Runner PIDs (from pgrep) ==="
	@pgrep -fl "Runner\." 2>/dev/null || echo "No Runner processes"

## debug-all: Run all debug commands
debug-all: debug-logs debug-runner-status debug-runner-logs debug-processes

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed 's/^/ /'
