## 1. Prepare application for AWS dependencies

- [x] 1.1 Add configurable TLS and SASL/SCRAM settings to all Kafka publishers, consumers, and DLQ writers while preserving local Compose defaults.
- [x] 1.2 Add TLS verification configuration for PostgreSQL and document the cloud connection settings.
- [x] 1.3 Add liveness and readiness endpoints and make readiness reflect successful application initialization.
- [x] 1.4 Add focused tests for Kafka security configuration, database TLS configuration, and health endpoint behavior.

## 2. Provision AWS staging infrastructure

- [x] 2.1 Add Terraform configuration and documented variables for the staging VPC, private subnets, security groups, and outputs.
- [x] 2.2 Provision ECR, ECS Fargate cluster/service/task definition, CloudWatch logs, and least-privilege task and execution roles.
- [x] 2.3 Provision the HTTPS ALB, ACM certificate input, and HTTP/OCPI, OCPP WebSocket, and gRPC target groups and routing.
- [x] 2.4 Provision private MSK with TLS/SASL/SCRAM, required topics, and Secrets Manager credentials.
- [x] 2.5 Provision private TLS-enabled RDS PostgreSQL with staging backup settings and a Secrets Manager database URL.
- [x] 2.6 Configure ALB and ECS health checks, OCPP idle timeout, and task draining settings.

## 3. Automate GitHub Actions deployment

- [x] 3.1 Document and configure the AWS GitHub OIDC trust role scoped to this repository and the staging environment.
- [x] 3.2 Add a GitHub Actions workflow for pushes to `main` and manual staging dispatch with Go checks and immutable SHA-tagged image publishing.
- [x] 3.3 Add workflow steps to run migrations as a one-off ECS task, stop on migration failure, deploy the task definition, and wait for ECS stability.
- [x] 3.4 Add post-deployment HTTP, gRPC, OCPP, and OCPI smoke checks and ensure failures mark the workflow unsuccessful.

## 4. Document and validate staging operations

- [x] 4.1 Document prerequisites, Terraform plan/apply/destroy, GitHub environment settings, required secrets, deployment, smoke checks, and rollback.
- [x] 4.2 Validate Terraform formatting and plan using staging-like inputs without applying cloud resources.
- [x] 4.3 Validate the GitHub workflow structure and ensure no long-lived AWS credentials or application secrets are committed.
- [x] 4.4 Run the project checks and local Compose smoke checks to confirm cloud configuration preserves local development behavior.
- [ ] 4.5 Deploy to an authorized AWS staging account and verify HTTP, gRPC, OCPP, OCPI, Kafka delivery, and PostgreSQL persistence end to end.

Task 4.5 requires an authorized AWS account, AWS CLI credentials, and real
certificate, domain, and Secrets Manager ARNs. These are not configured in
this workspace; no AWS resources have been applied.
