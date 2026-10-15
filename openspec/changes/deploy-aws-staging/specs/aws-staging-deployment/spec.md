## ADDED Requirements

### Requirement: Repeatable AWS staging infrastructure
The platform SHALL provide Terraform configuration that provisions an isolated AWS staging environment for the service, including ECS Fargate, ECR, an Application Load Balancer, Amazon MSK, RDS for PostgreSQL, private networking, IAM roles, secrets, and logs.

#### Scenario: Plan staging infrastructure
- **WHEN** an operator supplies the documented region, network, certificate, and sizing inputs and runs Terraform plan
- **THEN** Terraform reports the staging resources and changes without requiring application credentials in source control

#### Scenario: Restrict dependency network access
- **WHEN** the staging stack is provisioned
- **THEN** only the load balancer is internet-facing and MSK and RDS accept connections only from authorized application or operations security groups

#### Scenario: Route all supported protocols
- **WHEN** clients connect through the staging load balancer
- **THEN** HTTP and OCPI requests reach port 8080, OCPP WebSocket sessions reach port 8081, and gRPC requests reach port 50051

### Requirement: Secure cloud dependency connections
The service SHALL support TLS and configured SASL/SCRAM authentication for Kafka producers, consumers, and dead-letter writers, and SHALL support TLS-verified PostgreSQL connections in the staging environment.

#### Scenario: Connect to authenticated MSK
- **WHEN** ECS receives the configured broker addresses and SCRAM credentials from Secrets Manager
- **THEN** all Kafka publish, consume, and dead-letter paths connect over TLS using those credentials

#### Scenario: Connect to RDS with TLS
- **WHEN** ECS receives a TLS-enabled PostgreSQL connection URL and required trust configuration
- **THEN** database operations use an encrypted connection and reject invalid server certificates

#### Scenario: Keep credentials out of deployment artifacts
- **WHEN** Terraform provisions the environment or GitHub Actions deploys an image
- **THEN** passwords, OCPI tokens, and station credentials are not written to the repository, image, workflow logs, or Terraform state as plaintext outputs

### Requirement: Probe service health
The service SHALL expose distinct liveness and readiness endpoints suitable for ECS and load balancer health checks.

#### Scenario: Process liveness
- **WHEN** the service process is running and able to handle requests
- **THEN** the liveness endpoint returns a successful health response

#### Scenario: Readiness after initialization
- **WHEN** the service has initialized its required listeners and dependencies
- **THEN** the readiness endpoint returns success and the load balancer may route traffic to the task

#### Scenario: Unready service is removed from routing
- **WHEN** the service is not ready to accept traffic
- **THEN** the readiness endpoint fails and the load balancer marks the target unhealthy

### Requirement: Ordered GitHub Actions staging deployment
The repository SHALL provide a GitHub Actions workflow that validates the code, publishes an immutable image, runs migrations as a one-off ECS task, deploys the image to staging, and verifies service stability using GitHub OIDC for AWS access.

#### Scenario: Build and deploy from main
- **WHEN** the deployment workflow runs for an authorized push to `main` or manual staging dispatch
- **THEN** it runs the configured Go checks, builds and publishes an image tagged with the source commit SHA, and assumes the staging AWS role through OIDC without static AWS access keys

#### Scenario: Migrate before application rollout
- **WHEN** the new image is ready for deployment
- **THEN** the workflow runs the migration binary as a one-off ECS task and updates the service only after that task succeeds

#### Scenario: Stop on migration failure
- **WHEN** the migration task exits unsuccessfully
- **THEN** the workflow fails and does not update the ECS service to the new image

#### Scenario: Verify deployment
- **WHEN** the service update completes
- **THEN** the workflow waits for ECS stability and runs the documented HTTP, gRPC, OCPP, and OCPI staging smoke checks

### Requirement: Staging operations are documented
The repository SHALL document staging prerequisites, Terraform operations, GitHub configuration, deployment behavior, protocol smoke checks, rollback, and teardown.

#### Scenario: Prepare AWS staging
- **WHEN** an operator follows the staging documentation
- **THEN** they can identify required AWS, GitHub, network, certificate, and secret configuration before provisioning

#### Scenario: Remove staging environment
- **WHEN** an operator intentionally runs the documented teardown procedure
- **THEN** Terraform destroys the staging resources and the documentation identifies which data is removed or retained
