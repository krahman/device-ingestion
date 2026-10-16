## 1. Document development and implemented AWS architecture

- [ ] 1.1 Describe the service packages, listeners, Kafka/PostgreSQL event flow, Docker Compose setup, and Minikube configuration with links to the supporting files.
- [ ] 1.2 Document the implemented AWS resources and distinguish Terraform-managed, workflow-managed, and operator-managed setup.
- [ ] 1.3 Add the AWS GitHub Actions sequence diagram and grouped AWS prerequisite inventory with required inputs, permissions, and source links.

## 2. Describe the proposed Azure architecture

- [ ] 2.1 Add an explicitly proposed Azure service mapping and identify where Azure Event Hubs authentication may not match the current Kafka SCRAM implementation.
- [ ] 2.2 Add the proposed Azure deployment sequence and an Azure setup inventory labeled operator-managed, future-IaC, or decision-required.
- [ ] 2.3 Compare AWS and Azure candidates and list unresolved architecture, security, networking, data protection, and pilot-scope decisions.

## 3. Validate the architecture guide

- [ ] 3.1 Cross-check implementation statements against repository source/configuration and Azure product statements against official Microsoft documentation.
- [ ] 3.2 Validate repository links, Mermaid blocks, Markdown structure, and OpenSpec requirements; confirm no application or deployment behavior files changed.
