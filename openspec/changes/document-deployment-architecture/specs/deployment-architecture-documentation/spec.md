## ADDED Requirements

### Requirement: Explain the development architecture and local deployment paths
The deployment architecture guide SHALL describe the Go service boundaries, Kafka-to-PostgreSQL event flow, and the implemented Docker Compose and Minikube development paths, with links to the source and configuration files that support those descriptions.

#### Scenario: Reader traces the local runtime
- **WHEN** a developer consults the guide to understand local development
- **THEN** the guide explains the HTTP/OCPI, gRPC, and optional OCPP listeners, Kafka and PostgreSQL responsibilities, how Compose starts dependencies and setup, and how Minikube deploys the Helm chart with local credentials and dependencies

### Requirement: Document implemented AWS deployment behavior and prerequisites
The guide SHALL describe only AWS resources and deployment behavior present in repository Terraform, Helm, workflow, and documentation, and SHALL identify prerequisite ownership as Terraform-managed, workflow-managed, or operator-managed.

#### Scenario: Operator prepares AWS staging
- **WHEN** an operator uses the guide to prepare the AWS pilot
- **THEN** the guide lists required account/region, networking/DNS/TLS, IAM/OIDC, EKS add-ons and access, Kafka/database, Secrets Manager/KMS, and GitHub environment setup, including the needed inputs or permission scope and relevant repository file links

#### Scenario: Reader follows the AWS deployment sequence
- **WHEN** a reader follows the AWS sequence diagram
- **THEN** it shows the current GitHub Actions flow from push or manual dispatch through validation, image publication, EKS access, configuration and secret integration, ordered setup Jobs, Helm rollout, and protocol/persistence smoke verification

### Requirement: Separate the proposed Azure architecture from implemented support
The guide SHALL state that Azure deployment is not implemented, SHALL label every Azure architecture and setup item as proposed, and SHALL distinguish operator-managed requirements, future-IaC requirements, and decisions still required.

#### Scenario: Reader evaluates a future Azure deployment
- **WHEN** a reader consults the proposed Azure architecture
- **THEN** the guide shows a separately labeled proposed deployment sequence and candidate AWS-to-Azure service mappings without claiming that Azure resources or workflows exist

#### Scenario: Reader assesses Azure prerequisites and compatibility
- **WHEN** a reader assesses Azure setup requirements
- **THEN** the guide lists proposed identity/RBAC, registry, cluster, network, ingress/TLS, database, Kafka, secrets, DNS, and GitHub setup and clearly flags unresolved choices, including whether the current SCRAM-only Kafka adapter can authenticate to the chosen Kafka service

### Requirement: Cite sources and disclose pilot limitations
The guide SHALL include repository-relative links for implementation claims, official provider documentation links for proposed cloud product behaviors, and a concise account of pilot assumptions, limitations, and documentation gaps.

#### Scenario: Reader validates a claim
- **WHEN** a reader wants to verify an implementation or product claim
- **THEN** the guide links to the relevant source/configuration or official provider documentation and identifies unresolved or contradictory details as open questions rather than asserting unsupported behavior
