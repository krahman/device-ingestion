## 1. Add credential-free pull request validation

- [x] 1.1 Configure pull request events to run the existing Go formatting, test, build, Terraform validation, and Helm lint/render checks.
- [x] 1.2 Keep AWS OIDC permissions, ECR publishing, EKS access, and staging deployment unavailable to pull request runs; verify workflow event conditions and permissions.

## 2. Improve staging failure diagnostics

- [x] 2.1 Add bounded diagnostics for Helm upgrade and Deployment rollout failures, including selected status, pod descriptions/logs, and recent events without secret objects or values.
- [x] 2.2 Add equivalent bounded diagnostics for failed smoke Jobs while retaining current Kafka-init and migration Job logs.

## 3. Document and verify pipeline controls

- [x] 3.1 Document the validation status check, recommended branch protection, protected staging environment settings, and which controls require GitHub administrator configuration.
- [x] 3.2 Validate workflow syntax and confirm the pull request path cannot execute AWS deployment steps; review diagnostic commands for secret-safe output.
