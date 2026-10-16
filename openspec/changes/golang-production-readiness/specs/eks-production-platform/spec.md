## ADDED Requirements

### Requirement: Isolate the Terraform sandbox

The learning infrastructure SHALL be defined under `infra/terraform/sandbox/` with separate names, local state, and secure state backups. It SHALL NOT reuse or import existing staging or production Terraform configuration or state.

#### Scenario: Review the sandbox plan
- **WHEN** Terraform plans the learning environment
- **THEN** all resources belong to the isolated sandbox root and no staging/production state or resource is referenced

### Requirement: Validate node capacity before selecting an instance

The managed worker instance SHALL be selected only after calculating its allocatable CPU/memory against measured sandbox application requests, selected EKS add-on/DaemonSet requests, system reservations, and rollout overlap. The calculation SHALL confirm that the application can schedule with documented headroom, verify that the container image architecture matches the EC2 architecture, and include the selected instance's current regional cost estimate.

#### Scenario: Choose worker capacity
- **WHEN** the Terraform instance type is set
- **THEN** the repository records resource inputs, allocatable calculation, schedulability/headroom result, image/instance architecture compatibility, and `ap-southeast-1` hourly/exercise-duration estimate

### Requirement: Provision the minimum approved AWS footprint

The initial sandbox SHALL provision EKS, a managed node group configured for exactly one worker node at steady state and validated for capacity, ECR, scoped IAM/OIDC, and only required network resources in `ap-southeast-1`. It SHALL NOT create NAT Gateways, MSK, RDS, ALB, WAF, or other optional services.

The EKS cluster VPC configuration SHALL provide the AWS-required minimum of two cluster subnets in different Availability Zones. This prerequisite SHALL NOT be represented as multi-AZ application availability; the workload remains one replica on a small managed node group.

#### Scenario: Inspect the initial infrastructure plan
- **WHEN** an operator reviews the plan
- **THEN** it contains the required cluster subnets in two AZs, no NAT, MSK, RDS, ALB, or WAF, and identifies any selected public IPv4/VPC endpoint charges without claiming application HA

### Requirement: Restrict network exposure for the learning-only public worker

If public-subnet worker nodes are selected, node security groups SHALL allow only required EKS control-plane/node communication inbound, no SSH, and no public application ports. By default, node egress SHALL be limited to the security groups of the required VPC endpoints and the S3 gateway endpoint/prefix list; general internet egress is not allowed. The EKS API endpoint SHALL be reachable from the learner workstation and restricted to its current public CIDR. The plan SHALL document endpoint charges and the greater exposure of public-addressed workers compared with private-subnet production workers.

#### Scenario: Verify workstation access
- **WHEN** the cluster is created
- **THEN** the learner workstation can authenticate to the EKS API with `kubectl` and no application listener is publicly reachable

#### Scenario: Review egress choice
- **WHEN** the worker network is planned
- **THEN** required AWS services, endpoint destinations/ports, endpoint costs, and residual risk are documented; any request for general internet egress is blocked pending explicit review

#### Scenario: Review EC2 identity and metadata
- **WHEN** the managed node group is configured
- **THEN** its instance role contains only required node/bootstrap and ECR pull permissions, IMDSv2 is required, Pod access to metadata is minimized, and the metadata hop limit is justified against node-component requirements

### Requirement: Publish ECR images using scoped GitHub OIDC

GitHub Actions SHALL use GitHub OIDC with repository/environment-scoped trust to publish immutable, source-attributed images to ECR. The initial OIDC role SHALL NOT receive EKS cluster deployment permissions; the initial EKS deployment SHALL be performed manually by the learner workstation.

#### Scenario: GitHub publishes an image
- **WHEN** CI succeeds and the authorized publish job runs
- **THEN** the job assumes a short-lived OIDC role limited to the sandbox ECR operations and records the image digest

The learner workstation SHALL use a separately authorized AWS identity with only the permissions required to describe/connect to the sandbox cluster and perform the documented namespace-scoped manual deployment. The ECR publishing role SHALL NOT grant Kubernetes deployment permissions.

### Requirement: Estimate, approve, destroy, and verify resources

Before every Terraform apply, the operator SHALL review the exact plan, billable resources, current hourly and expected exercise-duration estimate, apply/destroy commands, and residual-resource checks. The workflow SHALL wait for explicit approval and SHALL never apply automatically. Local state and backups SHALL be retained until destruction is verified.

#### Scenario: Apply is proposed
- **WHEN** a Terraform apply is ready
- **THEN** the plan and cost estimate are presented and no apply occurs without explicit approval

#### Scenario: Sandbox teardown completes
- **WHEN** the operator destroys the sandbox
- **THEN** EKS, EC2, EBS, network interfaces, public IPv4, endpoints, load balancers, CloudWatch log groups, and other provisioned resources are checked; ECR storage and separately managed resources are reported; billing delays after deletion are explained; state is retained through verification

### Requirement: Enforce staging isolation and recover local state

Sandbox Terraform SHALL use distinct names, tags, files, and state under `infra/terraform/sandbox/`. A plan validation guard SHALL fail if excluded RDS, MSK, NAT Gateway, ALB, or WAF resources appear, even when introduced by a module. The complete plan SHALL still receive human review. State backups SHALL be encrypted and outside the repository, and a recovery procedure SHALL prevent applying empty state over existing infrastructure.

#### Scenario: Excluded infrastructure appears in a plan
- **WHEN** a plan contains an excluded resource or a module that creates one
- **THEN** automated plan validation fails and apply is blocked

#### Scenario: Local state is lost or corrupted
- **WHEN** the working sandbox state cannot be trusted
- **THEN** the operator freezes apply/destroy, checks inventory, restores a verified encrypted backup or reconstructs/imports resources through a reviewed recovery procedure, and does not apply using empty state

#### Scenario: Verify staging remains isolated
- **WHEN** the sandbox is planned or applied
- **THEN** no staging state, resource, or configuration is imported or modified and sandbox resources are identifiable by distinct tags/names
