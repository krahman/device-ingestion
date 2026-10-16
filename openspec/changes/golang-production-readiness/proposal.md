## Why

This change plans a personal learning exercise to deploy the existing Go service on AWS EKS in `ap-southeast-1`. There is no production traffic, customer data, fixed monthly budget, or enterprise availability requirement. The goal is to learn the production deployment path while keeping the initial AWS footprint small and understandable.

The repository already has local Docker Compose and Minikube paths, plus separate AWS staging configuration. The sandbox must be isolated and must not reuse staging or production Terraform. The proposal records what exists, what the sandbox will add, and what remains a later learning stage.

## What Changes

- Create an isolated Terraform sandbox under `infra/terraform/sandbox`; do not reuse `deploy/aws/staging/` or any production configuration/state.
- Provision one EKS cluster and one managed worker node only after measuring the app and EKS system pod resource needs and confirming schedulability.
- Deploy only the stateless Go application initially. Do not provision MSK, RDS, NAT Gateways, ALB, or WAF.
- Add a clearly named `sandbox` configuration that bypasses only the unsupported dependency-backed features; preserve the current full runtime and do not change normal production behavior. Return explicit HTTP 503/gRPC `Unavailable` for disabled data operations and never report false ingestion success.
- Use ECR and GitHub Actions for tests, practical scanning, Docker builds, and ECR publishing. Use GitHub OIDC with scoped AWS permissions; initially deploy manually using `kubectl` from the learner's workstation.
- Access the application only through `kubectl port-forward`; do not expose application ports publicly. Use Kubernetes health probes and resource requests/limits, and `kubectl logs` for initial debugging.
- Evaluate a learning-only public-subnet worker configuration with tightly restricted inbound security groups, no SSH, and only documented EKS/ECR/AWS API network needs. Verify workstation access to the EKS API before provisioning and document the security gap versus private-subnet production nodes.
- Use local Terraform state with secure backups. Before any apply, review `terraform plan`, list billable resources, estimate hourly and exercise-duration cost, and wait for explicit user approval. Never auto-apply.
- Keep an encrypted off-repository state backup and resource inventory; provide state recovery guidance and retain state until destroy verification.
- Defer CloudWatch and automated continuous deployment to later learning stages. Include full teardown, state retention until destruction verification, and review of ECR and other separately managed resources.
- Preserve the repository evidence matrix and mark all runtime/AWS outcomes as unverified until actually demonstrated.

## Capabilities

### New Capabilities

- `golang-service-readiness`: minimum bounded lifecycle, health, logging, and resource behavior required by the sandbox.
- `testing-quality-gates`: Go tests and CI checks required before publication.
- `container-supply-chain`: practical image build, scanning, ECR publication, and immutable rollback identity.
- `eks-production-platform`: isolated, single-region learning EKS/ECR infrastructure, networking, resource-fit validation, cost review, and teardown (the capability name is retained; this is not enterprise production infrastructure).
- `kubernetes-production-workload`: stateless one-replica app, probes, limits, and workstation port-forward access.
- `data-reliability`: preserve local Compose dependencies and clearly state that Kafka/PostgreSQL are not provisioned in the initial EKS exercise.
- `observability-operations`: initial `kubectl logs` workflow and later CloudWatch learning stage.

### Modified Capabilities

None. Existing staging and Minikube capabilities remain as documented. This proposal does not redefine their architecture or behavior.

## Impact

The eventual implementation may add `infra/terraform/sandbox/`, adjust CI workflows, add or configure Kubernetes manifests, and update learning documentation. This proposal phase changes only OpenSpec artifacts. It does not modify application code, existing staging Terraform, workflows, or AWS resources.
