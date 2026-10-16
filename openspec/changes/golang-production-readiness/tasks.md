## Implementation protocol

This file is the implementation plan only. Do not begin Stage 1 or modify application, CI, Terraform, or deployment files until the user approves the revised OpenSpec change. After approval, work one stage at a time. At each stage boundary, report files changed, exact validation commands/results, AWS services involved, cost exposure, and remaining issues, then wait for explicit approval before starting the next stage.

Never automatically apply infrastructure. Before any `terraform apply`:

1. Show the exact `terraform plan` and billable resources.
2. Estimate current `ap-southeast-1` hourly and expected exercise-duration cost using the selected resources and stated runtime assumptions.
3. Show the exact apply and teardown commands and residual-resource verification procedure.
4. Wait for explicit approval for that plan. If the plan changes materially, obtain approval again.

No AWS resources are provisioned during OpenSpec review or the initial repository audit.

## Stage 1 — Repository, dependency, network, and worker resource audit

**AWS services:** None. **Potential charges:** None. **Dependencies:** Existing source, Helm, Docker, Terraform configuration, and current EKS documentation/configuration.

- [x] 1.1 Inspect app composition and configuration to verify which paths require Kafka/PostgreSQL. Record that `app/composition.go:35-66` currently initializes both before serving.
- [x] 1.2 Inspect HTTP/gRPC health behavior, route registration, Dockerfile architecture, current app requests/limits, and chart features. Record that the current gRPC health service marks ingestion SERVING and that `values.yaml` targets RDS/MSK and enables AWS secrets/ALB values; do not reuse these defaults unchanged.
- [x] 1.3 Specify the sandbox route allowlist as HTTP `/health/live`, `/health/ready`, `/health/dependencies`, and gRPC health service only; all HTTP data/OCPI/metrics routes return 503 and all ingestion gRPC calls return `Unavailable`. If implementation cannot guarantee this, stop before Stage 2.
- [x] 1.4 Record the current chart resource baseline (`250m`/`512Mi` request, `1 CPU`/`1Gi` limit) and Dockerfile's implicit builder architecture as audit evidence only; the sandbox app must be measured after Stage 2, before instance selection.
- [x] 1.5 Identify required EKS system components (including VPC CNI, kube-proxy, CoreDNS, and node-level components); defer exact versions/resource requests until an EKS version is selected before the Stage 4 plan.
- [x] 1.6 Verify current EKS VPC requirements, including the minimum two cluster subnets in different AZs, from official AWS documentation; distinguish that control-plane subnet requirement from application/node high availability.
- [x] 1.7 Record workstation public CIDR as an operator-supplied prerequisite for the restricted public EKS API endpoint; do not query or store it in source control during this repository audit.
- [x] 1.8 Confirm the exact resource path `infra/terraform/sandbox/` is absent and isolated from staging and production state/configuration.
- [x] 1.9 Record required network flow categories (EKS control-plane/node, ECR API/DKR, S3 image layers, and AWS APIs required by selected node components). Derive exact endpoints and endpoint charges in Stage 4; general internet egress is not allowed by default.

Capacity measurement/calculation and the final EKS endpoint list are moved to Stage 4, after the sandbox runtime and candidate EKS/add-on versions exist but before selecting the EC2 instance or producing a plan. This avoids selecting an instance from measurements of a runtime that has not yet been implemented.

**Validation commands:** Read-only source/config review; `helm lint deploy/kubernetes/device-ingestion`; `helm template` with an isolated sandbox values file once defined; `go test ./...` for the current baseline if requested for the audit. Do not run `terraform apply`.

**Expected result / acceptance:** The audit confirms current runtime dependencies, defines the exact health-only sandbox contract, records existing resource/architecture baselines and EKS subnet requirements, and lists operator inputs. No EC2 type is selected at this stage.

**Rollback:** No mutations in this stage.

## Stage 2 — Dependency-free Go sandbox profile

**AWS services:** None. **Potential charges:** None. **Files:** `app/`, `cmd/`, `internal/`, focused tests, configuration docs.

- [x] 2.1 Implement an explicitly named `sandbox` runtime/configuration profile that does not initialize PostgreSQL or Kafka and does not create data workers/publishers; leave normal runtime behavior unchanged.
- [x] 2.2 Keep `/health/live` process-only. In normal mode `/health/ready` checks required dependencies; in sandbox mode readiness describes only enabled sandbox capabilities and a distinct dependency status reports Kafka/PostgreSQL as disabled/unavailable.
- [x] 2.3 Make HTTP operations requiring dependencies return 503 and gRPC operations return `Unavailable` before side effects; do not silently accept writes or return successful ingestion responses.
- [x] 2.4 Ensure gRPC health does not mark `IngestionService` SERVING in sandbox mode; keep process health distinguishable from ingestion capability health.
- [x] 2.5 Preserve normal full-runtime behavior for local Docker Compose and existing deployments when sandbox mode is not enabled.
- [x] 2.6 Ensure graceful termination, structured redacted JSON logs, and bounded listener settings continue to work in sandbox mode; do not open unused pprof/OCPP listeners.
- [x] 2.7 Measure sandbox process resource usage locally under health-check workload; define initial requests/limits with documented headroom. Final node fit remains a Stage 4 gate against exact EKS add-on versions.

**Validation commands:** `gofmt -w <changed-go-files>`; `go test ./...`; `go test -race ./...` if practical; run the sandbox profile locally and verify only the three HTTP health routes and gRPC health service respond, all data/OCPI/metrics routes return HTTP 503, ingestion RPCs return gRPC `Unavailable`, health status distinctions, normal full Compose runtime, logs, and SIGTERM exit.

**Expected result / acceptance:** The service runs without Kafka/PostgreSQL only in the explicit sandbox mode; health checks are truthful; unsupported ingestion does not create side effects; existing full local runtime remains available.

**Rollback:** Disable/revert the isolated sandbox profile if it changes default behavior or breaks normal local operation.

## Stage 3 — Docker image and GitHub Actions CI

**AWS services:** None for CI/build. **Potential charges:** GitHub Actions minutes; no AWS charges until ECR resources are created and an image is pushed.

- [x] 3.1 Preserve the existing multi-stage non-root Docker build and verify the new sandbox mode starts with the expected environment configuration.
- [x] 3.2 Configure a dedicated `.github/workflows/ci.yml` to run formatting, `go test ./...`, Go build, and selected static checks such as `go vet ./...`; keep staging deployment in its separate workflow.
- [x] 3.3 Add a practical container/dependency vulnerability scan and record its policy; do not claim a clean scan guarantees security.
- [x] 3.4 Build an image tagged with the source commit; produce an immutable reference suitable for ECR and rollback.
- [x] 3.5 Set an explicit Docker target architecture before building (initially use `linux/amd64` unless audit evidence supports another target); inspect image metadata and constrain EC2 candidates to the matching architecture.
- [x] 3.6 Keep pull-request CI free of AWS credentials. Prepare a narrowly scoped GitHub OIDC trust design solely for ECR publication; no EKS deploy role is required initially.
- [x] 3.7 Pin third-party Actions to reviewed immutable references where practical, keep workflow permissions minimal, and keep pushes validation-only; require explicit manual confirmation for staging deployment.

**Validation commands:** `go test ./...`; `go vet ./...`; `docker build -t device-ingestion:<git-sha> .`; run the container locally and verify health/sandbox behavior; run available workflow linter and image scanner.

**Expected result / acceptance:** CI passes, the image builds and starts non-root, scanning results are visible, and no CI job can access AWS until a later approved publish stage.

**Rollback:** Revert a faulty workflow/image change while retaining the last known-good image build.

## Stage 4 — Isolated Terraform sandbox, cost review, and approved apply

**AWS services:** EKS, one managed node group, ECR, IAM/GitHub OIDC for ECR publication, minimum VPC/subnet/route/security resources, and any explicitly selected VPC endpoints. No NAT Gateway, RDS, MSK, ALB, or WAF.

**Potential charges:** EKS cluster-hours, selected EC2 instance-hours, EBS, public IPv4, ECR storage, network transfer, and any VPC interface endpoint hourly/data processing charges. Calculate hourly and expected exercise-duration estimates from selected instance/capacity, endpoint plan, and expected hours. No fixed budget amount is assumed.

- [x] 4.1 Create `infra/terraform/sandbox/` as a standalone Terraform root with separate names/tags and local state; do not reference or import staging/production Terraform state.
- [x] 4.2 Add sandbox-specific `.gitignore` protection for state, backups, plan files, and credentials; document encrypted/access-controlled backups outside source control.
- [ ] 4.3 Select an EKS version currently supported in `ap-southeast-1`; identify exact VPC CNI, kube-proxy, and CoreDNS versions/configuration and their requests. Use only needed add-ons; no storage/monitoring add-ons are planned.
- [ ] 4.4 Calculate candidate node allocatable CPU/memory from selected EC2 specs after kube/system reservations, add-on and node DaemonSet requests, and the measured sandbox app requests. Include headroom and rollout strategy (Recreate/downtime-tolerant, so no surge Pod is required). Reject candidates that cannot schedule the workload.
- [ ] 4.5 Choose one EC2 instance only after 4.4 passes; require `amd64` compatibility with the Stage 3 image. Record CPU/memory arithmetic, resource inputs, candidate rejection reasons, current regional hourly cost, and exercise-duration estimate.
- [x] 4.6 Configure EKS with at least two cluster subnets in different AZs, a workstation-accessible public API endpoint restricted to the operator-supplied CIDR, and one managed worker node in the reviewed public-subnet learning topology; do not imply app HA.
- [x] 4.7 Add ECR API, ECR DKR, EC2 interface endpoints and an S3 gateway endpoint; configure node security group ingress only for EKS control-plane communication. No SSH or application ports; restrict worker egress to private EKS API, endpoint security group, and S3 prefix list.
- [x] 4.8 Require IMDSv2 with hop limit 1 and limit the node instance profile to EKS worker/bootstrap, CNI, and ECR pull managed policies.
- [x] 4.9 Configure manual workstation EKS access through an IAM access entry with cluster view and sandbox-namespace admin policies; keep this separate from the GitHub ECR-publish role.
- [x] 4.10 Create ECR with immutable tags, a five-image/tag retention policy, and repository/environment-scoped GitHub OIDC trust for image publication. Do not grant EKS access to GitHub Actions.
- [x] 4.11 Add `scripts/check-plan.py` to fail when managed RDS, MSK, NAT Gateway, ALB, or WAF resource types appear in plan JSON, including child modules. Keep a human full-plan review mandatory.
- [x] 4.12 Add a non-secret state inventory exporter and document encrypted off-repository state backups and recovery; never apply with empty state against existing resources.
- [ ] 4.13 Run `terraform fmt -check`, `terraform validate`, the exclusion plan guard, then `terraform plan -out=tfplan`; inspect `terraform show tfplan`, resource counts, endpoints, CIDRs, and cost worksheet.
- [ ] 4.14 Present exact plan, billable resource list, hourly and expected exercise cost, apply/destroy commands, and post-destroy checks. Stop and obtain explicit approval.
- [ ] 4.15 Only after approval, run the reviewed `terraform apply tfplan`; save state/backup securely and verify EKS/API/node/ECR readiness.

**Validation commands:** `terraform -chdir=infra/terraform/sandbox fmt -check`; `terraform -chdir=infra/terraform/sandbox validate`; sandbox plan-guard command; `terraform -chdir=infra/terraform/sandbox plan -out=tfplan`; `terraform -chdir=infra/terraform/sandbox show tfplan`. After approval/apply, use AWS CLI describe/list commands and `aws eks update-kubeconfig`; `kubectl get nodes` and `kubectl describe nodes` from the learner workstation.

**Expected result / acceptance:** Isolated EKS and ECR exist in Singapore; one selected managed node is Ready and has calculated capacity; workstation API access works; no banned AWS services exist; plan and cost approval are recorded.

**Rollback:** Do not auto-destroy after a failed apply. Preserve state, diagnose and show a new plan. Any destroy requires a reviewed destroy plan and explicit approval.

## Stage 5 — Publish image and manually deploy the Go service

**AWS services:** Existing sandbox ECR and EKS. **Potential charges:** ECR storage/transfer and EKS/EC2 uptime continue; no new infrastructure is intended. Confirm any additional resources are absent from commands/manifests.

- [ ] 5.1 After verifying CI, allow the OIDC-scoped GitHub Actions publish job to push the commit image to the sandbox ECR repository and record its digest.
- [ ] 5.2 Add sandbox-specific Kubernetes configuration with one application replica, sandbox runtime mode, measured requests/limits, startup/readiness/liveness probes, non-root restrictions, no service-account token unless needed, and only ClusterIP/Pod access.
- [ ] 5.3 Ensure no Kafka, PostgreSQL, Secrets Store CSI, ALB ingress, public LoadBalancer service, or production staging values are rendered.
- [ ] 5.4 From the learner workstation, authenticate with the intended AWS identity and manually apply the sandbox manifests/Helm release using `kubectl`; document each command and expected result.
- [ ] 5.5 Wait for `kubectl rollout status` and inspect `kubectl get pods,deploy,svc`, `kubectl describe pod`, `kubectl describe nodes`, requests/limits, and scheduler events; confirm the app schedules within validated capacity.

**Validation commands:** `helm lint`/`helm template` or `kubectl apply --dry-run=client` against sandbox values; GitHub Actions run; `aws eks update-kubeconfig`; `kubectl apply` or `helm upgrade --install`; `kubectl rollout status deployment/<name>`; `kubectl get pods,deploy,svc`; `kubectl describe pod <pod>`.

**Expected result / acceptance:** CI passes, image is published to ECR, workstation `kubectl` deploys only the Go app, and its Pod reaches Ready without external app exposure.

**Rollback:** Revert the Kubernetes manifest/release or image to the last successful revision. Keep EKS running until teardown is separately reviewed.

## Stage 6 — Port-forward, health, update, rollback, and logs

**AWS services:** Existing EKS/EC2/ECR only. **Potential charges:** EKS/EC2 hourly charges and ECR storage continue while resources remain.

- [ ] 6.1 Run `kubectl port-forward` to the application Pod or ClusterIP service and verify `/health/live` and `/health/ready` from the workstation.
- [ ] 6.2 Confirm data-ingestion routes clearly report sandbox unavailability and do not claim successful persistence.
- [ ] 6.3 Publish and deploy a second commit image; verify rollout and health using manual `kubectl` commands. Use rolling surge only if capacity calculation shows two Pods fit; otherwise configure and demonstrate a downtime-tolerant replacement strategy.
- [ ] 6.4 Demonstrate rollback with `kubectl rollout undo` or an explicit prior immutable image digest and confirm the prior revision becomes Ready.
- [ ] 6.5 Inspect application startup/request/shutdown logs with `kubectl logs`; verify no credentials or sensitive payloads are emitted.
- [ ] 6.6 Record the one-node/one-replica downtime and public-node networking limitations.

**Validation commands:** `kubectl port-forward service/<name> 8080:<port>`; `curl -fsS http://127.0.0.1:8080/health/live`; `curl -fsS http://127.0.0.1:8080/health/ready`; deploy second digest; `kubectl rollout status`; `kubectl rollout undo deployment/<name>`; `kubectl logs deployment/<name>`.

**Expected result / acceptance:** Health endpoints respond through port-forward; a new image version deploys; rollback restores a healthy prior revision; logs are inspectable with kubectl.

**Rollback:** If update or health checks fail, stop and restore last known-good image/revision before teardown.

## Stage 7 — Remove workloads, destroy, and verify resources

**AWS services:** Existing sandbox resources until destroyed. **Potential charges:** EKS, EC2, EBS, public IPv4, ECR storage, endpoints, and network usage continue until removed or explicitly retained.

- [ ] 7.1 Remove Kubernetes workloads/services and confirm no public load balancer was created.
- [ ] 7.2 Back up and retain Terraform state securely; show `terraform plan -destroy` and enumerate all resource deletions/retentions.
- [ ] 7.3 Present the exact destroy plan and residual resource inventory, then wait for explicit user approval before `terraform destroy`.
- [ ] 7.4 Run destroy only after approval. Verify EKS cluster, managed node group/EC2 instances, EBS volumes, ENIs/security groups, public IPv4, VPC endpoints/routes/subnets, and any load balancers are absent.
- [ ] 7.5 Review ECR repository/images and any separately managed resources. Show exact targets and implications before deleting retained ECR content; do not silently retain billable storage or delete user-selected images.
- [ ] 7.6 Keep Terraform state and backups until AWS resource destruction is verified; then report whether ECR or other resources remain and their charge categories.
- [ ] 7.7 Check inventory for CloudWatch log groups, EBS volumes/snapshots, public IPv4 addresses, load balancers, VPC endpoints, ENIs, and untagged or separately managed resources; explain that billing records/usage can appear after deletion.
- [ ] 7.8 Document state recovery from a missing/corrupted working file using encrypted off-repository backup and resource inventory; freeze applies and follow reviewed restore/import steps rather than applying with empty state.

**Validation commands:** `kubectl delete -f <sandbox-manifests>` or `helm uninstall <release>`; `terraform -chdir=infra/terraform/sandbox plan -destroy`; after approval `terraform -chdir=infra/terraform/sandbox destroy`; AWS CLI inventory scoped to sandbox tags/names; check ECR repository and images explicitly.

**Expected result / acceptance:** All intended infrastructure is destroyed and verified; any retained ECR or other resource is listed with ongoing cost category; state is retained through verification.

**Rollback:** Destruction is not reversible. Review the full destroy plan and secure state backup before execution; require explicit user approval.

## Later learning stages (not part of initial acceptance)

- [ ] L1 Propose CloudWatch log shipping, retention, IAM, and current ingestion/storage estimate; require approval before enabling.
- [ ] L2 Propose automated EKS CD and required runner/API network access, IAM/RBAC, rollback, and charges; require separate review and approval. Do not silently expand the ECR-only OIDC role.

## Definition of Done

- `go test ./...` passes and GitHub Actions CI passes.
- Docker image builds and passes the selected practical scan.
- GitHub Actions publishes an immutable, source-attributed image to ECR using scoped GitHub OIDC.
- Isolated `infra/terraform/sandbox` provisions EKS only after resource-fit, plan, cost estimate, and explicit approval.
- Only the dependency-free stateless Go sandbox mode is deployed; the Pod reaches Ready and health endpoints respond.
- Workstation accesses the app only with `kubectl port-forward`; application ports are not publicly exposed; initial logs are inspected with `kubectl logs`.
- A new image version is deployed manually and rollback is demonstrated.
- Kubernetes workloads are removed, Terraform destroy is run after approval, resources are verified absent, and local state is retained until verification.
- CloudWatch and automated CD remain later learning stages; no enterprise production readiness is claimed.
