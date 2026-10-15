## Why

The service currently runs locally through Docker Compose, but the repository has no repeatable AWS environment or deployment workflow. A staging deployment will validate the OCPP WebSocket, OCPI, HTTP/gRPC, Kafka, and PostgreSQL paths in a cloud environment before production planning.

## What Changes

- Add Terraform infrastructure for a staging environment using ECS Fargate, an Application Load Balancer, ECR, Amazon MSK, and RDS for PostgreSQL.
- Make the service's Kafka connections support authenticated TLS connections to MSK and add health endpoints for deployment probes.
- Add a GitHub Actions workflow that uses OIDC, runs Go checks, builds and pushes an immutable container image, runs database migrations as a one-off ECS task, deploys the ECS service, and verifies deployment health.
- Document staging prerequisites, configuration, deployment, smoke checks, and teardown.

## Capabilities

### New Capabilities
- `aws-staging-deployment`: Provision and deploy the device-ingestion service to a repeatable AWS staging environment through infrastructure as code and GitHub Actions.

### Modified Capabilities

## Impact

- Adds Terraform files and a GitHub Actions deployment workflow.
- Updates Kafka configuration, HTTP health endpoints, and deployment documentation.
- Uses AWS ECS, ECR, Application Load Balancer, MSK, RDS for PostgreSQL, Secrets Manager, CloudWatch, and GitHub Actions OIDC.
- Requires AWS account, region, VPC/network, TLS certificate, and GitHub OIDC role configuration to be supplied when staging is provisioned.
