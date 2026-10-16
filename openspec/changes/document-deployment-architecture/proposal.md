## Why

The repository now has a working AWS EKS pilot and a local Minikube path, while Azure deployment remains unimplemented. A source-grounded architecture guide will give developers and operators one place to understand the runtime, follow the AWS deployment sequence, and see what must be decided and provisioned before Azure support can be built.

## What Changes

- Add `docs/deployment-architecture.md` describing development, Docker Compose, Minikube, and the implemented AWS EKS deployment.
- Add an AWS deployment sequence diagram and group AWS prerequisites by responsibility, including whether Terraform, GitHub Actions, or an operator supplies each item.
- Describe Azure as a proposal only, with a separate proposed deployment sequence, possible service mappings, setup inventory, and unresolved decisions.
- Identify pilot limitations and source documentation gaps without changing application code, infrastructure, workflows, or deployment behavior.

## Capabilities

### New Capabilities

- `deployment-architecture-documentation`: A source-linked guide distinguishes implemented development and AWS deployment behavior from proposed Azure architecture and lists required cloud-side setup and open decisions.

### Modified Capabilities

None.

## Impact

- Adds `docs/deployment-architecture.md`.
- Adds a documentation-only OpenSpec capability and tasks.
- References existing source, Docker Compose, Minikube, Helm, Terraform, GitHub Actions, and deployment guide files. No code, API, infrastructure, workflow, or dependency changes are in scope.
