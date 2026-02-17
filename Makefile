# PicoTunnel Makefile

.PHONY: build clean test vet fmt dev-server dev-client docker-build docker-push release

# Build variables
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"
GO_FLAGS := -v
CGO_ENABLED_SERVER := 1
CGO_ENABLED_CLIENT := 0

# Build targets
build: build-server build-client

build-server:
	@echo "Building server..."
	CGO_ENABLED=$(CGO_ENABLED_SERVER) go build $(GO_FLAGS) $(LDFLAGS) -o bin/picotunnel-server ./cmd/server

build-client:
	@echo "Building client..."
	CGO_ENABLED=$(CGO_ENABLED_CLIENT) go build $(GO_FLAGS) $(LDFLAGS) -o bin/picotunnel-client ./cmd/client

# Development targets
dev-server: build-server
	@echo "Starting development server..."
	./bin/picotunnel-server \
		--listen :8080 \
		--tunnel :8443 \
		--http :8080 \
		--https "" \
		--data ./dev-data \
		--domain localhost

dev-client: build-client
	@echo "Starting development client..."
	@echo "Usage: ./bin/picotunnel-client --server localhost:8443 --token <your-token> --insecure"
	
# Testing
test:
	go test -v ./...

vet:
	go vet ./...

fmt:
	go fmt ./...

# Docker builds
docker-build: docker-build-server docker-build-client

docker-build-server:
	@echo "Building server Docker image..."
	docker build -f Dockerfile.server -t picotunnel-server:$(VERSION) .
	docker tag picotunnel-server:$(VERSION) picotunnel-server:latest

docker-build-client:
	@echo "Building client Docker image..."
	docker build -f Dockerfile.client -t picotunnel-client:$(VERSION) .
	docker tag picotunnel-client:$(VERSION) picotunnel-client:latest

# GitHub Container Registry push
docker-push: docker-build
	@echo "Pushing Docker images to GHCR..."
	docker tag picotunnel-server:$(VERSION) ghcr.io/jclement/picotunnel-server:$(VERSION)
	docker tag picotunnel-server:$(VERSION) ghcr.io/jclement/picotunnel-server:latest
	docker tag picotunnel-client:$(VERSION) ghcr.io/jclement/picotunnel-client:$(VERSION)
	docker tag picotunnel-client:$(VERSION) ghcr.io/jclement/picotunnel-client:latest
	docker push ghcr.io/jclement/picotunnel-server:$(VERSION)
	docker push ghcr.io/jclement/picotunnel-server:latest
	docker push ghcr.io/jclement/picotunnel-client:$(VERSION)
	docker push ghcr.io/jclement/picotunnel-client:latest

# Release build (cross-platform)
release:
	@echo "Building release binaries..."
	@mkdir -p dist
	# Linux amd64
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/picotunnel-server-linux-amd64 ./cmd/server
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/picotunnel-client-linux-amd64 ./cmd/client
	# Linux arm64
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=aarch64-linux-gnu-gcc go build $(LDFLAGS) -o dist/picotunnel-server-linux-arm64 ./cmd/server || echo "Skip arm64 server (requires cross-compiler)"
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/picotunnel-client-linux-arm64 ./cmd/client
	# macOS
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/picotunnel-server-darwin-amd64 ./cmd/server || echo "Skip macOS server (requires CGO)"
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/picotunnel-client-darwin-amd64 ./cmd/client
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/picotunnel-client-darwin-arm64 ./cmd/client
	# Windows
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/picotunnel-client-windows-amd64.exe ./cmd/client

# Cleanup
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/ dist/ dev-data/
	go clean -cache

# Setup development environment
setup:
	@echo "Setting up development environment..."
	@mkdir -p bin dev-data
	go mod tidy
	go mod verify

# Show help
help:
	@echo "Available targets:"
	@echo "  build          - Build both server and client"
	@echo "  build-server   - Build server only"
	@echo "  build-client   - Build client only"
	@echo "  dev-server     - Run development server"
	@echo "  dev-client     - Show development client usage"
	@echo "  test           - Run tests"
	@echo "  vet            - Run go vet"
	@echo "  fmt            - Format code"
	@echo "  docker-build   - Build Docker images"
	@echo "  docker-push    - Push Docker images to GHCR"
	@echo "  release        - Build cross-platform release binaries"
	@echo "  clean          - Clean build artifacts"
	@echo "  setup          - Setup development environment"
	@echo "  help           - Show this help"

# Default target
.DEFAULT_GOAL := help