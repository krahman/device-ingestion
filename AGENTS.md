# Repository Guidelines

## Project Structure & Module Organization

This is a Go module (`device-ingestion`). Executables live in `cmd/server`, `cmd/migrate`, and `cmd/reconcile`; `app` wires configuration and adapters. Core packages are under `internal`: `domain` defines telemetry, `application` contains use cases and ports, and `adapters` implements HTTP, gRPC, Kafka, and PostgreSQL. `proto/telemetry.proto` is the gRPC source; its generated Go files are checked in. SQL migrations are in `internal/adapters/persistence/postgres/migrations`. Third-party dependencies are vendored in `vendor/`; avoid editing vendored code for service changes.

## Build, Test, and Development Commands

- `make build` compiles all packages using vendored dependencies.
- `make run` starts the service (Kafka must be available at `localhost:9092`).
- `make kafka-up`, `make kafka-topics`, and `make postgres-up` start local dependencies and create Kafka topics; `make migrate` applies database migrations.
- `go test ./...` runs Go package tests. Run it after changes that affect behavior.
- `make fmt` applies `gofmt` to project Go sources.
- `make generate` regenerates protobuf and gRPC code after editing `proto/telemetry.proto` (requires `protoc` and the Go generators).
- `make reconcile` replays retained Kafka records into PostgreSQL.

## Coding Style & Naming Conventions

Use standard Go formatting (`gofmt`), tabs for Go indentation, short lower-case package names, and exported identifiers in `PascalCase`; unexported identifiers use `camelCase`. Keep domain and application packages independent of transport and storage details. Put new adapters under `internal/adapters` and connect them through application ports in the composition root (`app`). Keep migrations sequentially numbered and descriptive, for example `0003_add_telemetry_field.sql`.

## Testing Guidelines

Place Go tests beside the package code in `*_test.go` files and name tests `TestBehavior`. Prefer focused unit tests for domain and application behavior; use integration tests when validating Kafka or PostgreSQL adapter behavior. The repository currently has no dedicated test target, so run `go test ./...` directly.

## Commit & Pull Request Guidelines

Recent commits use short, direct, imperative summaries (for example, “replace NATS with Kafka”). Follow that style and keep each commit focused. Pull requests should explain the behavior or architecture change, list validation commands and results, link related issues when applicable, and include request/response examples for API changes. Call out any migration or configuration changes.

## Configuration & Local Safety

Use environment variables for runtime configuration; do not commit secrets. Local Kafka and PostgreSQL containers use development settings from the Makefile. Keep pprof bound to loopback by default and restrict access if changing its listener address.
