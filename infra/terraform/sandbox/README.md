# Device Ingestion AWS Learning Sandbox

This Terraform root is isolated from `deploy/aws/staging`. It creates one EKS
cluster, one small managed node group, one immutable ECR repository, and the
minimum VPC/IAM resources needed to run only the stateless Go service in its
`APP_PROFILE=sandbox` mode. It creates no RDS, MSK, NAT Gateway, load balancer,
WAF, or CloudWatch log group.

**This directory has not been applied.** Terraform planning is read-only with
respect to AWS resources. Do not run `terraform apply` until the complete plan
and cost estimate have been reviewed and explicit approval is given.

## AWS resources and network boundary

- Region: `ap-southeast-1` only.
- EKS Kubernetes version: `1.36` by default. Check regional availability and
  standard support with the preflight command below immediately before plan.
- Two `/24` public subnets in distinct available AZs host the EKS control-plane
  network interfaces; the single managed worker is placed in the first AZ.
- The worker subnet has an Internet Gateway route, but automatic public IPv4
  assignment is disabled. The instance therefore has no public internet
  address. Its security group permits only the private EKS API, ECR/EC2
  interface endpoints, and the S3 gateway endpoint. No SSH or application
  ports are open. The launch template attaches only this restricted worker
  group; it does not attach the EKS-created cluster group with broad default
  egress. Explicit worker rules allow cluster-to-kubelet and worker-to-API
  traffic.
- Three interface endpoints (`ecr.api`, `ecr.dkr`, and `ec2`) are placed in
  the worker AZ. The S3 gateway endpoint has no hourly endpoint charge. The
  cluster API is public and private; its public CIDR allowlist is required and
  accepts only IPv4 `/32` entries. The workstation uses that public API. Nodes
  use the private API endpoint.
- EKS managed add-ons are limited to VPC CNI, kube-proxy, and CoreDNS. Terraform
  queries the current versions compatible with the selected EKS version during
  planning and records the selected versions in outputs and state. Inspect and
  record those exact versions before apply.
- The node is `t3.medium` (2 vCPU, 4 GiB, x86_64) with one 20 GiB encrypted
  gp3 root volume. T3 surplus CPU credits are disabled (`standard` mode).
- The operator's workstation IAM principal gets cluster-wide view access and
  namespace-scoped admin access for `device-ingestion-sandbox`. GitHub OIDC is
  restricted to the exact repository's `sandbox` GitHub environment and can
  push/describe images in this ECR repository only. It receives no EKS access.
- ECR tags are immutable. The lifecycle policy retains five tagged images and
  expires untagged images after seven days; this is a pilot rollback window.

The two-AZ subnets satisfy EKS control-plane subnet requirements. The single
worker AZ and one replica are intentionally not highly available. A public
subnet is a routing property; with no public address and the restricted
security group, this worker does not have general outbound internet access.
Production normally places nodes in private subnets and uses reviewed egress
paths and multi-AZ capacity.

## Worker capacity review

The application image is `linux/amd64`. Local Kubernetes sandbox values set
the application request to `100m` CPU / `256Mi` memory and the limit to `500m`
CPU / `512Mi` memory (`deploy/kubernetes/minikube/values.yaml`). Stage 2's
14,260 KiB measurement was from a native macOS process, so it is not treated as
a Linux-container peak measurement.

| Capacity item | CPU | Memory | Basis |
| --- | ---: | ---: | --- |
| `t3.medium` raw capacity | 2,000m | 4,096Mi | EC2 published instance specification |
| Conservative OS + kubelet planning reserve | 500m | 1,024Mi | Budgeted reserve for capacity planning; actual AL2023 allocatable values must be checked on the node |
| Planning allocatable floor | 1,500m | 3,072Mi | Raw capacity minus conservative reserve |
| System add-on budget | 400m | 512Mi | Conservative allowance for VPC CNI, kube-proxy, and two CoreDNS replicas; verify requests from installed add-on Pods |
| Go application request | 100m | 256Mi | Current Minikube sandbox values |
| Remaining planned headroom | 1,000m | 2,304Mi | Allocatable floor minus add-on and application request budgets |

This budget leaves substantial room for one sandbox Pod and the standard EKS
system add-ons. It is not a substitute for the exact regional add-on version
and node-allocatable data. Before apply, confirm the add-on versions and
requests against the plan and their schemas. After apply, `kubectl describe
node`, `kubectl get pods -A`, Pod requests, and scheduler events are mandatory
capacity checks. The app rollout uses a downtime-tolerant `Recreate` strategy in
Stage 5; no surge Pod is budgeted.

## Preliminary AWS cost estimate

Rates below are USD estimates checked on **2026-10-16**. They assume one
continuously running cluster, one `t3.medium`, one 20 GiB gp3 root disk, three
single-AZ interface endpoints, no public worker IP, no NAT Gateway, no
application load balancer, and up to 1 GiB of retained ECR images. The 72-hour
exercise is an example duration, not a user-defined budget or time limit.

| Resource | Estimate | 8 hours | 72 hours | 730 hours |
| --- | ---: | ---: | ---: | ---: |
| EKS standard-support cluster | `$0.10/hour` | `$0.80` | `$7.20` | `$73.00` |
| `t3.medium` Linux On-Demand | `$0.0528/hour` | `$0.42` | `$3.80` | `$38.54` |
| 3 PrivateLink interface endpoints | `$0.030/hour` total (AWS published example rate `$0.01` per endpoint ENI-hour) | `$0.24` | `$2.16` | `$21.90` |
| 20 GiB gp3 root volume | `$1.92/month` | `$0.02` | `$0.19` | `$1.92` |
| ECR storage assumption | up to `$0.10/month` for 1 GiB | `<$0.01` | `$0.01` | `$0.10` |
| **Estimated total** | **about `$0.185/hour` plus ECR storage** | **`$1.48`** | **`$13.36`** | **`$135.46`** |

The per-hour estimate is approximately `$0.10` EKS + `$0.0528` EC2 + `$0.030`
interface endpoints + `$0.00263` for 20 GiB gp3. Singapore `t3.medium` and
gp3 rates are from the current regional price catalog; the EKS standard
support price and the `$0.01` endpoint ENI-hour example are from AWS pricing.
AWS PrivateLink also charges per processed GB. Confirm the endpoint rate in the
AWS Pricing Calculator before apply. The S3 gateway endpoint has no additional
charge.

This estimate excludes taxes, data processing beyond the assumed small image
pull, internet data transfer, image storage beyond 1 GiB, snapshots, resource
replacement overlap during node-group updates, and any separately created
resources. No free-tier or credit eligibility is assumed. EKS, EC2, and
interface endpoints continue billing until deleted; stopping the application
does not stop those charges. The final pre-apply estimate must be refreshed in
the AWS Pricing Calculator and reconciled with the Terraform plan.

Pricing references:

- [Amazon EKS pricing](https://aws.amazon.com/eks/pricing/) — standard and extended cluster-hour prices.
- [AWS regional instance and EBS catalog for `ap-southeast-1`](https://aws-pricing.com/ap-southeast-1.html) — `t3.medium` and gp3 rates; confirm against AWS Pricing Calculator.
- [AWS PrivateLink pricing](https://aws.amazon.com/privatelink/pricing/) — endpoint-hour and per-GB processing charges.
- [Amazon ECR pricing](https://aws.amazon.com/ecr/pricing/) — private image storage charges.
- [Amazon EKS Kubernetes versions](https://docs.aws.amazon.com/eks/latest/userguide/kubernetes-versions.html) — support status; regional availability is checked with the AWS CLI below.

## Operator prerequisites

Before planning, the operator must provide:

1. Current workstation public IPv4 address as a `/32` for
   `allowed_operator_cidrs`. Recheck this if the network changes.
2. IAM user or role ARN for the workstation's `kubectl` identity. It must be
   in the same AWS account selected by the AWS CLI profile.
3. Exact GitHub `owner/repository` name.
4. Existing account-level IAM OIDC provider ARN for
   `token.actions.githubusercontent.com`.
5. A protected GitHub environment named `sandbox`. Add the AWS publisher role
   ARN to it in the later image-publishing stage.
6. An AWS CLI profile with permissions to plan/read the target account and,
   only after plan approval, create the listed VPC, EKS, IAM, ECR, and endpoint
   resources.

Check the region's version and AZs:

```sh
aws eks describe-cluster-versions --region ap-southeast-1 \
  --query 'clusterVersions[?clusterVersion==`1.36`].[clusterVersion,status,defaultPlatformVersion]' \
  --output table
aws ec2 describe-availability-zones --region ap-southeast-1 \
  --filters Name=state,Values=available \
  --query 'AvailabilityZones[].ZoneName' --output table
aws ec2 describe-instance-type-offerings --region ap-southeast-1 \
  --location-type availability-zone \
  --filters Name=instance-type,Values=t3.medium \
  --query 'InstanceTypeOfferings[].Location' --output table
aws sts get-caller-identity
```

`1.36` must appear with `STANDARD_SUPPORT`. The final regional check, not a
hard-coded example, controls whether this version is used. Confirm that
`t3.medium` is offered in the selected worker AZ before finalizing the node
choice.

Create a local input file from `sandbox.tfvars.example` and replace every
placeholder. The file is ignored by Git. Get the workstation IP with
`curl -4 https://checkip.amazonaws.com`, then append `/32` to the returned
address. Do not put credentials or tokens in this file.

## Terraform workflow

Run from the repository root:

```sh
cp infra/terraform/sandbox/sandbox.tfvars.example infra/terraform/sandbox/sandbox.auto.tfvars
terraform -chdir=infra/terraform/sandbox init
terraform -chdir=infra/terraform/sandbox fmt -check
terraform -chdir=infra/terraform/sandbox validate
terraform -chdir=infra/terraform/sandbox plan -out=tfplan
terraform -chdir=infra/terraform/sandbox show -json tfplan | python3 infra/terraform/sandbox/scripts/check-plan.py
terraform -chdir=infra/terraform/sandbox show tfplan
```

Review every planned resource, the selected AZs/add-on versions, public API
CIDRs, endpoint placement and SG rules. The plan guard must pass and the
complete plan must contain no RDS, MSK, NAT Gateway, ALB, or WAF resources.
Plan and state files stay local and are ignored by Git. A Terraform plan does
not create billable resources, but it reads AWS account metadata.

Back up state after apply and before any destroy. Store an encrypted copy
outside the repository on an access-controlled volume:

```sh
umask 077
gpg --symmetric --cipher-algo AES256 \
  --output /secure/off-repository/path/device-ingestion-sandbox.tfstate.gpg \
  infra/terraform/sandbox/state/terraform.tfstate
```

Keep the encryption passphrase separately from the backup. Test that the backup
decrypts to a temporary file and that `terraform state list` can read it before
destroying resources. If the working state is missing or corrupt, freeze all
Terraform applies, restore the encrypted backup, inspect the inventory, then
run `terraform state list` and a reviewed plan. If there is no valid backup,
import existing AWS resources individually; never run an empty-state apply.

Only after the plan, cost worksheet and resource list have been reviewed and
explicit approval is recorded may the operator run:

```sh
terraform -chdir=infra/terraform/sandbox apply tfplan
```

After an approved apply, save the non-secret resource inventory outside the
repository, record the exact EKS and add-on versions, and run:

```sh
terraform -chdir=infra/terraform/sandbox show -json | python3 infra/terraform/sandbox/scripts/resource-inventory.py > /secure/off-repository/path/device-ingestion-sandbox-inventory.json
terraform -chdir=infra/terraform/sandbox output -json eks_addon_versions
aws eks update-kubeconfig --region ap-southeast-1 --name device-ingestion-sandbox
kubectl get nodes -o wide
kubectl describe nodes
kubectl get pods -A -o wide
kubectl get events -A --sort-by=.lastTimestamp
```

Confirm `.status.allocatable`, system Pod requests, and that the app request fits
with headroom before moving to Stage 5.

## Destroy procedure

First remove any application workload, then preserve and encrypt the state.
Produce and review a destroy plan:

```sh
terraform -chdir=infra/terraform/sandbox plan -destroy -out=destroy.tfplan
terraform -chdir=infra/terraform/sandbox show destroy.tfplan
```

Review exact deletions and any resources not managed by this state. Wait for
explicit approval. Only then execute the reviewed plan:

```sh
terraform -chdir=infra/terraform/sandbox apply destroy.tfplan
```

Retain state and the resource inventory until AWS checks confirm the EKS
cluster and node group, EC2 instances, EBS volumes, VPC endpoints, ECR images,
public IPs, and any unexpected load balancers or log groups are gone. ECR
repository retention/deletion must be reviewed separately; do not remove images
silently. Billing records may post after deletion. Do not delete state before
resource absence has been verified.
