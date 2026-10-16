# aws-staging-deployment Specification

## Purpose
TBD - created by archiving change deploy-aws-staging. Update Purpose after archive.

## Requirements
### Requirement: Repeatable AWS staging infrastructure
The platform SHALL provide Terraform configuration that provisions an isolated AWS staging environment for the service, including an EKS cluster and worker capacity, ECR, TLS-enabled ingress, Amazon MSK, RDS for PostgreSQL, private networking, workload identity, secrets, and logs.

#### Scenario: Plan staging infrastructure
- **WHEN** an operator supplies the documented region, network, certificate, cluster sizing, and dependency inputs and runs Terraform plan
- **THEN** Terraform reports the staging resources and changes without requiring application credentials in source control

#### Scenario: Restrict dependency network access
- **WHEN** the staging stack is provisioned
- **THEN** only the TLS ingress is internet-facing and MSK and RDS accept connections only from authorized Kubernetes workload security groups

#### Scenario: Route all supported protocols
- **WHEN** clients connect through the staging ingress
- **THEN** HTTP and OCPI requests reach port 8080, configured OCPP WebSocket sessions reach port 8081, and gRPC requests reach port 50051

### Requirement: Secure cloud dependency connections
The service SHALL support TLS and configured SASL/SCRAM authentication for Kafka producers, consumers, and dead-letter writers, and SHALL support TLS-verified PostgreSQL connections in the staging environment.

#### Scenario: Connect to authenticated MSK
- **WHEN** the Kubernetes workload receives the configured broker addresses and SCRAM credentials from Secrets Manager
- **THEN** all Kafka publish, consume, and dead-letter paths connect over TLS using those credentials

#### Scenario: Connect to RDS with TLS
- **WHEN** the Kubernetes workload receives a TLS-enabled PostgreSQL connection URL and required trust configuration
- **THEN** database operations use an encrypted connection and reject invalid server certificates

#### Scenario: Keep credentials out of deployment artifacts
- **WHEN** Terraform provisions the environment or GitHub Actions deploys an image
- **THEN** passwords, OCPI tokens, and station credentials are not written to the repository, image, workflow logs, or Terraform state as plaintext outputs

### Requirement: Probe service health
The service SHALL expose distinct liveness and readiness endpoints suitable for Kubernetes probes and ingress health checks.

#### Scenario: Process liveness
- **WHEN** the service process is running and able to handle requests
- **THEN** the liveness endpoint returns a successful health response

#### Scenario: Readiness after initialization
- **WHEN** the service has initialized its required listeners and dependencies
- **THEN** the readiness endpoint returns success and Kubernetes may route traffic to the pod

#### Scenario: Unready service is removed from routing
- **WHEN** the service is not ready to accept traffic
- **THEN** the readiness endpoint fails and Kubernetes removes the pod from ready Service endpoints

### Requirement: Ordered GitHub Actions staging deployment
The repository SHALL provide a GitHub Actions workflow that validates the code, publishes an immutable image, runs Kafka topic initialization and database migrations as Kubernetes Jobs, deploys the image to EKS, and verifies service readiness using GitHub OIDC for AWS access.

#### Scenario: Build and deploy after explicit manual confirmation
- **WHEN** an operator manually dispatches the staging workflow and confirms the staging deployment input
- **THEN** it runs the configured Go checks, builds and publishes an image tagged with the source commit SHA, and assumes the staging AWS role through OIDC without static AWS access keys

#### Scenario: Pushes do not access AWS
- **WHEN** a commit is pushed to any branch, including `main`
- **THEN** CI validation may run, but no AWS credentials are requested, no image is published, and no Kubernetes deployment is started

#### Scenario: Initialize dependencies before application rollout
- **WHEN** the candidate image is ready for deployment
- **THEN** the workflow runs Kafka topic initialization and migration Jobs and updates the Kubernetes release only after both Jobs succeed

#### Scenario: Stop on setup failure
- **WHEN** topic initialization or the migration Job exits unsuccessfully
- **THEN** the workflow fails and does not update the application Deployment to the new image

#### Scenario: Verify deployment
- **WHEN** the Kubernetes rollout completes
- **THEN** the workflow waits for Deployment readiness and runs the documented HTTP, gRPC, OCPP, and OCPI staging smoke checks

### Requirement: Staging operations are documented
The repository SHALL document EKS staging prerequisites, Terraform operations, GitHub configuration, Kubernetes deployment behavior, protocol smoke checks, rollback, and teardown.

#### Scenario: Prepare AWS staging
- **WHEN** an operator follows the staging documentation
- **THEN** they can identify required AWS, GitHub, cluster, network, certificate, and secret configuration before provisioning

#### Scenario: Remove staging environment
- **WHEN** an operator intentionally runs the documented teardown procedure
- **THEN** Terraform destroys the EKS pilot and staging resources and the documentation identifies which data is removed or retained

### Requirement: Validate pull requests without deployment credentials
The repository SHALL run the configured source, Terraform, and Helm validation checks for pull requests without granting the pull request workflow AWS deployment credentials or Kubernetes access and without publishing or deploying an image.

#### Scenario: Pull request validation succeeds
- **WHEN** a pull request is opened or updated
- **THEN** the configured validation checks run and report a status usable by repository branch protection

#### Scenario: Pull request validation cannot deploy
- **WHEN** validation runs for a pull request event
- **THEN** the run does not assume an AWS role, publish to ECR, modify an EKS cluster, or deploy to staging

### Requirement: Provide bounded diagnostics for staging deployment failures
The staging deployment workflow SHALL emit bounded Kubernetes diagnostics for application rollout and smoke-check failures without printing Kubernetes Secret objects or secret values.

#### Scenario: Application rollout fails
- **WHEN** the Helm upgrade or application Deployment rollout fails
- **THEN** the workflow reports relevant Deployment and Pod status, selected pod descriptions and logs, and recent namespace events before failing

#### Scenario: Smoke Job fails
- **WHEN** the staging smoke Job fails
- **THEN** the workflow reports that Job's status, pod description and logs, and relevant recent namespace events before failing

#### Scenario: Diagnostics avoid secret disclosure
- **WHEN** the workflow collects failure diagnostics
- **THEN** it does not dump Kubernetes Secret objects, secret-bearing rendered manifests, or unrestricted environment values
