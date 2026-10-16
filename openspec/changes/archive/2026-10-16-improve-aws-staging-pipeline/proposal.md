## Why

The existing staging workflow validates and deploys from `main`, but pull requests do not receive the same repository checks before merge. Deployment failures in Helm rollout and smoke stages also provide less cluster context than the setup Job failures, making staging issues slower to diagnose. Improving those paths will make the current EKS pilot safer to change and easier to operate.

## What Changes

- Run the repository validation suite for pull requests without AWS credentials or deployment side effects.
- Keep AWS staging deployment restricted to the existing authorized `main` and manual paths, with validation completed first and staging deployment serialized.
- Add failure diagnostics for rollout and smoke failures using relevant Kubernetes pod, Job, event, and log details while avoiding secret output.
- Document the checks and required GitHub branch/environment protections for the pipeline.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `aws-staging-deployment`: Require pre-merge validation isolated from AWS deployment credentials and actionable, secret-safe diagnostics when a staging deployment fails.

## Impact

- `.github/workflows/deploy-staging.yml` and potentially a separate pull request validation workflow.
- `docs/aws-staging-deployment.md` for GitHub branch protection and staging environment setup.
- `openspec/specs/aws-staging-deployment/spec.md` through a delta spec. The current baseline still mentions the earlier ECS implementation; the active `deploy-kubernetes-pilot` change carries the EKS replacement. This proposal targets only the workflow improvements and will avoid silently rewriting that unrelated baseline history.
