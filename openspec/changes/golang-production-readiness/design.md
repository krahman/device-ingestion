## Context and current audit

This plan is for a personal EKS learning exercise in `ap-southeast-1`, with no production traffic, customer data, fixed monthly budget, or enterprise availability target. The audit uses repository files as of 2026-10-16; it is not a live AWS or runtime audit. This is a learning sandbox, not production infrastructure.

The service is a Go composition root with HTTP/OCPI, OCPP WebSockets, gRPC, Kafka, and PostgreSQL adapters (`app/composition.go`, `app/runtime.go`, `internal/adapters/`). Docker Compose (`compose.yaml`) and Minikube (`deploy/kubernetes/minikube/`, `docs/minikube-deployment.md`) exist. The AWS staging stack under `deploy/aws/staging/` includes EKS, RDS, MSK, NAT, and ALB-related setup and is explicitly excluded from reuse.

The current application startup calls `postgres.Open`, pings PostgreSQL, checks Kafka, then initializes publishers and workers (`app/composition.go:35-66`). Therefore, the approved initial AWS scope (only the stateless Go service, with no DB, Kafka, or managed services) cannot use the current full runtime unchanged. The plan must add a clearly scoped dependency-free sandbox runtime/profile for health checks and explicitly selected stateless routes, or stop for review if the existing composition cannot support it safely. It must not fake readiness while data endpoints are unusable, silently add dependencies, or accept writes it cannot process. The existing chart defaults also point at RDS/MSK and enable Secrets Store/ALB (`deploy/kubernetes/device-ingestion/values.yaml`); create separate sandbox values/manifests and do not reuse those defaults as-is.

### Evidence matrix

Status reflects source/configuration evidence only; it does not claim that AWS behavior has been tested.

| Area | Requirement | Current status | Evidence | Priority | Action |
|---|---|---|---|---|---|
| Application | Graceful shutdown | Partial | `cmd/server/main.go:19-22`; `app/runtime.go:46-79`; OCPP WebSocket lifecycle `internal/adapters/ocppapi/handler.go:96-133` | P1 | Preserve bounded signal handling; only exercise OCPP drain if enabled in sandbox profile. |
| Application | Health endpoints | Verified in source | `app/composition.go:160-173`; `app/health_test.go:12-39` | P0 | Keep probes backed by the actual sandbox runtime's health semantics. |
| Application | Dependency-free runtime | Missing | Startup requires PostgreSQL/Kafka in `app/composition.go:35-66` | P0 | Add a deliberate health-only/stateless sandbox mode, disable unsupported write paths, and test readiness/route behavior. |
| Application | JSON logging and resource behavior | Partial | JSON `slog` in `app/runtime.go:30-44`; no AWS log shipping | P1 | Keep redacted JSON stdout logs; initially inspect via `kubectl logs`. |
| Testing | Go tests and CI | Partial | Existing tests; `.github/workflows/ci.yml` runs formatting, tests, build, image checks, Terraform validation, and Helm lint/render | P0 | Ensure tests/build and practical image checks pass before ECR publication. |
| Docker | Non-root image and target architecture | Partial | Multi-stage image and UID/GID 10001 in `Dockerfile:1-24`; build sets `GOOS=linux` but inherits builder `GOARCH` | P0 | Set an explicit sandbox image architecture and ensure the selected EC2 architecture matches it. |
| CI/CD | ECR publish and manual deploy | Partial | Existing staging workflow uses OIDC/ECR/EKS in `.github/workflows/deploy-staging.yml`; existing Terraform trust/policies in `deploy/aws/staging/eks.tf` are not sandbox policy | P0 | Add sandbox-scoped CI and ECR publication via GitHub OIDC. Initial EKS deploy is workstation `kubectl`, not GitHub CD. |
| AWS | Isolated Terraform sandbox | Missing | No `infra/terraform/sandbox/` exists | P0 | Create a separate, local-state sandbox stack; never reuse staging state/modules/config as a deployment template. |
| AWS | Network and workstation API access | Missing for sandbox | Staging networking is in `deploy/aws/staging/main.tf:1-79`; current cluster endpoint settings in `eks.tf`/variables | P0 | Validate workstation public IP/API reachability, public node subnet route, security group, endpoint mode, and ECR/API access before plan/apply. |
| AWS | Worker capacity | Unknown | Existing chart requests `250m CPU`, `512Mi`, limits `1 CPU`, `1Gi` in `deploy/kubernetes/device-ingestion/values.yaml`; this chart also enables Kafka/RDS-dependent configuration | P0 | Measure actual sandbox app requests plus EKS add-on/daemon requests, allocatable capacity, and update/surge requirements before instance selection. |
| AWS | Expensive components | Present in staging, excluded in sandbox | NAT/RDS/MSK in `deploy/aws/staging/main.tf:54-79,132-194`; ingress configured in chart values | P0 | Do not provision NAT, MSK, RDS, ALB, or WAF in initial sandbox. |
| Kubernetes | Probe/resource/port-forward setup | Partial | Existing probes and requests/limits in `deploy/kubernetes/device-ingestion/templates/deployment.yaml:56-75` and `values.yaml`; service values default to `ClusterIP` | P0 | Add sandbox-specific values with no public ingress, no CSI dependency, stateless mode, requests/limits, health checks, and one replica. |
| Operations | Initial observability | Missing in AWS | Structured logs exist, but no sandbox CloudWatch path | P1 | Use `kubectl logs` in initial stage; CloudWatch is deferred. |
| Operations | Cost/destroy procedure | Partial in staging only | `docs/aws-staging-deployment.md` documents staging deployment/teardown; no isolated sandbox runbook | P0 | Document cost estimate before apply, state backups, workload removal, Terraform destroy, and residual-resource checks. |

## Approved architecture

### Initial learning path

1. **Local iteration:** continue using Docker Compose for the full Go/Kafka/PostgreSQL development system and Minikube for local Kubernetes rehearsal.
2. **CI and image:** GitHub Actions runs tests, build, practical vulnerability scanning, and Docker build. A publish job uses repository/environment-scoped GitHub OIDC to push a commit-attributed immutable image to an ECR repository created by the isolated sandbox Terraform. There is no automated EKS deployment in the initial stage.
3. **Terraform:** create a new isolated stack only under `infra/terraform/sandbox/`, in `ap-southeast-1`, with EKS, a small managed node group after a resource-fit calculation, ECR, IAM/OIDC, and only required network resources. Use local state with secure backup; never check state or saved plans into Git.
4. **Networking:** no NAT Gateway. Amazon EKS requires at least two cluster subnets in different AZs; provide that minimum even though the sandbox has no multi-AZ application availability objective ([EKS networking requirements](https://docs.aws.amazon.com/eks/latest/userguide/network-reqs.html)). The managed node group starts at one capacity-validated node. Evaluate public-subnet workers as a learning-only option. Restrict inbound node security group rules to required cluster/node traffic; no SSH and no public application ports. Verify the learner workstation can reach the EKS API before apply and restrict the public API endpoint to the learner's current public CIDR. Derive the required EKS/ECR/AWS API flows and default worker egress to the corresponding VPC endpoints/S3 prefix list, including endpoint cost in the estimate. The flow categories are EKS control-plane/node communication, ECR API and registry access, S3 image-layer downloads, and AWS APIs required by selected node components (for example, EC2 for VPC CNI). Confirm the exact endpoint set against selected versions before the Terraform plan. If a required bootstrap/add-on operation needs broader egress, stop and present the exception for review rather than silently allowing general internet egress. Do not claim that public-subnet workers are equivalent to private-subnet production nodes.
5. **Application mode:** add an explicit dependency-free `sandbox` configuration because current startup requires PostgreSQL and Kafka. The sandbox allowlist is HTTP `/health/live`, `/health/ready`, `/health/dependencies`, plus the gRPC health service. `/health/live` reports process liveness; full-mode `/health/ready` checks PostgreSQL and Kafka; sandbox readiness covers only the enabled health-only profile and exposes dependencies as disabled/unavailable. The gRPC ingestion service must not report serving in sandbox mode. Data ingestion, OCPI, database/Kafka workers, DB-backed metrics, OCPP, pprof, and writes remain disabled; HTTP returns 503 and gRPC returns `Unavailable` for unsupported operations. Do not create in-cluster dependencies, RDS, or MSK.
6. **Kubernetes:** deploy one Go application replica with resource requests/limits, probes, non-root security context, no unnecessary service-account token, and a ClusterIP service or direct Pod port-forward. Use a downtime-tolerant rollout strategy if one node cannot fit surge capacity. Do not create ingress, ALB, WAF, public service, secrets CSI, Kafka, or PostgreSQL resources.
7. **Access and debugging:** use workstation `kubectl` after `aws eks update-kubeconfig`; use `kubectl port-forward` to test the health endpoint; use `kubectl get/describe/logs` for initial debugging. No app traffic is exposed publicly.
8. **Later learning stages:** CloudWatch log collection and automated CD are explicitly deferred. They require a separate stage plan, charge disclosure, and approval before provisioning/configuring.

### Sandbox image-publisher trust boundary

The future sandbox ECR publisher is a separate GitHub Actions job and IAM role from the existing staging deployment role. Only that publishing job receives `id-token: write`; pull-request validation remains without AWS credentials. The role trust requires the GitHub OIDC audience `sts.amazonaws.com` and an exact subject for the configured repository and protected `sandbox` environment (`repo:<owner>/<repository>:environment:sandbox`). Its permissions include `ecr:GetAuthorizationToken` and image-upload actions scoped to the sandbox ECR repository; it has no EKS cluster access or deploy permissions. The repository owner/name and the protected environment must be confirmed before Terraform trust is finalized. Stage 3 prepares this boundary; Stage 4 provisions its IAM/OIDC and ECR resources only after plan and cost approval.

The credential-free CI workflow builds a commit-labeled `linux/amd64` image from digest-pinned Go and Alpine base images, verifies the non-root user and sandbox health behavior, emits a CycloneDX SBOM artifact retained for seven days, and scans the image with Trivy. The scan gate fails for fixable HIGH or CRITICAL vulnerabilities (`--ignore-unfixed`); this threshold is a pilot policy and a clean scan is not a security guarantee. Pushes and pull requests run validation only and never assume AWS credentials. Staging ECR/EKS deployment is manual, runs CI first, and requires an explicit confirmation input that defaults to false.

### Resource-fit gate

Do not pick the smallest EC2 instance by name or price. The sandbox values for
Minikube request `100m` CPU / `256Mi` memory and limit `500m` CPU / `512Mi`
(`deploy/kubernetes/minikube/values.yaml`). Stage 2's 14,260 KiB RSS sample was
from a native macOS process, so it does not establish Linux-container peak
memory or CPU usage.

The current Terraform root uses `t3.medium` as a **candidate**, not an approved
capacity result: raw capacity is 2 vCPU / 4,096 MiB. The review worksheet
reserves 500m CPU / 1,024 MiB for OS and kubelet, giving a conservative planning
floor of 1,500m / 3,072 MiB allocatable. It budgets 400m / 512 MiB for the
VPC CNI, kube-proxy, and two CoreDNS replicas, and then the app request of 100m
/ 256 MiB. Estimated remaining request headroom is 1,000m / 2,304 MiB. The
worker uses `Recreate`, so no second app replica is included for rollout. These
are conservative estimates, not measured `Node.status.allocatable` or exact
add-on requests.

Terraform selects the newest EKS-compatible add-on version during planning.
Before apply, inspect the exact selected versions and their default Pod
requests for the regional EKS version; reject the candidate if total requests
or measured Linux memory do not fit with headroom. After apply, verify actual
node allocatable CPU/memory, system Pod requests, `kubectl describe nodes`, and
scheduler events before progressing to Stage 5. The image is explicitly
`linux/amd64` and the candidate worker is x86_64. This candidate must be
reconsidered if the plan or resulting node differs from those assumptions.

The preliminary cost worksheet and assumptions are in
`infra/terraform/sandbox/README.md`. Refresh it in the AWS Pricing Calculator
and reconcile it with the final plan before requesting apply approval.

## Network security trade-offs

The no-NAT public-subnet approach is a learning compromise, not the production recommendation. The EKS VPC has two cluster subnets in different AZs, but this does not make the one-node application highly available. The worker subnet has an Internet Gateway route but disables public IPv4 assignment; node security-group egress is limited to the EKS API, ECR/EC2 interface endpoints, and S3 prefix list. The worker has no direct internet egress or public inbound access. VPC interface endpoints incur hourly/data processing charges. If a required operation cannot work with endpoint-scoped egress, document the exact exception and obtain review before widening access.

The EKS API endpoint is separate from application exposure. It must be reachable from the learner workstation to support manual `kubectl`, and its public CIDR restriction must be tested with the workstation's actual public IP. Application ports remain ClusterIP/Pod-only and are accessed through `kubectl port-forward`.

Production private-subnet nodes typically avoid direct public node addressing and route required egress through controlled NAT or VPC endpoints, with additional network controls and often multiple AZs. Those components add cost and are out of this sandbox scope. Public nodes are not a production-ready substitute.

For this sandbox, the EC2 instance profile must contain only EKS worker/bootstrap and ECR image-pull permissions needed by the node. The GitHub OIDC role is limited to ECR publication; the learner's workstation role receives only the required EKS cluster access. Require IMDSv2 and minimize Pod access to instance metadata; choose the metadata hop limit only after verifying node components still work. No production credentials or customer data may be stored in the sandbox.

## Cost, approval, state, and teardown

No fixed monthly budget exists. Before every `terraform apply`, present the exact `terraform plan`, identify billable resources, estimate hourly cost and the expected exercise-duration total with assumptions using current regional pricing, explain residual charges, list exact apply/destroy commands, and wait for explicit approval. Never run `terraform apply` automatically. Expected charge categories can include EKS cluster-hours, EC2 worker-hours, EBS volumes, public IPv4, VPC endpoints if selected, ECR storage/transfer, and data transfer. No NAT, RDS, MSK, ALB, or WAF charges are intended in the initial plan.

Keep Terraform state local and access-controlled, make a secure backup before apply and before destroy, and never commit state/plan files. Retain both working state and backup until AWS resources are verified destroyed. On teardown: remove Kubernetes workloads first; run the reviewed `terraform destroy`; verify EKS, managed nodes/EC2, EBS, load balancers (none expected), security groups/network interfaces, public IPv4, endpoint resources, and other created resources; review ECR storage and separately managed resources; then retain or dispose of state only after verification. ECR repository/image retention is a separate consideration and must be priced/reviewed; do not delete repository contents without showing the target.

Keep the working state in a dedicated ignored sandbox state directory and maintain encrypted backups outside the repository. Record created resource names/IDs and sandbox tags in an inventory. If state is missing or corrupt, freeze apply/destroy, recover from a verified encrypted backup if possible, compare state against the AWS inventory, and reconstruct/import resources only through a reviewed recovery procedure; never initialize empty state and apply over existing resources. Keep the state and backup until destroy verification.

The Terraform plan guard must fail validation if planned resource types include RDS, MSK, NAT Gateway, ALB, or WAF resources, including equivalents hidden behind a reused module. A human must still review the complete plan before approval; the guard supplements, not replaces, that review.

## Decisions and non-goals

Approved: single region, isolated sandbox path, small managed node group selected by capacity validation, stateless Go service only, ECR, GitHub Actions CI/image publishing via OIDC, initial manual `kubectl`, `kubectl port-forward`, probes/limits, `kubectl logs`, local Terraform state, cost approval, and complete teardown. No NAT, MSK, RDS, ALB, or WAF initially. CloudWatch and automated CD are later stages.

The existing service's mandatory Kafka/PostgreSQL startup is a verified incompatibility with the approved stateless-only deployment. The implementation plan must address this with a clearly limited dependency-free mode and must not mask the limitation. If the application cannot provide that mode safely, stop before AWS provisioning and present the concrete conflict for a decision.

Non-goals: production availability/SLO, multi-AZ HA, private subnet production design, durable data, managed Kafka/database, public ingress, autoscaling, WAF, 24/7 monitoring, cross-region recovery, compliance, and automatic CD in the initial exercise.

## Unresolved prerequisites before implementation/provisioning

- AWS account ID and the operator's least-privilege Terraform/EKS administrator identity; no credentials belong in the repository.
- Learner workstation public CIDR and whether it remains stable through the cluster exercise; EKS API access must be testable before apply.
- Current regional EKS 1.36 availability and compatible add-on versions/requests; confirm with AWS CLI/plan immediately before approval.
- Actual measured Linux sandbox application CPU/memory and exact add-on/DaemonSet requests; compare them with the documented conservative t3.medium planning budget before apply.
- Confirm available EC2 capacity for the selected x86_64 `t3.medium` in the selected worker AZ; if unavailable or insufficient, re-evaluate cost/capacity before apply.
- Exact endpoint set for EKS worker bootstrap, VPC CNI/EC2, ECR API/DKR, S3 image layers, and any other selected AWS API; endpoint hourly/data costs and security-group rules.
- GitHub owner/repository, existing OIDC provider ARN, protected `sandbox` environment, workstation IAM principal ARN, and current workstation `/32` CIDR.
- Local state path, filesystem permissions, encryption method for off-repository backups, and restore/import operator procedure.
- Expected cluster/worker running hours, ECR retention, and any VPC endpoint usage for the regional hourly/exercise-duration estimate. There is no monthly budget threshold to supply.
- Exact AWS endpoint set and cost for the selected EKS version/add-ons and node bootstrap; all general internet egress remains disallowed unless separately reviewed.

These are implementation inputs, not reasons to provision early. Stage 1 must resolve the technical choices that can be audited without AWS changes; account/workstation/GitHub-specific values must be supplied or confirmed by the operator before the Terraform plan is finalized.

## Staged implementation and approval gates

Implement one stage at a time. After each stage, report changed files, exact validation and results, cost exposure, and open issues; wait for approval before continuing. Any stage that creates or configures billable AWS resources requires its own reviewed plan/cost estimate and explicit approval. Initial scope ends after manual deployment, update, rollback, teardown, and verification. CloudWatch and automated CD remain later learning proposals.

1. Repository/dependency and worker resource audit.
2. Dependency-free Go sandbox profile and tests.
3. Docker build and GitHub Actions CI/scanning.
4. Isolated Terraform/ECR/OIDC configuration, plan, cost estimate, and user-approved apply.
5. Image publishing to ECR and manual workstation `kubectl` deployment.
6. Port-forward health tests, image update, rollback demonstration, and `kubectl logs`.
7. Workload removal, Terraform destroy, and resource verification.
8. Optional future proposal for CloudWatch and automated CD.

## Risks

- Dependency-free mode changes which service routes work; be explicit and test that unsupported routes do not imply successful ingestion.
- Public subnet workers have broader egress exposure than private nodes. Security groups, endpoint choices, and public API CIDR must be reviewed.
- A tiny node may not fit app requests, EKS system pods, and rollout overlap. Resource-fit validation is a hard gate, not a documentation-only note.
- Single node and one replica can be unavailable during maintenance or update; brief downtime is acceptable only as a disclosed learning limitation.
- ECR storage, EKS cluster-hours, EC2, EBS, public IPv4, and optional VPC endpoint usage continue to cost while resources remain. Destroy after the exercise and verify leftovers.
- Local state loss can orphan resources; secure backups and inventory verification are required.
- The existing staging Terraform includes services expressly excluded here. Path/state isolation and a full plan review prevent accidental staging reuse.
