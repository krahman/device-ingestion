## Why

Kubernetes is a platform requirement and this project is a pilot for deploying and operating the device-ingestion service on Kubernetes. The current AWS staging change uses ECS, so it does not meet the pilot objective or establish a repeatable Kubernetes deployment path.

## What Changes

- Replace the ECS application runtime and ECS deployment workflow with an AWS EKS pilot deployment.
- Add Kubernetes workload configuration for the service, protocol routing, health probes, secure configuration, and ordered Kafka topic and database migration jobs.
- Keep ECR, MSK, RDS PostgreSQL, the existing application image, and its HTTP, OCPP, and gRPC behavior.
- Document EKS pilot setup, deploy, verification, rollback, and teardown.

## Capabilities

### New Capabilities
- `kubernetes-deployment`: Run and deploy the device-ingestion service on Kubernetes, including its workload, protocol routing, setup jobs, and rollout behavior.

### Modified Capabilities
- `aws-staging-deployment`: Replace ECS-based staging infrastructure and deployment requirements with EKS and Kubernetes while retaining AWS managed Kafka and PostgreSQL dependencies.

## Impact

- Replaces ECS resources and ECS workflow steps in `deploy/aws/staging` and `.github/workflows/deploy-staging.yml`.
- Adds EKS cluster and node capacity, Kubernetes manifests or chart, AWS ingress integration, and Kubernetes deployment workflow configuration.
- Continues to use ECR, MSK, RDS PostgreSQL, Secrets Manager, ACM, and the existing application ports and health endpoints.
- Requires AWS account and region access, a TLS certificate and DNS, GitHub OIDC configuration, and pilot application secrets.
