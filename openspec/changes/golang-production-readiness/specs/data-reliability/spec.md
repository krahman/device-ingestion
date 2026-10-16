## ADDED Requirements

### Requirement: Preserve the complete local development dependency path

Local full-system development SHALL continue to use Docker Compose for PostgreSQL and Kafka, with migrations and the normal Go runtime. The EKS initial sandbox SHALL NOT provision or deploy PostgreSQL or Kafka.

#### Scenario: Developer runs the full local service
- **WHEN** the developer uses the documented local Compose workflow
- **THEN** PostgreSQL and Kafka are available locally and the full service runtime can be exercised

#### Scenario: EKS sandbox starts without data dependencies
- **WHEN** the stateless sandbox profile runs in EKS
- **THEN** it does not attempt to connect to PostgreSQL/Kafka, start workers, or run migrations

### Requirement: Make unsupported data operations explicit in sandbox mode

In the initial EKS stage, database and Kafka-dependent ingestion routes SHALL be disabled or return a clear unavailable response before side effects. Health readiness SHALL not imply that data persistence or event processing is available.

#### Scenario: Data endpoint is invoked in sandbox mode
- **WHEN** a client sends telemetry or another operation requiring PostgreSQL/Kafka
- **THEN** the response indicates the sandbox capability is unavailable and no success response, event publication, or persistence claim occurs

### Requirement: Keep data infrastructure decisions separate

The initial Terraform/Kubernetes sandbox SHALL NOT create managed or in-cluster data services. Any later Kafka or PostgreSQL learning exercise SHALL be proposed separately with network, data lifecycle, cost, and teardown implications.

#### Scenario: Managed or in-cluster data service is proposed
- **WHEN** a later change proposes RDS, MSK, or a Kubernetes data workload
- **THEN** it receives its own architecture/cost review and explicit approval before implementation or provisioning
