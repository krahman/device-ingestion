# AWS staging deployment

This guide provisions an isolated staging stack for the device ingestion
service. Terraform creates the network, Application Load Balancer (ALB), Amazon
MSK cluster, RDS PostgreSQL instance, ECR repository, ECS Fargate service,
CloudWatch logs, and GitHub Actions deployment role. GitHub Actions builds an
immutable image for each commit, initializes Kafka topics, applies database
migrations, deploys the service, and runs protocol and persistence checks.

The ALB is the only public application entry point. It routes HTTPS traffic to
the HTTP and OCPI API, upgrades WebSocket connections for OCPP 1.6J, and routes
gRPC requests using the gRPC content type:

| Service | Public endpoint | Container port |
| --- | --- | ---: |
| HTTP and OCPI 2.2.1 | `https://api.<base-domain>` | 8080 |
| OCPP 1.6J WebSocket | `wss://ocpp.<base-domain>/ocpp/<station-id>` | 8081 |
| gRPC | `grpc.<base-domain>:443` with TLS | 50051 |

## Prerequisites

- An AWS account and a region with at least two availability zones.
- AWS CLI credentials with permission to create the listed networking, ECS,
  ECR, MSK, RDS, IAM, Secrets Manager, KMS, and CloudWatch resources.
- Terraform 1.6 or newer and Docker for local validation/builds.
- An ACM certificate in the selected region covering `api`, `ocpp`, and `grpc`
  under the chosen staging domain.
- An existing GitHub Actions OIDC provider in the AWS account.
- A GitHub repository and a protected GitHub environment named `staging`.
- DNS control for the selected domain. Create records for all three hostnames
  pointing to the ALB DNS name after Terraform creates it. DNS hosting is not
  managed by this configuration.

The example values target `ap-southeast-1` and are placeholders. Replace them
with values for your account and DNS domain before planning.

## Create application secrets

Create these Secrets Manager secrets before applying Terraform. Terraform only
stores the secret ARNs in its configuration; it does not create or populate
the station, partner, or MSK passwords.

1. Create a customer-managed symmetric KMS key in the same region/account for
   the MSK SCRAM secret. The key policy must allow MSK to use the key.
2. Create an MSK secret whose name begins with `AmazonMSK_`, encrypted with
   that KMS key. Its secret value must be JSON with `username` and `password`
   fields. Use a unique, strong password. MSK SCRAM authentication uses
   `SCRAM-SHA-512` over TLS.
3. Create an OCPP stations secret containing the JSON array expected by
   `OCPP_STATIONS`. Include at least one station with credentials, complete
   location metadata, and one connector so the deployment smoke check can
   exercise OCPP and database persistence.
4. Create an OCPI partner secret containing the JSON array expected by
   `OCPI_EMSP_PARTNERS`. Include a valid staging eMSP party and token.

Keep secret values out of `terraform.tfvars`, source control, and workflow
variables. Terraform creates an RDS PostgreSQL master password in Secrets
Manager and injects its credentials into ECS tasks. The database connection
uses `sslmode=verify-full` with the container's system CA bundle.

## Provision infrastructure

From the repository root:

```sh
cd deploy/aws/staging
cp terraform.tfvars.example terraform.tfvars
```

Edit `terraform.tfvars` with the AWS region, two availability zones, certificate
ARN, base domain, GitHub owner/repository, OIDC provider ARN, secret ARNs, KMS
key ARN, and CPO country/party identifiers. Keep this file local; `.gitignore`
excludes it.

Initialize and review the plan before creating resources:

```sh
terraform init
terraform fmt -check -recursive
terraform validate
terraform plan -out=staging.tfplan
terraform apply staging.tfplan
```

The first apply creates the infrastructure with the ECS service at zero tasks.
The Terraform task definition refers to a `bootstrap` image tag that is not
published; GitHub Actions registers real task definitions with the commit SHA
image and starts the service during its first deployment. The stack includes
ongoing charges for MSK brokers, RDS, a NAT gateway, and the ALB.

Record the Terraform outputs needed for GitHub configuration:

```sh
terraform output -raw github_deploy_role_arn
terraform output -raw ecs_cluster_name
terraform output -raw ecs_service_name
terraform output -raw app_security_group_id
terraform output -json private_subnet_ids
terraform output -raw alb_dns_name
terraform output -json https_endpoints
```

The database, MSK cluster, and ECS tasks are in private subnets. Private tasks
use the NAT gateway for image pulls and AWS API calls. Security groups restrict
broker and database access to the service tasks.

The ALB waits for the configured deregistration delay (60 seconds by default)
before removing a task from service. OCPP connections can still be interrupted
when the task exits; stations should reconnect after a deployment.

## Configure GitHub Actions

In the repository's **Settings → Environments**, create `staging`, restrict
deployment branches to `main`, and add:

| Kind | Name | Value |
| --- | --- | --- |
| Environment secret | `AWS_DEPLOY_ROLE_ARN` | `github_deploy_role_arn` Terraform output |
| Environment variable | `AWS_REGION` | Terraform region |
| Environment variable | `ECS_PRIVATE_SUBNET_IDS` | Comma-separated `private_subnet_ids` output |
| Environment variable | `ECS_APP_SECURITY_GROUP` | `app_security_group_id` output |
| Environment variable | `ECS_DESIRED_COUNT` | Optional; defaults to `1` |

The IAM trust policy accepts only this repository's `staging` environment OIDC
subject. The workflow receives short-lived AWS credentials; do not add static
AWS access keys. A push to `main` or a manual workflow dispatch runs validation
and deployment.

## Deploy and verify

The workflow publishes an image tagged with the commit SHA. It then runs the
Kafka topic initializer and migration task in sequence. A non-zero exit from
either task fails the workflow before it updates the ECS service. After ECS is
stable, the smoke task checks readiness, OCPI version discovery and Locations,
HTTP and gRPC telemetry ingestion, OCPP BootNotification/StatusNotification/
MeterValues, and corresponding PostgreSQL persistence.

Check the service and target health with the AWS CLI:

```sh
aws ecs describe-services --cluster device-ingestion-staging \
  --services device-ingestion-staging
aws elbv2 describe-target-health --target-group-arn <target-group-arn>
```

`/health/live` reports process liveness and `/health/ready` checks database and
Kafka availability. `/metrics` is deliberately blocked at the public ALB. Use
CloudWatch log group `/ecs/device-ingestion/staging` for application and
one-off task logs; MSK broker logs are in `/aws/msk/device-ingestion/staging`.

## Roll back and tear down

To roll back an application release, select a prior SHA-tagged image, register
the previous ECS task definition, and update the service to that revision.
Keep schema changes additive and compatible with the previous app revision;
the workflow does not automatically reverse database migrations.

To remove the entire staging stack, first preserve any logs or database data
needed for investigation, then review and explicitly run:

```sh
terraform plan -destroy
terraform destroy
```

Destroying the stack deletes the RDS instance without a final snapshot and
removes its data. Remove the externally managed ACM certificate, DNS records,
OIDC provider, KMS key, and application secrets separately when they are no
longer needed.
