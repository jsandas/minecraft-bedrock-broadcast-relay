.PHONY: all test test-unit build build-raknet build-nethernet clean docker-up docker-down example example-nethernet quality fmt fmt-check go-mod-tidy lint lint-install help

# Compose file used by the docker-up / docker-down targets
COMPOSE_FILE ?= docker-compose.yaml

# Default target
all: build

# Build both relays
build: build-raknet build-nethernet

# Build the RakNet relay (Bedrock before 26.60)
build-raknet:
	go build -o raknet_relay ./cmd/raknet_relay

# Build the NetherNet relay (Bedrock 26.50+)
build-nethernet:
	go build -o nethernet_relay ./cmd/nethernet_relay

# Run all tests and quality checks
test: quality test-unit

# Run unit tests only
test-unit:
	@go test -v ./...

# Start docker services
docker-up:
	docker compose -f $(COMPOSE_FILE) up -d

# Stop docker services
docker-down:
	docker compose -f $(COMPOSE_FILE) down

# Clean build artifacts
clean:
	rm -f raknet_relay nethernet_relay
	go clean

# Run linting with golangci-lint
lint:
	golangci-lint run --timeout=5m

# Run linting with golangci-lint (install if not present)
lint-install:
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "Installing golangci-lint..."; \
		go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0; \
	fi
	golangci-lint run --timeout=5m

# Check code formatting
fmt-check:
	@if [ "$(shell gofmt -s -l . | wc -l)" -gt 0 ]; then \
		echo "Code is not formatted. Please run 'gofmt -s -w .'"; \
		gofmt -s -l .; \
		exit 1; \
	fi
	@echo "Code formatting is correct"

go-mod-tidy:
	@go mod tidy
	@if [ -n "$(shell git status --porcelain | egrep '(go.mod|go.sum)')" ]; then \
		echo "go.mod or go.sum is not tidy. Please run 'go mod tidy'"; \
		exit 1; \
	fi
	@echo "go.mod and go.sum are tidy"

# Format code
fmt:
	gofmt -s -w .

# Run all code quality checks
quality: fmt-check go-mod-tidy lint
	@echo "All code quality checks passed!"

# Run the RakNet relay example
example: build-raknet
	./raknet_relay

# Run the NetherNet relay example
example-nethernet: build-nethernet
	./nethernet_relay

# Show help
help:
	@echo "Available targets:"
	@echo "  build               - Build both relays (raknet_relay, nethernet_relay)"
	@echo "  build-raknet        - Build the RakNet relay (Bedrock before 26.60)"
	@echo "  build-nethernet     - Build the NetherNet relay (Bedrock 26.50+)"
	@echo "  test                - Run all tests (quality checks + unit tests)"
	@echo "  test-unit           - Run unit tests"
	@echo "  docker-up           - Start docker containers (COMPOSE_FILE)"
	@echo "  docker-down         - Stop docker containers (COMPOSE_FILE)"
	@echo "  clean               - Clean build artifacts"
	@echo ""
	@echo "Code Quality:"
	@echo "  quality             - Run all code quality checks"
	@echo "  lint                - Run golangci-lint"
	@echo "  lint-install        - Install golangci-lint and run linting"
	@echo "  fmt-check           - Check code formatting"
	@echo "  fmt                 - Format code with gofmt"
	@echo "  go-mod-tidy         - Check if go.mod is tidy"
	@echo ""
	@echo "Other:"
	@echo "  example             - Build and run the RakNet relay"
	@echo "  example-nethernet   - Build and run the NetherNet relay"
	@echo "  help                - Show this help"