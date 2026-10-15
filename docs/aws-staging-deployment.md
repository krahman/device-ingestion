# AWS EKS staging pilot

This guide provisions an isolated AWS pilot for deploying device-ingestion on
Kubernetes. Terraform creates the VPC, EKS control plane and private managed
node group, ECR, MSK, RDS PostgreSQL, workload IAM role, GitHub OIDC deploy role,
and supporting security groups and logs. The AWS Load Balancer Controller
creates the public ALB from Kubernetes Ingress resources. The app keeps its
HTTP/OCPI, OCPP WebSocket, and gRPC listeners on ports 8080, 8081, and 50051.

The cluster uses one application replica by default. A 65-second pre-stop
delay gives the ALB's 60-second target deregistration time to drain before the
process receives its termination signal. Replacing a pod can still close OCPP
sessions; chargers should reconnect. The pilot does not provide production
availability or cost optimization.

## Prerequisites

- An AWS account and region with at least two availability zones.
- AWS CLI, Terraform 1.6 or newer, kubectl, Helm 3, and Docker.
- An ACM certificate in the selected region covering `api`, `ocpp`, and `grpc`
  under the staging domain, plus DNS control for those hostnames.
- An existing GitHub Actions OIDC provider and a protected GitHub environment
  named `staging`.
- Pre-created MSK SCRAM, OCPP station, and OCPI partner Secrets Manager secrets.
  The MSK SCRAM secret must use a customer managed KMS key.
- A Kubernetes version currently supported by EKS in the selected region. Set
  the required `kubernetes_version` input to the version approved by the
  platform team; EKS version availability can vary by region.
- Replace the example `cluster_endpoint_public_access_cidrs` with operator or
  self-hosted runner egress CIDRs. The sample placeholder is not deployable.

Terraform's bootstrap principal becomes the initial EKS cluster administrator.
Use that principal for the initial add-on installation and namespace creation.
GitHub Actions receives Kubernetes admin permissions only in the configured
application namespace.

## Provision the AWS resources

```sh
cd deploy/aws/staging
cp terraform.tfvars.example terraform.tfvars
```

Set the AWS region, availability zones, a currently available EKS Kubernetes
version for that region, certificate ARN, base domain, GitHub repository and
OIDC provider ARN, secret ARNs, and CPO identifiers in `terraform.tfvars`. This
file is ignored by Git. Review the sizing and Kubernetes version inputs before
applying.

```sh
terraform init
terraform fmt -check -recursive
terraform validate
terraform plan -out=staging.tfplan
terraform apply staging.tfplan
```

The EKS Kubernetes API endpoint is reachable publicly and privately from the
configured CIDR ranges; IAM authentication is required. Worker nodes remain in
private subnets and use the NAT gateway for image pulls and AWS APIs. ALB and
internal load balancer subnet tags are applied by Terraform.

Record the deployment values and GitHub role:

```sh
terraform output -raw eks_cluster_name
terraform output -raw github_deploy_role_arn
terraform output -raw application_role_arn
terraform output -json deployment_values
```

The `deployment_values` output contains connection addresses and secret ARNs,
not secret contents. Use its values to configure the GitHub staging
environment.

## Install cluster integrations

Install the AWS Load Balancer Controller and the Secrets Store CSI Driver with
the AWS Secrets and Configuration Provider (ASCP) before deploying the chart.
The application service role and secret-scoped IAM policy are created by
Terraform; the cluster add-ons themselves are installed by a cluster
administrator.

For the AWS Load Balancer Controller, follow the [AWS EKS installation guide](https://docs.aws.amazon.com/eks/latest/userguide/lbc-helm.html).
Create its dedicated IAM role using the official controller policy, associate
that role with the controller service account through EKS workload identity,
then install the Helm chart in `kube-system`:

```sh
helm repo add eks https://aws.github.io/eks-charts
helm repo update
kubectl create serviceaccount aws-load-balancer-controller -n kube-system
kubectl annotate serviceaccount aws-load-balancer-controller -n kube-system \
  eks.amazonaws.com/role-arn=<load-balancer-controller-role-arn>
helm upgrade --install aws-load-balancer-controller eks/aws-load-balancer-controller \
  --namespace kube-system \
  --set clusterName="$(terraform output -raw eks_cluster_name)" \
  --set region="$(terraform output -raw aws_region)" \
  --set vpcId="$(terraform output -raw vpc_id)" \
  --set serviceAccount.create=false \
  --set serviceAccount.name=aws-load-balancer-controller
```

The controller needs the tagged public subnets to create the internet-facing
ALB and permissions to manage target groups and worker security group rules.
The IAM role policy is maintained by the upstream controller project and is
not duplicated in this pilot Terraform stack.

Install the Secrets Store CSI Driver and AWS provider as described in the
[AWS ASCP EKS guide](https://docs.aws.amazon.com/secretsmanager/latest/userguide/ascp-eks-installation.html).
The chart uses `SecretProviderClass` to fetch Kafka SCRAM credentials, the RDS
master username and password, OCPP station configuration, and OCPI partner
tokens from Secrets Manager. The synced Kubernetes Secret is populated when a
pod mounts the CSI volume. The workload role can read only those four secrets.

The EKS add-on installs the AWS provider; install the upstream CSI driver first
and verify both components are running:

```sh
helm repo add secrets-store-csi-driver \
  https://kubernetes-sigs.github.io/secrets-store-csi-driver/charts
helm repo update
helm upgrade --install csi-secrets-store \
  secrets-store-csi-driver/secrets-store-csi-driver -n kube-system
aws eks create-addon --cluster-name "$(terraform output -raw eks_cluster_name)" \
  --addon-name aws-secrets-store-csi-driver-provider
kubectl get pods -n kube-system
```

Create the application namespace using the initial cluster administrator:

```sh
aws eks update-kubeconfig --name "$(terraform output -raw eks_cluster_name)" --region <aws-region>
kubectl create namespace device-ingestion-staging
```

If `application_namespace` was changed, create that namespace instead. GitHub's
EKS access policy is scoped to this namespace and cannot create it.

## Configure GitHub Actions

Create the protected `staging` environment and add `AWS_DEPLOY_ROLE_ARN` as an
environment secret. Add these environment variables using Terraform outputs
and the selected staging inputs:

| Name | Value |
| --- | --- |
| `AWS_REGION` | Terraform region |
| `EKS_CLUSTER_NAME` | `eks_cluster_name` output |
| `K8S_NAMESPACE` | `application_namespace` output |
| `APPLICATION_ROLE_ARN` | `application_role_arn` output |
| `DATABASE_HOST` | `deployment_values.db_host` |
| `DATABASE_SECRET_ARN` | `deployment_values.database_secret_arn` |
| `KAFKA_BROKERS` | `deployment_values.msk_brokers` |
| `MSK_SCRAM_SECRET_ARN` | `deployment_values.msk_scram_secret_arn` |
| `OCPP_STATIONS_SECRET_ARN` | `deployment_values.ocpp_stations_secret_arn` |
| `OCPI_PARTNERS_SECRET_ARN` | `deployment_values.ocpi_partners_secret_arn` |
| `CERTIFICATE_ARN` | `deployment_values.certificate_arn` |
| `BASE_DOMAIN` | `deployment_values.base_domain` |
| `CPO_COUNTRY_CODE` | `deployment_values.cpo_country_code` |
| `CPO_PARTY_ID` | `deployment_values.cpo_party_id` |
| `KAFKA_TOPIC_REPLICATION_FACTOR` | `2` |
| `OCPP_ENABLED` | `true` when station configuration is supplied; otherwise `false` |

The workflow assumes the AWS role through GitHub OIDC, pushes a commit-tagged
immutable image to ECR, and uses the EKS access entry scoped to the staging
namespace. It does not fetch secret contents into the GitHub runner.

## Deploy and verify

A push to `main` or manual workflow dispatch validates the Go code, Terraform,
and Helm chart, then builds and publishes the image. Deployment applies the
namespace workload configuration and `SecretProviderClass`, runs Kafka topic
initialization and database migration Jobs sequentially, upgrades the Helm
release only after both Jobs complete, waits for the Kubernetes Deployment to
be ready, and runs protocol and persistence smoke checks from a pod in the
cluster.

After the Ingress resources become ready, inspect the ALB hostname and create
DNS records for all three names:

```sh
kubectl get ingress -n device-ingestion-staging
```

| Service | Public endpoint | Container port |
| --- | --- | ---: |
| HTTP and OCPI | `https://api.<base-domain>` | 8080 |
| OCPP 1.6J WebSocket | `wss://ocpp.<base-domain>/ocpp/<station-id>` | 8081 |
| gRPC | `grpc.<base-domain>:443` with TLS | 50051 |

The chart blocks public access to `/metrics`. `/health/live` is used for
liveness; `/health/ready` checks PostgreSQL and Kafka before routing traffic.
Use `kubectl logs` for application and setup Job output, and CloudWatch EKS
control plane logs for API, audit, and authenticator activity.

## Roll back and tear down

Use `helm history device-ingestion -n device-ingestion-staging` and
`helm rollback device-ingestion <revision> -n device-ingestion-staging` to
restore a previous application release. Keep schema changes additive and
backward-compatible with the previous image; the workflow does not reverse
database migrations. A later deployment can also publish a prior source
revision using its immutable commit image tag.

To remove the pilot, uninstall the release and then review the Terraform
destroy plan:

```sh
helm uninstall device-ingestion -n device-ingestion-staging
cd deploy/aws/staging
terraform plan -destroy
terraform destroy
```

Destroying the stack deletes EKS, MSK, RDS, ECR, networking, and the NAT
gateway. RDS has no final snapshot configured. Preserve any data or logs needed
for investigation before teardown. Remove the externally managed ACM
certificate, DNS records, GitHub OIDC provider, KMS key, and application
secrets separately when they are no longer needed.
