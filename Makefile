.PHONY: build run docker-build docker-run clean test help build-all dev-setup

# Default target
help:
	@echo "AzNetMon - ICMP Network Monitor"
	@echo "Available commands:"
	@echo "  build        - Build the Go binary"
	@echo "  build-all    - Build for multiple platforms"
	@echo "  run          - Run locally with default targets"
	@echo "  docker-build - Build Docker image"
	@echo "  docker-run   - Run Docker container"
	@echo "  compose-up   - Start with docker-compose"
	@echo "  compose-down - Stop docker-compose"
	@echo "  clean        - Clean build artifacts"
	@echo "  test         - Run tests"
	@echo "  dev          - Run in development mode with hot reload"
	@echo "  dev-setup    - Setup development environment"
	@echo "  fmt          - Format code with go fmt and go vet"
	@echo "  security     - Run security checks with gosec"
	@echo ""
	@echo "Examples:"
	@echo "  make run TARGETS='8.8.8.8,1.1.1.1'"
	@echo "  make docker-run TARGETS='192.168.1.1,google.com'"

# Build the Go binary
build:
	@echo "Building aznetmon..."
	go mod tidy
	go build -o aznetmon main.go
	@echo "Build complete: ./aznetmon"

# Build for multiple platforms
build-all:
	@echo "Building for multiple platforms..."
	GOOS=linux GOARCH=amd64 go build -o aznetmon-linux-amd64 main.go
	GOOS=darwin GOARCH=amd64 go build -o aznetmon-darwin-amd64 main.go
	GOOS=darwin GOARCH=arm64 go build -o aznetmon-darwin-arm64 main.go
	GOOS=windows GOARCH=amd64 go build -o aznetmon-windows-amd64.exe main.go
	@echo "Cross-platform builds complete"

# Run locally with optional targets
TARGETS ?= 8.8.8.8,1.1.1.1,google.com
PORT ?= 8080

run: build
	@echo "Starting aznetmon with targets: $(TARGETS)"
	sudo ./aznetmon -targets "$(TARGETS)" -port $(PORT)

# Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build -t aznetmon:latest .
	@echo "Docker image built: aznetmon:latest"

# Run Docker container
docker-run: docker-build
	@echo "Running Docker container with targets: $(TARGETS)"
	docker run --rm --cap-add=NET_RAW -p $(PORT):8080 \
		-e ICMP_TARGETS="$(TARGETS)" \
		aznetmon:latest

# Run with docker-compose
compose-up:
	docker-compose up --build -d
	@echo "AzNetMon started with docker-compose"
	@echo "Dashboard: http://localhost:8080"

compose-down:
	docker-compose down
	@echo "AzNetMon stopped"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f aznetmon aznetmon-*
	docker rmi aznetmon:latest 2>/dev/null || true
	@echo "Clean complete"

# Run tests (placeholder for future tests)
test:
	go test -v ./...

# Development mode with live reload (requires air)
dev:
	@if ! command -v air > /dev/null; then \
		echo "Installing air for live reload..."; \
		go install github.com/cosmtrek/air@latest; \
	fi
	air

# Install air for development
install-dev-tools:
	go install github.com/cosmtrek/air@latest

# Check for security vulnerabilities
security:
	@if ! command -v gosec > /dev/null; then \
		echo "Installing gosec..."; \
		go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest; \
	fi
	gosec ./...

# Format code
fmt:
	go fmt ./...
	go vet ./...

# Dev environment setup
dev-setup: install-dev-tools
	@echo "Setting up development environment..."
	@echo "Installing air for hot reload..."
	go install github.com/cosmtrek/air@latest
	@echo "Installing gosec for security scanning..."
	go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest
	@echo "Installing golangci-lint..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Installing delve debugger..."
	go install github.com/go-delve/delve/cmd/dlv@latest
	@echo "Development environment setup complete!"

# Show project status
status:
	@echo "=== AzNetMon Project Status ==="
	@echo "Go version: $$(go version)"
	@echo "Dependencies:"
	@go list -m all
	@echo ""
	@echo "Docker images:"
	@docker images aznetmon || echo "No Docker images found"
	@echo ""
	@echo "Running containers:"
	@docker ps --filter "ancestor=aznetmon" || echo "No containers running"
