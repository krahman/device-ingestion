GO ?= go
PROTOC ?= protoc
PROTO_FILE := proto/telemetry.proto
KAFKA_CONTAINER := device-ingestion-kafka
KAFKA_IMAGE ?= apache/kafka:4.3.1
POSTGRES_CONTAINER := device-ingestion-postgres
POSTGRES_IMAGE ?= postgres:17-alpine
RECONCILE_WORKERS ?= 2

.PHONY: help install-tools install-protoc-macos generate tidy vendor build run kafka-up kafka-down kafka-topics postgres-up postgres-down migrate reconcile fmt

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

run: ## Run with Kafka at localhost:9092
	$(GO) run -mod=vendor ./cmd/server

kafka-up: ## Start a local single-node Kafka broker in Docker
	docker run -d --name $(KAFKA_CONTAINER) -p 9092:9092 $(KAFKA_IMAGE)

kafka-down: ## Stop and remove the local Kafka container
	-docker rm -f $(KAFKA_CONTAINER)

kafka-topics: ## Create local telemetry and dead-letter topics
	docker exec $(KAFKA_CONTAINER) /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --create --if-not-exists --topic device.telemetry --partitions 4 --replication-factor 1
	docker exec $(KAFKA_CONTAINER) /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 --create --if-not-exists --topic device.telemetry.dlq --partitions 1 --replication-factor 1

postgres-up: ## Start a local PostgreSQL database in Docker
	docker run -d --name $(POSTGRES_CONTAINER) -p 5432:5432 -e POSTGRES_USER=device_ingestion -e POSTGRES_PASSWORD=device_ingestion -e POSTGRES_DB=device_ingestion -v device-ingestion-postgres-data:/var/lib/postgresql/data $(POSTGRES_IMAGE)

postgres-down: ## Stop and remove the local PostgreSQL container
	-docker rm -f $(POSTGRES_CONTAINER)

migrate: ## Apply PostgreSQL schema migrations
	$(GO) run -mod=vendor ./cmd/migrate

reconcile: ## Replay retained Kafka telemetry into PostgreSQL
	$(GO) run -mod=vendor ./cmd/reconcile -workers=$(RECONCILE_WORKERS)

fmt: ## Format project Go source files
	find app cmd internal proto -name '*.go' -print0 | xargs -0 gofmt -w
