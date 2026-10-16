## ADDED Requirements

### Requirement: Provide an explicit dependency-free sandbox profile

The service SHALL provide a clearly named `sandbox` mode that starts without PostgreSQL or Kafka, publishers, consumers, database migrations, OCPP, pprof, or database-backed metrics. The only HTTP routes served in sandbox mode SHALL be `/health/live`, `/health/ready`, and `/health/dependencies`; the only gRPC capability SHALL be the gRPC health service. The default/full runtime used for local development and existing deployments SHALL retain its current behavior.

#### Scenario: Start the sandbox runtime
- **WHEN** the sandbox profile is selected
- **THEN** the process starts without attempting PostgreSQL/Kafka connectivity and emits structured startup logs describing the limited mode without secrets

#### Scenario: Start the normal runtime
- **WHEN** the sandbox profile is not selected
- **THEN** the existing full dependency-backed application composition remains available for Docker Compose and existing deployment environments

### Requirement: Report truthful health and unsupported route behavior

`/health/live` SHALL report process liveness only. In the full runtime, `/health/ready` SHALL report dependency readiness for PostgreSQL and Kafka. In the explicitly named `sandbox` configuration, readiness SHALL describe only the enabled sandbox capabilities and SHALL expose that PostgreSQL/Kafka-dependent functionality is disabled through a distinct dependency-readiness status. Kubernetes probes SHALL use the sandbox readiness behavior without falsely reporting that ingestion dependencies are available. Routes requiring PostgreSQL or Kafka SHALL be disabled or return an explicit unavailable response before side effects.

#### Scenario: Check sandbox readiness
- **WHEN** Kubernetes calls the readiness endpoint
- **THEN** readiness reflects the limited sandbox service and a distinct dependency status reports Kafka/PostgreSQL-dependent capabilities as disabled/unavailable

#### Scenario: Invoke a dependency-backed route
- **WHEN** a client invokes a route requiring PostgreSQL or Kafka in sandbox mode
- **THEN** HTTP returns an explicit unavailable response such as 503, gRPC returns `Unavailable`, no event is published or persisted, and no success response is returned

#### Scenario: Verify disabled dependencies cannot drop data silently
- **WHEN** automated tests invoke dependency-backed HTTP/gRPC routes in sandbox mode
- **THEN** tests assert the unavailable status and assert that no publisher, repository write, or success response was called/returned

#### Scenario: Check gRPC health in sandbox mode
- **WHEN** a client checks gRPC health for the ingestion service
- **THEN** the health status does not claim the ingestion service is serving; process-level health remains distinguishable from the disabled ingestion capability

### Requirement: Preserve bounded shutdown, logging, and request handling

Both runtime profiles SHALL retain SIGTERM/SIGINT handling, bounded listener shutdown, structured redacted logs, and applicable request validation. Any listener unused by the sandbox profile SHALL not be opened.

#### Scenario: Terminate sandbox Pod
- **WHEN** Kubernetes terminates the sandbox Pod
- **THEN** the process exits within its configured grace period and emits a structured shutdown outcome

#### Scenario: Inspect runtime logs
- **WHEN** the sandbox handles a health request or shuts down
- **THEN** logs remain useful through `kubectl logs` and exclude credentials and unrestricted payload contents
