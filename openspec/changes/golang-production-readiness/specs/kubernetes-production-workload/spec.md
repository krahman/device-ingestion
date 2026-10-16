## ADDED Requirements

### Requirement: Deploy only the stateless Go sandbox profile

The initial EKS release SHALL contain only one replica of the Go service running its explicit dependency-free sandbox profile. It SHALL NOT deploy Kafka, PostgreSQL, database migrations, external secrets CSI, or any other dependency workload. Unsupported data-ingestion routes SHALL remain disabled/unavailable and SHALL NOT claim successful persistence.

#### Scenario: Render sandbox Kubernetes resources
- **WHEN** the sandbox chart/manifests are rendered
- **THEN** they contain only the Go workload and required ClusterIP/service-account/config resources, with no data services, Secrets Store CSI, ingress, or public LoadBalancer service

#### Scenario: Sandbox runtime receives a data write
- **WHEN** a client invokes a route that requires Kafka or PostgreSQL
- **THEN** the service returns a clear safe unavailable response before side effects and does not report the request as persisted

### Requirement: Provide a single protected Pod with accurate health probes

The workload SHALL start with one replica, resource requests and limits derived from capacity validation, startup/readiness/liveness probes, non-root restrictions, and a bounded termination grace period. The Pod SHALL be schedulable on the selected node under the documented allocatable calculation.

The application SHALL not receive a Kubernetes API token unless an explicitly required sandbox capability needs it; otherwise automounting SHALL be disabled.

The rollout strategy SHALL account for one-node capacity. If the node cannot run old and new replicas simultaneously, the deployment SHALL use an explicitly downtime-tolerant replacement strategy rather than stall waiting for surge capacity.

#### Scenario: Application becomes ready
- **WHEN** Kubernetes starts the sandbox Pod
- **THEN** the Pod reaches Ready only when the dependency-free runtime can serve its supported routes and health checks

#### Scenario: Node disruption occurs
- **WHEN** the single node is unavailable
- **THEN** documentation identifies the resulting service interruption and makes no high-availability claim

#### Scenario: Deployment update exceeds surge capacity
- **WHEN** a new revision is deployed and two replicas cannot fit
- **THEN** the rollout uses the documented replacement strategy, tolerates brief downtime, completes successfully, and remains rollback-capable

### Requirement: Use workstation kubectl and port-forward access

The initial deployment and rollback SHALL be performed manually from the learner workstation using an authorized AWS identity and `kubectl`. The application SHALL be accessed through `kubectl port-forward`; application ports SHALL NOT be exposed through public ingress, ALB, WAF, NodePort, or LoadBalancer service.

#### Scenario: Learner accesses health endpoints
- **WHEN** the workload is Ready
- **THEN** a workstation `kubectl port-forward` reaches the health endpoints and no public application endpoint is created

#### Scenario: Learner updates and rolls back
- **WHEN** a new ECR image digest is selected
- **THEN** the learner can update the Deployment manually, observe rollout status, and restore the prior revision/digest with `kubectl`

### Requirement: Keep production deployment features out of initial scope

Initial acceptance SHALL use one replica and manual deployment without HPA, PDB, topology-spread policy, public ingress, CloudWatch log shipping, or automated EKS CD. Documentation SHALL identify these as later learning or production decisions.

#### Scenario: Review initial deployment scope
- **WHEN** a maintainer inspects the rendered resources and workflow
- **THEN** no automated EKS deploy, CloudWatch collector, autoscaler, or public ingress is configured
