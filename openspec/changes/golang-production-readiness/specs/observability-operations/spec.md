## ADDED Requirements

### Requirement: Use Kubernetes-native debugging in the initial sandbox

Initial sandbox debugging SHALL use existing structured application stdout/stderr through `kubectl logs`, along with Kubernetes Pod/deployment status and events. It SHALL NOT provision CloudWatch log shipping or a monitoring stack in the initial exercise.

#### Scenario: Operator inspects application output
- **WHEN** the application starts, serves a health request, or shuts down
- **THEN** the operator can inspect its logs using `kubectl logs` without exposing credentials or sensitive payloads

#### Scenario: Pod fails to start or schedule
- **WHEN** the application Pod is not Ready
- **THEN** the runbook directs the operator to inspect `kubectl describe pod`, events, resource requests, and `kubectl logs`

### Requirement: Verify health and rollout manually

The initial exercise SHALL verify `/health/live`, `/health/ready`, Pod Ready state, Deployment rollout, update, and rollback from the learner workstation using `kubectl` and `kubectl port-forward`.

#### Scenario: Health check fails
- **WHEN** either health endpoint fails through port-forward
- **THEN** the learning acceptance is not marked complete and the failure is diagnosed before proceeding

### Requirement: Defer CloudWatch and automated CD

CloudWatch log collection and automated GitHub Actions deployment to EKS SHALL be separate later learning stages requiring an updated plan, cost review where applicable, and explicit approval. Initial GitHub Actions SHALL test, scan, build, and publish to ECR only.

#### Scenario: Later stage proposes CloudWatch or EKS CD
- **WHEN** a future stage adds log shipping or automated deployment
- **THEN** it documents added IAM/network/resource requirements and costs and does not silently broaden the initial ECR publish workflow
