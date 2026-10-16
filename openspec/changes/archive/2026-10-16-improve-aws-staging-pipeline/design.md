## Context

The current workflow in `.github/workflows/deploy-staging.yml` performs Go formatting/tests/build, Terraform validation, and Helm lint/render in a `validate` job, then deploys from `main` pushes or manual dispatch. Validation is not triggered for pull requests. The deployment job already uses a protected-environment context named `staging`, GitHub OIDC, immutable commit-SHA image tags, a deployment concurrency group, sequential Kafka and migration Jobs, a Helm rollout, and protocol/persistence smoke checks. Setup Job failures emit their logs, while rollout and smoke failures do not consistently collect pod state, events, and logs. The AWS guide is `docs/aws-staging-deployment.md`.

The active `deploy-kubernetes-pilot` change captures the move from the earlier ECS implementation to EKS. This change builds on that current workflow and does not redesign the AWS infrastructure or application deployment model.

## Goals / Non-Goals

**Goals:**

- Run the same safe validation checks for pull requests, without AWS credentials, image publication, or cluster access.
- Preserve the existing authorized staging triggers and deployment ordering.
- Make Kubernetes rollout and smoke failures easier to investigate without printing secret values.
- Document the repository settings needed to make validation and staging protections effective.

**Non-Goals:**

- Change Terraform resources, EKS topology, application behavior, Helm resources, database migrations, or cloud permissions.
- Deploy pull request code to AWS or grant pull request workflows AWS OIDC permissions.
- Add automatic rollback. The deployment guide already describes Helm rollback, and database migration reversibility requires separate application policy.
- Promise branch protection or environment approval settings that can only be configured in GitHub repository settings.

## Decisions

### Add pull request validation without AWS access

Add a pull request trigger to the validation workflow, either by splitting the current validation job into a dedicated CI workflow or by adding a validation-only workflow trigger while keeping the deployment job gated to `push` on `main` and `workflow_dispatch`. Prefer a structure that makes the no-deploy boundary explicit and does not expose `id-token: write`, AWS credentials, ECR publication, or Kubernetes access to pull request runs. Reuse the current checks and avoid maintaining divergent validation commands.

**Alternative:** Run the full deployment workflow on pull requests and conditionally skip deploy steps. Rejected because future edits can accidentally activate credentialed steps for untrusted code.

### Preserve serialized staging deployments

Keep the existing concurrency group for the staging deploy job so concurrent runs do not race updates to the same Helm release. Require staging deployment checks to complete before credentials are assumed, and keep GitHub environment protection documented as an operator setting. Do not invent a new image promotion flow or deployment input without a separate requirement.

**Alternative:** Allow concurrent staging deploys. Rejected because two workflow runs could apply setup Jobs and Helm releases against the same namespace at once.

### Collect bounded, secret-safe diagnostics on failure

Add `if: failure()` diagnostic steps after rollout and smoke operations that report relevant resource status, pod descriptions, recent namespace events, and logs for the failed application or Job. Use explicit resource selectors/names and bounded output. Do not dump Secrets, rendered manifests containing secret data, all environment variables, or the entire namespace configuration. Keep the existing focused logs for Kafka and migration Job failures.

**Alternative:** Dump all Kubernetes resources and manifests. Rejected because that can expose credentials and produce unbounded, noisy logs.

### Document repository-side controls and limits

Update the AWS staging guide to state which checks should be required by branch protection and explain that GitHub environment reviewers/branch restrictions must be configured in repository settings. Describe the difference between a check implemented by YAML and an administrator setting; do not claim those settings are provisioned by Terraform.

## Risks / Trade-offs

- [Validation runs add CI minutes to pull requests] → Reuse existing checks and avoid AWS deployment work in the PR path.
- [Failure logs can contain application data] → Bound collection to selected resource diagnostics and never print Kubernetes Secret objects or secret-bearing rendered values.
- [Repository settings may remain unconfigured] → Document the required status check and protected `staging` environment settings, and state they are external prerequisites.
- [Workflow trigger edits may accidentally deploy pull request code] → Keep deployment permissions and job conditions explicit and verify the workflow event matrix during implementation.

## Migration Plan

1. Add validation-only pull request execution and confirm the pull request event cannot reach credentialed deployment steps.
2. Add bounded failure diagnostics for rollout and smoke failures.
3. Update the staging guide with status check and environment protection configuration.
4. Validate workflow syntax and local static commands, then open a pull request to observe the validation check.
5. Confirm staging environment protection and required checks in GitHub settings before relying on them for merges or deployments.

No infrastructure migration or rollback is required. If the workflow change causes deployment issues, revert the workflow/documentation changes; the existing immutable image and Helm history remain the operational rollback mechanisms.

## Open Questions

- Should pull request validation run for all pull requests or only changes that affect application, Terraform, chart, and workflow inputs? Prefer all pull requests initially unless CI cost data indicates a need to narrow it.
- What exact number of prior events/log lines is useful while keeping GitHub logs bounded? Select conservative limits during implementation.
- Which branch protection/ruleset and environment reviewer settings are available for this repository? These must be verified by an administrator in GitHub.
