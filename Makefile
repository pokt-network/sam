.PHONY: help install build run dev clean test deps check-pocketd

# Default target
help:
	@echo "SAM - Simple AppStakes Manager"
	@echo ""
	@echo "Available commands:"
	@echo "  make install       - Install Go dependencies"
	@echo "  make build        - Build the SAM binary"
	@echo "  make run          - Build and run SAM server"
	@echo "  make dev          - Run in development mode with auto-reload"
	@echo "  make clean        - Clean build artifacts"
	@echo "  make test         - Run tests"
	@echo "  make deps         - Check and install dependencies"
	@echo "  make check-pocketd - Verify pocketd is installed"
	@echo ""

# Install Go dependencies
install:
	@echo "Installing Go dependencies..."
	go mod download
	go mod tidy
	@echo "Dependencies installed successfully!"

# Build the binary
VERSION ?= $(shell cat VERSION 2>/dev/null || echo dev)
build:
	@echo "Building SAM..."
	go build -ldflags="-s -w -X main.version=$(VERSION)" -o sam ./cmd/web/
	@echo "Build complete! Binary: ./sam"

# Run the server
run: build check-pocketd
	@echo "Starting SAM server..."
	./sam

# Development mode (requires air for hot reload)
dev:
	@if command -v air > /dev/null; then \
		echo "Starting in development mode with hot reload..."; \
		air; \
	else \
		echo "Installing air for hot reload..."; \
		go install github.com/cosmtrek/air@latest; \
		echo "Starting in development mode..."; \
		air; \
	fi

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -f sam
	rm -f tmp/
	@echo "Clean complete!"

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Check and install dependencies
deps:
	@echo "Checking dependencies..."
	@command -v go >/dev/null 2>&1 || { echo "Error: Go is not installed. Please install Go 1.19+"; exit 1; }
	@echo "Go version: $$(go version)"
	@echo ""
	@echo "Checking required Go packages..."
	@go list -m github.com/gorilla/mux >/dev/null 2>&1 || { echo "Installing github.com/gorilla/mux..."; go get github.com/gorilla/mux; }
	@go list -m github.com/rs/cors >/dev/null 2>&1 || { echo "Installing github.com/rs/cors..."; go get github.com/rs/cors; }
	@go list -m gopkg.in/yaml.v3 >/dev/null 2>&1 || { echo "Installing gopkg.in/yaml.v3..."; go get gopkg.in/yaml.v3; }
	@echo "All dependencies are installed!"

# Check if pocketd is available
check-pocketd:
	@echo "Checking for pocketd..."
	@if command -v pocketd >/dev/null 2>&1; then \
		echo "✓ pocketd found at: $$(which pocketd)"; \
		pocketd version 2>/dev/null || echo "  (version command not available)"; \
	else \
		echo "⚠ WARNING: pocketd not found in PATH"; \
		echo "  Transactions (upstake/fund) will not work without pocketd"; \
		echo "  Install pocketd and ensure it's in your PATH"; \
	fi
	@echo ""

# Initialize configuration
init-config:
	@if [ ! -f config.yaml ]; then \
		echo "Creating sample config.yaml..."; \
		echo "config:" > config.yaml; \
		echo "  keyring-backend: test" >> config.yaml; \
		echo "  pocketd-home: ~/.pocket" >> config.yaml; \
		echo "  thresholds:" >> config.yaml; \
		echo "    warning_threshold: 2000000000" >> config.yaml; \
		echo "    danger_threshold: 1000000000" >> config.yaml; \
		echo "  networks:" >> config.yaml; \
		echo "    pocket:" >> config.yaml; \
		echo "      rpc_endpoint: https://rpc.example.com:443" >> config.yaml; \
		echo "      api_endpoint: https://api.example.com" >> config.yaml; \
		echo "      gateways:" >> config.yaml; \
		echo "        - gateway_address_1" >> config.yaml; \
		echo "      bank: bank_address" >> config.yaml; \
		echo "      applications:" >> config.yaml; \
		echo "        - app_address_1" >> config.yaml; \
		echo "Sample config.yaml created! Please edit it with your values."; \
	else \
		echo "config.yaml already exists!"; \
	fi

# Setup project
setup: deps init-config
	@echo "Creating web directory..."
	@mkdir -p web
	@if [ ! -f web/index.html ]; then \
		echo "⚠ Please add index.html to the web/ directory"; \
	fi
	@echo ""
	@echo "Setup complete! Next steps:"
	@echo "  1. Edit config.yaml with your network settings"
	@echo "  2. Ensure pocketd is installed and in your PATH"
	@echo "  3. Run 'make run' to start the server"

# Quick start (deps + build + run)
start: deps build check-pocketd
	@echo ""
	@echo "Starting SAM server..."
	./sam