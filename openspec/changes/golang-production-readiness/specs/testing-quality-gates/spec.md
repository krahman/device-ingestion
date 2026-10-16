## ADDED Requirements

### Requirement: Run automated Go tests in CI

Continuous integration SHALL build the service and run `go test ./...` on changes within the repository's supported Go environment. Tests that require PostgreSQL or Kafka SHALL use disposable local/CI dependencies and SHALL NOT require AWS resources.

#### Scenario: Unit test fails
- **WHEN** a required Go test fails
- **THEN** CI fails and the image is not eligible for publication/deployment

#### Scenario: Sandbox profile behavior changes
- **WHEN** the dependency-free runtime is modified
- **THEN** tests verify startup without Kafka/PostgreSQL, truthful health behavior, and safe rejection of dependency-backed routes

#### Scenario: Persistence integration tests run
- **WHEN** a database integration test is selected for CI
- **THEN** it uses an isolated PostgreSQL instance, applies repository migrations, and cleans up its disposable dependency

### Requirement: Apply practical static checks

CI SHALL run formatting, build, and selected static/dependency checks such as `go vet ./...`. Race detection may be required when supported by measured CI runtime. Coverage thresholds, contract gates, load tests, and benchmarks SHALL NOT use invented targets.

#### Scenario: Static check fails
- **WHEN** a required formatting, build, test, or static check fails
- **THEN** the CI result is unsuccessful and reports the failing command

#### Scenario: Performance gate is proposed
- **WHEN** a load or coverage threshold is proposed
- **THEN** the threshold is based on a measured baseline and an explicitly agreed target before it blocks changes

### Requirement: Keep CI independent of AWS credentials

Pull-request CI SHALL run without AWS credentials or permissions. ECR publication SHALL occur only in a separately scoped workflow job using GitHub OIDC after CI passes. The initial workflow SHALL NOT deploy to EKS; that deployment is performed manually from the learner workstation.

#### Scenario: Pull request workflow runs
- **WHEN** a pull request triggers CI
- **THEN** it can complete tests/build without requesting AWS credentials or provisioning AWS resources

#### Scenario: CI publishes an image
- **WHEN** a protected publish job runs after successful CI
- **THEN** it receives only the scoped ECR permissions needed to push the immutable image and receives no EKS deployment permissions
