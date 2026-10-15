## MODIFIED Requirements

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

### Requirement: Ordered GitHub Actions staging deployment
The repository SHALL provide a GitHub Actions workflow that validates the code, publishes an immutable image, runs Kafka topic initialization and database migrations as Kubernetes Jobs, deploys the image to EKS, and verifies service readiness using GitHub OIDC for AWS access.

#### Scenario: Build and deploy from main
- **WHEN** the deployment workflow runs for an authorized push to `main` or manual staging dispatch
- **THEN** it runs the configured Go checks, builds and publishes an image tagged with the source commit SHA, and assumes the staging AWS role through OIDC without static AWS access keys

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
