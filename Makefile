GO ?= go
PROTOC ?= protoc
PROTO_FILE := proto/telemetry.proto
NATS_CONTAINER := device-ingestion-nats

.PHONY: help install-tools install-protoc-macos generate tidy vendor build run nats-up nats-down fmt

help: ## Show available commands
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_-]+:.*##/ {printf "%-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

install-tools: ## Install Go protobuf generators (protoc must be installed separately)
	$(GO) install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
	$(GO) install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

install-protoc-macos: ## Install protoc on macOS with Homebrew
	brew install protobuf

generate: ## Generate Go protobuf and gRPC code from the .proto files
	@command -v $(PROTOC) >/dev/null 2>&1 || { echo "protoc not found; run 'make install-protoc-macos' on macOS"; exit 1; }
	PATH="$$($(GO) env GOPATH)/bin:$$PATH" $(PROTOC) \
		--go_out=. --go_opt=module=device-ingestion \
		--go-grpc_out=. --go-grpc_opt=module=device-ingestion \
		$(PROTO_FILE)

tidy: ## Clean and update Go module requirements
	$(GO) mod tidy

vendor: ## Refresh vendored Go dependencies
	$(GO) mod vendor

build: ## Build all packages using vendored dependencies
	$(GO) build -mod=vendor ./...

run: ## Run the ingestion server (requires NATS at localhost:4222)
	$(GO) run -mod=vendor ./cmd/server

nats-up: ## Start a local NATS server in Docker on port 4222
	docker run -d --name $(NATS_CONTAINER) -p 4222:4222 nats:2

nats-down: ## Stop and remove the local NATS Docker container
	-docker rm -f $(NATS_CONTAINER)

fmt: ## Format project Go source files
	find cmd internal proto -name '*.go' -print0 | xargs -0 gofmt -w
