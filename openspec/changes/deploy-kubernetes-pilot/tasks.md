## 1. Replace ECS staging runtime with EKS

- [x] 1.1 Update Terraform to provision the EKS pilot cluster, private worker capacity, workload identity, and cluster access while retaining ECR, MSK, RDS, and required networking.
- [x] 1.2 Remove ECS cluster, service, task definitions, ECS IAM roles, and ECS-specific outputs and documentation.
- [x] 1.3 Document the AWS Load Balancer Controller and Secrets Store CSI integration required by the pilot.

## 2. Package the Kubernetes application

- [x] 2.1 Add a Helm chart for the Deployment, namespace-scoped service account, Service ports, resource settings, ConfigMap/Secret references, and HTTP liveness/readiness probes.
- [x] 2.2 Add TLS ingress routing for HTTP/OCPI, OCPP WebSockets, and gRPC, including OCPP idle timeout and conditional OCPP exposure.
- [x] 2.3 Add Kubernetes Jobs or deployment workflow steps for Kafka topic initialization and PostgreSQL migrations using the same immutable image as the app.
- [x] 2.4 Configure workload identity permissions to read only the required AWS Secrets Manager values and project them into the app environment.
- [x] 2.5 Set pilot replica count, rollout behavior, termination grace period, and documented resource defaults.

## 3. Automate deployment and verification

- [x] 3.1 Replace ECS deployment steps with GitHub OIDC authentication, ECR image publishing, Kubernetes credentials, and Helm release deployment.
- [x] 3.2 Ensure topic initialization and migration Jobs must succeed before the app release updates, and that failures stop the rollout.
- [x] 3.3 Wait for Kubernetes rollout readiness and run HTTP, gRPC, OCPP, OCPI, Kafka delivery, and PostgreSQL persistence smoke checks.
- [x] 3.4 Scope AWS and Kubernetes deployment permissions to the staging repository, environment, and namespace.

## 4. Document and validate the pilot

- [x] 4.1 Document cluster prerequisites, Terraform setup, secrets, deployment, smoke checks, rollback, cost considerations, and teardown.
- [x] 4.2 Validate Terraform formatting/configuration, chart lint/rendering, and GitHub workflow YAML without applying cloud resources.
- [ ] 4.3 Run local Kubernetes validation where available and verify all application protocols and dependency paths in the authorized AWS pilot cluster.

Task 4.3 is pending: the local `kind` cluster could not start because the Docker daemon is not running, and this workspace has no authorized AWS staging account or credentials.
