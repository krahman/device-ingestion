## Context

The repository contains a Go service with HTTP/OCPI, optional OCPP WebSocket, and gRPC listeners. Docker Compose provides local Kafka and PostgreSQL; a Minikube profile deploys the existing Helm chart with local dependencies and Kubernetes credentials. AWS staging is the only cloud deployment implemented: Terraform provisions the VPC, EKS and supporting AWS services, while a GitHub Actions workflow publishes an image and deploys it. There are no Azure deployment manifests, Terraform modules, or workflows in the repository.

The requested artifact is a source-grounded guide at `docs/deployment-architecture.md`. It must separate implemented facts from future Azure design, and it must not make code or cloud configuration changes.

## Goals / Non-Goals

**Goals:**

- Explain the development runtime and the paths through which Kafka events are stored in PostgreSQL.
- Describe Compose and Minikube configuration, including local-only plaintext settings and test credentials.
- Document the implemented AWS resource ownership, prerequisites, and GitHub Actions release sequence.
- Present an explicitly proposed Azure design, side-by-side mappings, setup ownership, and open decisions.
- Link claims to repository files and official Azure documentation where a cloud product behavior is referenced.
- Document pilot assumptions and current deployment limitations.

**Non-Goals:**

- Implement or claim support for Azure deployment.
- Change application behavior, Helm templates, Terraform, workflows, or cloud resources.
- Select final Azure services, networking exposure, production availability, or data-retention policy.
- Duplicate every operational command from the existing Compose, Minikube, and AWS guides.

## Decisions

### Put the cross-environment view in one source-linked guide

Use `docs/deployment-architecture.md` as the entry point for development, implemented AWS staging, and proposed Azure deployment. Keep operational step-by-step procedures in `docs/minikube-deployment.md` and `docs/aws-staging-deployment.md`; link to them rather than copying their full procedures. This preserves a compact architecture view while leaving detailed runbooks authoritative in their existing locations.

### Label implementation status at every cloud boundary

Start with an implementation-status summary. Label AWS as implemented in repository code, Minikube/Compose as local paths, and Azure as proposed/not implemented. Use ownership labels for Azure requirements: **operator-managed**, **would be provisioned by future IaC**, and **decision required**. Do not imply resources have been created in any cloud subscription.

### Ground AWS setup ownership in Terraform and workflow behavior

Describe resources in `deploy/aws/staging` as Terraform-managed only when those resources exist in the configuration. Identify the ACM certificate, GitHub OIDC provider, DNS records, application secrets, cluster add-ons, namespace bootstrap, and GitHub environment values according to the current guide and workflow. The AWS Load Balancer Controller and Secrets Store CSI components are operator-installed; the workflow deploys the application only after the namespace and add-ons are ready.

### Treat Azure mappings as candidates, not an approved bill of materials

Map EKS to AKS, ECR to ACR, RDS PostgreSQL to Azure Database for PostgreSQL Flexible Server, Secrets Manager to Key Vault, and GitHub AWS OIDC to Azure federated identity/RBAC as candidate equivalents. Present the Event Hubs Kafka endpoint only as an option requiring compatibility review: the repository's Kafka security adapter supports TLS and SCRAM-SHA-256/512, while Event Hubs uses its own Kafka endpoint authentication/configuration and does not implement a native Kafka cluster. Keep a Kafka-compatible managed service or native Kafka deployment as alternatives pending a decision. Link Microsoft's documentation for the protocol and authentication constraints.

### Draw two sequence diagrams with different status

The AWS diagram will reflect the current workflow: push/manual trigger, validation, OIDC role assumption, immutable image publication, EKS kubeconfig, config/SecretProviderClass application, sequential Kafka-init and migration Jobs, Helm rollout, and in-cluster protocol/persistence smoke Job. The Azure diagram will be explicitly titled proposed and use future CI identity, registry, cluster, setup jobs, rollout, and smoke-check steps. Avoid presenting infrastructure provisioning as part of every application deployment; Terraform is a separate operator workflow today.

### Inventory cloud-side prerequisites by owner and permission

Use grouped checklists or tables for AWS and proposed Azure. Each row states whether the platform/Terraform/workflow creates or consumes it, what an operator must do, and relevant values/permissions. Link rows to the implementation files. For proposed Azure rows, state the required identity scopes and network/security decisions without inventing subscription IDs, regions, names, quotas, or exact SKU values.

## Risks / Trade-offs

- [Documentation can drift from deployment code] → Include repository-relative file links and keep the guide focused on architecture and resource ownership, with detailed procedures linked to existing guides.
- [Cloud service names may imply feature parity] → State that the Azure mapping is proposed and explicitly surface Kafka authentication/protocol review and ingress differences.
- [AWS prerequisites span Terraform and manual setup] → Separate Terraform-created, workflow-created, and operator-managed items in the inventory.
- [A single page can become long] → Use concise sectioning, tables, and Mermaid diagrams; avoid duplicating commands and verbose source excerpts.
- [Azure service behavior can change] → Link to official Microsoft documentation and avoid relying on undocumented defaults.

## Migration Plan

1. Add `docs/deployment-architecture.md` with development architecture, actual AWS architecture and sequence, AWS setup inventory, proposed Azure mapping and sequence, Azure setup inventory, and open decisions.
2. Cross-check every implemented claim against the source or configuration and every proposed Azure claim against official Microsoft documentation.
3. Validate Markdown structure, Mermaid blocks, repository links, and OpenSpec artifacts. No deployment or cloud migration is performed.

## Open Questions

- None block the documentation proposal. The guide will preserve unresolved Azure choices as explicit decision items, especially Kafka compatibility/authentication, ingress/TLS, cluster/API exposure, PostgreSQL backup/retention, Key Vault secret delivery, and cluster access authorization.
