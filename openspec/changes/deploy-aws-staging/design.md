## Context

The Go service currently runs as one container with HTTP/OCPI on `:8080`, OCPP WebSockets on `:8081` when stations are configured, and gRPC on `:50051`. It publishes to Kafka and consumes events into PostgreSQL. `Dockerfile` builds the server and migration binaries; `compose.yaml` is for local development. The Kafka adapters currently use plaintext connections, the HTTP server has no readiness or liveness routes, and the repository has no infrastructure-as-code or GitHub Actions deployment workflow.

Staging must exercise all three public protocols and the same Kafka-to-PostgreSQL path planned for production while keeping the environment independently deployable and removable.

## Goals / Non-Goals

**Goals:**
- Provision an isolated AWS staging environment from Terraform.
- Run the existing container on ECS Fargate behind an HTTPS Application Load Balancer, routing HTTP/OCPI, OCPP WebSocket, and gRPC traffic to their existing ports.
- Provide private Kafka and PostgreSQL services and secure application credentials.
- Deploy from GitHub Actions using short-lived AWS credentials obtained through OIDC.
- Apply schema migrations before an app rollout and verify the deployed service after rollout.

**Non-Goals:**
- Production high availability, autoscaling policy, multi-region recovery, or production cost optimization.
- Replacing Kafka with another event service or changing OCPP/OCPI protocol behavior.
- Provisioning a custom domain or certificate automatically when the staging account's DNS ownership is not known. The certificate ARN and optional DNS zone are inputs.
- Managing application partner/station data outside the existing environment-variable configuration model.

## Decisions

### Terraform manages a dedicated staging stack

Place Terraform configuration in a dedicated deployment directory with environment inputs for AWS region, VPC CIDRs/subnets, certificate ARN, desired task count, and resource sizing. Provision an ECS cluster/service and task definition, ECR repository, ALB and three target groups, MSK, RDS PostgreSQL, security groups, Secrets Manager entries, CloudWatch log group, and IAM roles. Use private subnets for ECS tasks, MSK, and RDS; only the ALB is internet-facing. Require explicit `terraform destroy` to remove the environment.

**Alternative:** AWS CDK would also be viable, but Terraform is selected for its declarative, provider-neutral infrastructure workflow and ease of local plan review. Keep provider resources modular enough to replace this choice later.

### One ECS service exposes the current listener ports

Use an ECS Fargate service with `awsvpc` networking and IP target groups. Configure ALB HTTPS routing for HTTP/OCPI on 8080, OCPP WebSockets on 8081, and gRPC on 50051 with an ALB gRPC target group. Use the supplied ACM certificate on the HTTPS listener. Configure the ALB idle timeout above the configured OCPP heartbeat interval. Start staging at desired count one; task replacement may briefly interrupt OCPP sessions, and chargers must reconnect.

**Alternative:** Split HTTP, OCPP, and gRPC into separate services. That adds deployment and scaling complexity without current independent binaries or scaling requirements.

### MSK uses TLS and SASL/SCRAM for the initial staging integration

Add Kafka client configuration for TLS and SASL/SCRAM credentials, with credentials delivered to ECS from Secrets Manager. Use a currently supported MSK Kafka release (3.9.0 in this staging configuration). Include a `kafka-init` one-off task in the application image; GitHub Actions runs it before migrations and service rollout to create the required topics idempotently. This keeps Kafka private and avoids coupling topic creation to a GitHub runner that cannot reach the VPC. The pinned `kafka-go` version provides SCRAM and TLS support; its additional SCRAM dependencies are vendored.

**Alternative:** MSK IAM authentication avoids static broker credentials but requires IAM SASL/OAUTHBEARER signing support in every Kafka reader and writer. Revisit after staging is functional.

### RDS PostgreSQL remains private and TLS-enabled

Use RDS for PostgreSQL in private subnets with encryption and automated staging backups. Configure the application database URL with TLS verification. Inject the URL from Secrets Manager and permit inbound database traffic only from the ECS task security group.

### Health checks distinguish process health from dependency readiness

Add liveness and readiness endpoints on the existing HTTP listener. Liveness indicates that the process can continue running. Readiness indicates the service has initialized and can accept traffic, including required startup dependencies. Configure ALB target health checks against readiness and ECS container health against liveness. Keep `/metrics` private or restrict its exposure through ALB rules/security policy.

### GitHub Actions performs an ordered, immutable deployment

On pushes to `main` and manual dispatch, the workflow checks out the source, runs formatting/build/test checks, builds the Docker image, tags and pushes it using the commit SHA, and assumes a narrowly scoped AWS deployment role through GitHub OIDC. It registers immutable-image task definition revisions, runs the Kafka topic initializer and migration binary as one-off ECS tasks, waits for successful completion, deploys the service, waits for stability, then runs protocol smoke checks. The AWS role trust policy is restricted to this repository and the staging GitHub environment; no long-lived AWS keys are stored in GitHub.

**Alternative:** Build locally and deploy with the AWS CLI. This is useful during debugging but is not a repeatable shared staging path.

## Risks / Trade-offs

- [MSK authentication is not configurable in current adapters] → Add TLS/SASL configuration consistently to producers, consumers, and dead-letter writers; verify with an integration smoke test before enabling the service.
- [A single staging task has downtime during replacement] → Document charger reconnect behavior and use a longer deregistration/draining window; increase task count only when staging requires continuity.
- [Long-lived WebSockets may be closed during deploy or ALB idle timeout] → Set timeout above OCPP heartbeat and exercise reconnects during staging rollout.
- [Migration failure can block deployment] → Run migrations as a separate task, fail the workflow on non-zero exit, and retain the previous task definition for rollback.
- [Cloud resources incur ongoing cost] → Keep staging sizes and task count configurable, enable tags, and document teardown; avoid automatic destruction on workflow completion.
- [ALB and ECS listener configuration can be restrictive] → Validate HTTP, WebSocket upgrade, and gRPC health checks and routing in the deployed staging environment.

## Migration Plan

1. Add health endpoints and secure Kafka configuration while preserving local Compose defaults.
2. Add Terraform for staging and apply it to create network and managed dependencies, registry, load balancer, and ECS resources.
3. Create the GitHub OIDC role and repository/environment variables and secrets required by the workflow.
4. Build and push an initial image; provision Kafka topics and required secrets; run the migration task.
5. Deploy the ECS service and validate HTTP telemetry, gRPC telemetry, OCPP BootNotification/StatusNotification/MeterValues, OCPI Locations, and PostgreSQL persistence.
6. Roll back application releases by redeploying the previous immutable image/task definition. Roll back infrastructure changes with a reviewed Terraform plan; database migrations must remain backward-compatible with the previous image.
7. Tear down staging only with an explicit Terraform destroy after preserving any data needed for investigation.

## Open Questions

- Which AWS account, region, base domain, and ACM certificate ARN will staging use?
- What Kafka topic retention period should staging use? The current initializer relies on broker defaults.
