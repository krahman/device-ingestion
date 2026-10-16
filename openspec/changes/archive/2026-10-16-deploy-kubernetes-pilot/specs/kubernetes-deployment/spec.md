## ADDED Requirements

### Requirement: Run the service as a Kubernetes workload
The staging platform SHALL run the existing device-ingestion server image as a Kubernetes Deployment with explicit resource requests and limits, a dedicated service account, and configurable immutable image reference.

#### Scenario: Start the pilot workload
- **WHEN** an operator deploys the staging release with a valid image and configuration
- **THEN** Kubernetes starts the configured application replica and marks it ready only after its readiness probe succeeds

#### Scenario: OCPP listener is optional
- **WHEN** no OCPP station configuration is supplied
- **THEN** the workload does not publish the OCPP service port as an active ingress backend

### Requirement: Route supported protocols through Kubernetes ingress
The platform SHALL route HTTP and OCPI traffic to port 8080, OCPP WebSocket traffic to port 8081 when configured, and gRPC traffic to port 50051 through TLS-enabled AWS ingress.

#### Scenario: Route HTTP and OCPI
- **WHEN** a client sends HTTPS traffic to the staging API hostname
- **THEN** the ingress forwards HTTP and OCPI requests to the service on port 8080

#### Scenario: Route OCPP WebSockets
- **WHEN** a configured charger opens an OCPP WebSocket connection on the OCPP hostname
- **THEN** the ingress upgrades and forwards the connection to port 8081 while applying an idle timeout above the configured heartbeat interval

#### Scenario: Route gRPC
- **WHEN** a gRPC client connects to the gRPC hostname using TLS
- **THEN** the ingress forwards gRPC traffic to port 50051 using gRPC-compatible backend settings

### Requirement: Configure Kubernetes health and graceful termination
The Deployment SHALL use the service liveness and readiness endpoints and allow the application to shut down gracefully during pod replacement.

#### Scenario: Unready pod receives no new traffic
- **WHEN** the application readiness endpoint reports failure
- **THEN** Kubernetes removes the pod from ready Service endpoints

#### Scenario: Pod receives termination signal
- **WHEN** Kubernetes replaces or stops an application pod
- **THEN** the pod is given a termination grace period longer than the application's configured shutdown limit

### Requirement: Load secrets from AWS Secrets Manager
The application workload SHALL receive Kafka, database, station, and OCPI partner secrets from AWS Secrets Manager through a workload identity and the configured Kubernetes secret integration.

#### Scenario: Start with required credentials
- **WHEN** the pod starts with authorized secret references
- **THEN** the existing application environment variables receive the configured credentials without storing secret values in the image or Helm values

#### Scenario: Deny unrelated secret access
- **WHEN** the application service account requests a secret outside its assigned staging secret set
- **THEN** AWS denies the request

### Requirement: Run staging setup and migrations before rollout
The deployment process SHALL run Kafka topic initialization and PostgreSQL migrations as Kubernetes Jobs using the candidate immutable image before updating the application Deployment.

#### Scenario: Setup jobs succeed
- **WHEN** topic initialization and migration Jobs complete successfully
- **THEN** the deployment process updates the application workload to the candidate image

#### Scenario: Setup job fails
- **WHEN** either setup Job exits unsuccessfully
- **THEN** the deployment process fails and leaves the application Deployment on its previous image

### Requirement: Deploy and verify the Kubernetes release
The repository SHALL provide an automated deployment workflow that authenticates to AWS using GitHub OIDC, publishes an immutable image, updates the Kubernetes release, waits for readiness, and runs protocol smoke checks.

#### Scenario: Deploy an authorized revision
- **WHEN** an authorized main-branch or manual staging deployment runs
- **THEN** the workflow uses short-lived AWS credentials, publishes a commit-tagged image, and deploys that image to the staging namespace

#### Scenario: Verify the released service
- **WHEN** the Kubernetes rollout becomes ready
- **THEN** the workflow verifies HTTP, gRPC, OCPP, OCPI, Kafka delivery, and PostgreSQL persistence and fails if any required check fails

### Requirement: Validate the application chart locally
The repository SHALL provide a Minikube profile that deploys the application Helm chart with local Kafka and PostgreSQL dependencies and runs the protocol and persistence smoke checks without AWS integrations.

#### Scenario: Run the local Kubernetes pilot
- **WHEN** an operator starts the documented Minikube profile
- **THEN** the chart deploys with local credentials and dependencies, runs topic initialization and migrations, and passes HTTP, gRPC, OCPP, OCPI, Kafka, and PostgreSQL smoke checks
