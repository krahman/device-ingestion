## Context

The service is a Go container that listens for HTTP/OCPI on port 8080, gRPC on 50051, and OCPP WebSockets on 8081 when stations are configured. It requires Kafka and PostgreSQL at startup. The image already contains the server, Kafka topic initializer, migration tool, and staging smoke checker. Liveness and readiness endpoints are available on the HTTP listener. The archived staging implementation provisions ECS, ECR, MSK, RDS, networking, and an ALB; no Kubernetes cluster is identified in the repository.

This is a Kubernetes implementation pilot on AWS. The design assumes this change provisions an EKS cluster and its pilot node capacity, because the repository does not describe a shared cluster. MSK, RDS, ECR, and the existing VPC/dependency design remain managed AWS services.

## Goals / Non-Goals

**Goals:**
- Deploy the existing service image on EKS and establish a repeatable GitHub Actions release path.
- Route HTTPS HTTP/OCPI, OCPP WebSocket, and gRPC traffic to their existing listeners.
- Run Kafka topic initialization and database migrations as ordered Kubernetes Jobs before application rollout.
- Keep AWS credentials and application secrets out of source control and container images.
- Keep the pilot small, with one application replica by default and documented teardown.

**Non-Goals:**
- Production availability guarantees, autoscaling, multi-region recovery, or a cost-optimized long-lived cluster.
- Splitting the combined Go process into separate protocol or worker services.
- Replacing MSK, RDS, or ECR with in-cluster services.
- Making the chart support non-AWS clusters in this pilot.

## Decisions

### Provision an EKS pilot cluster in the staging VPC

Extend the AWS staging Terraform to provision EKS control-plane access, private worker capacity, cluster IAM, and the network permissions required to reach MSK and RDS. Keep worker nodes in private subnets and public ingress at the existing ALB boundary. Use a small managed node group sized through variables, with a single app replica as the pilot default.

**Alternative:** Deploy to a pre-existing shared cluster. That would be simpler, but no cluster or platform conventions are present in this repository. The assumption should be revisited if the platform team supplies a cluster before implementation.

### Package the workload as a Helm chart

Add a chart for the Deployment, Service, configuration references, probes, and protocol ingress. Keep environment-specific values separate from templates and inject the immutable image tag from the deployment workflow. HTTP and OCPP use HTTP/HTTPS ALB routing with WebSocket support; gRPC uses an ALB gRPC target group and host rule. Preserve the OCPP idle timeout and expose only configured listeners.

**Alternative:** Raw manifests or Kustomize would work, but the pilot needs a concise way to vary image, resource sizing, hostnames, certificate, and secret references without duplicating resources.

### Keep credentials in AWS Secrets Manager and project them into the pod

Use the AWS Secrets and Configuration Provider for the Secrets Store CSI Driver, with EKS workload identity granting the application service account access only to its required secrets. Configure synchronized Kubernetes Secret values for the existing environment-variable interface. Keep non-secret settings in Helm values or ConfigMaps. Do not put secret values in Terraform variables that enter state, Helm values, or workflow logs.

**Alternative:** Manually created Kubernetes Secrets are adequate for a throwaway local cluster, but do not provide the AWS-backed secret lifecycle expected from the staging pilot.

### Run setup work as Kubernetes Jobs before rollout

Use the same immutable image for a Kafka topic initialization Job and a migration Job. The deployment workflow waits for both Jobs to complete successfully before upgrading the Helm release, then waits for Deployment readiness and runs the existing protocol smoke checks. Job names or release revisions must allow a new image to run on each deployment.

**Alternative:** Init containers would repeat topic and migration work for every replica and couple app startup to schema mutation. A deployment pipeline makes ordering and failure visible.

### Configure probes and termination for the current process lifecycle

Use HTTP liveness and readiness probes on port 8080. Set startup delay and probe periods to tolerate dependency initialization. Use a 65-second pre-stop delay to let the ALB's 60-second target deregistration drain, then allow the application to close listeners within its 10-second shutdown limit. Start with one replica because rolling replacement interrupts OCPP WebSocket sessions and each replica adds Kafka consumers and database connections.

## Risks / Trade-offs

- [EKS adds control-plane, node, load-balancer, and networking cost to a pilot] → Make node sizing explicit, use one application replica, and document teardown and ongoing charges.
- [ALB ingress behavior differs across HTTP, WebSockets, and gRPC] → Configure separate host rules/target protocols and verify each protocol through the deployed endpoint.
- [Secrets Store CSI synchronization and workload identity add cluster components] → Pin and document the required add-ons and grant the workload identity access only to named secrets.
- [A single replica has downtime during replacement and may drop OCPP sessions] → Use readiness-based rollout and connection draining; document charger reconnect expectations.
- [EKS access and GitHub OIDC permissions are easy to over-broaden] → Restrict the deploy role to the repository/environment and bind Kubernetes deployment rights to a dedicated namespace/service identity.

## Migration Plan

1. Replace ECS cluster/service/task definitions and ECS workflow deployment steps with EKS and Kubernetes resources while retaining ECR, MSK, RDS, and required network/security resources.
2. Install or provision the AWS Load Balancer Controller and Secrets Store CSI integration with workload identity.
3. Deploy the chart using the immutable image, run topic initialization and migrations, then wait for the app rollout.
4. Validate HTTP telemetry, gRPC telemetry, OCPP WebSocket behavior, OCPI endpoints, Kafka delivery, and PostgreSQL persistence.
5. Roll back application releases by deploying a previous image tag and chart values; keep migrations additive and backward-compatible.
6. Tear down pilot cluster and staging resources only through an explicit reviewed Terraform destroy.

## Open Questions

- Will the platform team provide an EKS cluster or require this project to provision and own the pilot cluster? This proposal assumes project-provisioned EKS.
- Which Kubernetes version, ingress-controller ownership model, and AWS secrets add-on versions are approved by the platform team?
- Which AWS account, region, domain, ACM certificate, and pilot resource budget will be used?
