## ADDED Requirements

### Requirement: Validate pull requests without deployment credentials

The repository SHALL run the configured source, Terraform, and Helm validation checks for pull requests without granting the pull request workflow AWS deployment credentials or Kubernetes access and without publishing or deploying an image.

#### Scenario: Pull request validation succeeds
- **WHEN** a pull request is opened or updated
- **THEN** the configured validation checks run and report a status usable by repository branch protection

#### Scenario: Pull request validation cannot deploy
- **WHEN** validation runs for a pull request event
- **THEN** the run does not assume an AWS role, publish to ECR, modify an EKS cluster, or deploy to staging

### Requirement: Provide bounded diagnostics for staging deployment failures

The staging deployment workflow SHALL emit bounded Kubernetes diagnostics for application rollout and smoke-check failures without printing Kubernetes Secret objects or secret values.

#### Scenario: Application rollout fails
- **WHEN** the Helm upgrade or application Deployment rollout fails
- **THEN** the workflow reports relevant Deployment and Pod status, selected pod descriptions and logs, and recent namespace events before failing

#### Scenario: Smoke Job fails
- **WHEN** the staging smoke Job fails
- **THEN** the workflow reports that Job's status, pod description and logs, and relevant recent namespace events before failing

#### Scenario: Diagnostics avoid secret disclosure
- **WHEN** the workflow collects failure diagnostics
- **THEN** it does not dump Kubernetes Secret objects, secret-bearing rendered manifests, or unrestricted environment values
