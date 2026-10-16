## ADDED Requirements

### Requirement: Build and tag a non-root application image

The application image SHALL retain a multi-stage build and non-root runtime where compatible with the existing service. Published images SHALL be attributable to a source revision and SHALL be pushed to ECR using an immutable reference suitable for rollback.

#### Scenario: Build an image
- **WHEN** CI builds a source revision
- **THEN** the image starts with required runtime files only, runs non-root, and exposes source revision identity

#### Scenario: Publish a learning release
- **WHEN** an authorized workflow publishes the image
- **THEN** it uses GitHub OIDC and records the immutable ECR image digest for deployment and rollback

### Requirement: Apply practical image hygiene

The build SHALL exclude credentials and irrelevant files from its context and SHALL support a practical vulnerability scan and SBOM generation where tools are available. Scan results SHALL be reported without claiming that a passing scan guarantees security.

#### Scenario: Image scan finds a blocking issue
- **WHEN** an agreed scanner policy identifies a blocking finding
- **THEN** CI reports the finding and prevents deployment until fixed or explicitly reviewed

#### Scenario: Image is deployed
- **WHEN** Kubernetes installs the learning image
- **THEN** the deployment uses the recorded image digest or immutable commit tag rather than a mutable `latest` tag

### Requirement: Keep a rollback image available during the exercise

The learning exercise SHALL retain the previous verified image until the rollback demonstration completes and SHALL document any ECR storage/retention charges.

#### Scenario: Rollback is demonstrated
- **WHEN** the current learning release is rolled back
- **THEN** the prior verified image remains available and the restored workload passes its health check
