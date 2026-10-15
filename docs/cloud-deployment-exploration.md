# Cloud deployment exploration

**Status:** Architecture exploration, not an implementation plan  
**Date:** 2026-10-15

## Goal

Identify a practical path to deploy the device ingestion platform on AWS or
Azure while preserving its HTTP/gRPC telemetry APIs, OCPP 1.6J WebSocket
ingress, OCPI 2.2.1 interface, Kafka event flow, and PostgreSQL persistence.

## Current application shape

The service is a Go container with three listeners:

| Listener | Port | Use |
| --- | ---: | --- |
| HTTP | 8080 | Telemetry API, metrics, and OCPI |
| OCPP | 8081 | OCPP 1.6J WebSocket sessions; enabled when `OCPP_STATIONS` is configured |
| gRPC | 50051 | Telemetry API |

The app publishes telemetry and charging events to Kafka. Bounded consumers
persist them to PostgreSQL. It also applies database migrations through the
separate `cmd/migrate` executable. The repository has a Dockerfile and a local
Compose stack; Compose currently supplies single-node Kafka and PostgreSQL for
development.

Relevant implementation points:

- [Composition root](../app/composition.go) creates the three listeners and
  enables the OCPP listener only when stations are configured.
- [Kafka telemetry publisher](../internal/adapters/kafka/publisher.go) and
  [charging event publisher](../internal/adapters/kafka/charging_events.go)
  connect to brokers with the current default plaintext transport.
- [Runtime configuration](../app/config.go) accepts Kafka broker addresses and
  a PostgreSQL URL from environment variables.
- [Docker image](../Dockerfile) builds the app and migration binaries into a
  non-root runtime image.

## Deployment shape

```text
Chargers and eMSPs
        │ HTTPS / WSS / gRPC
        ▼
Cloud load balancer or ingress
        │
        ▼
Go app containers ───── managed Kafka-compatible broker
        │
        └─────────────── managed PostgreSQL
```

Expose the protocol endpoints through an HTTPS-capable load balancer or
ingress. Keep Kafka and PostgreSQL on private networking, reachable only by the
app and operational jobs. Inject station passwords, eMSP tokens, database
credentials, and broker credentials from the cloud secret service.

## Readiness gaps

### Kafka authentication and encryption

The current Kafka publishers, readers, and dead-letter writers use the default
transport without TLS or SASL settings. Cloud Kafka services commonly require
encrypted connections and authentication. Azure Event Hubs' Kafka endpoint,
for example, requires TLS and SASL authentication. Add configurable transport
security and credentials before connecting to a managed broker.

Azure Event Hubs provides a Kafka protocol endpoint, but it is a managed Azure
event streaming service rather than an Apache Kafka cluster. Validate the
client operations, delivery behavior, consumer groups, topic provisioning,
offset handling, and dead-letter flow against it. Its Kafka endpoint supports
Kafka clients from version 1.0 onward and maps topics to Event Hubs.

### PostgreSQL TLS and networking

`DATABASE_URL` is configurable, while the local Compose connection uses
`sslmode=disable`. Set the cloud connection string to the provider's TLS mode
and use private networking. Azure Database for PostgreSQL Flexible Server
supports private VNet access or public access with firewall rules/private
endpoints.

### Health probes and migrations

The service exposes `/metrics`, but it does not currently have dedicated
readiness and liveness endpoints. Add probe semantics that distinguish process
startup from readiness to accept traffic. Run `cmd/migrate` as a one-off job
before routing traffic to a new app version rather than making every replica
perform migrations.

### Long-lived OCPP sessions

OCPP uses persistent WebSocket connections. Set ingress idle timeouts above the
expected charger heartbeat interval, and ensure stations reconnect when tasks
are restarted or scaled in. The current OCPP BootNotification response
advertises a 300-second heartbeat interval; the handler's read deadline is ten
minutes. These values must be considered alongside cloud ingress limits and
charger behavior.

Scale-out does not require shared WebSocket session state in the current
implementation, but rolling deployments will close connections on replaced
tasks. Plan for reconnects and graceful task draining. Keep at least one
available replica when uninterrupted charger connectivity matters.

## AWS option

| Concern | Candidate |
| --- | --- |
| Container runtime | Amazon EKS managed node group |
| Image registry | Amazon ECR |
| Ingress | AWS Load Balancer Controller and Application Load Balancer |
| Kafka | Amazon MSK |
| PostgreSQL | Amazon RDS for PostgreSQL |
| Secrets and logs | AWS Secrets Manager, Secrets Store CSI Driver, and CloudWatch |

The Kubernetes pilot uses the AWS Load Balancer Controller to create an
Application Load Balancer from Kubernetes Ingress resources. Route HTTP and
OCPI traffic to port 8080, OCPP WebSockets to 8081, and gRPC to 50051 with
protocol-specific target group settings. Configure the ALB idle timeout above
the OCPP heartbeat interval.

Place worker nodes, MSK, and RDS in private subnets. Expose only the ALB
publicly; restrict broker and database access to the EKS worker security group.
Use workload identity for the app's Secrets Manager access and GitHub OIDC for
the deployment workflow.

**Fit:** EKS is the selected pilot target because Kubernetes deployment and
operations are the project objective. It adds cluster and ingress-controller
operations compared with ECS, while preserving the managed AWS data services.

## Azure option

| Concern | Candidate |
| --- | --- |
| Container runtime | AKS for the current multi-port layout; evaluate Container Apps for a simpler setup |
| Image registry | Azure Container Registry |
| Ingress | AKS ingress/Application Gateway, or Container Apps ingress after validating ports and timeouts |
| Kafka | Azure Event Hubs Kafka endpoint, or a Kafka service if Apache Kafka behavior is required |
| PostgreSQL | Azure Database for PostgreSQL Flexible Server |
| Secrets and logs | Azure Key Vault and Azure Monitor / Log Analytics |

Azure Container Apps HTTP ingress supports WebSockets and gRPC, and its
documented request timeout is 240 seconds. Because this app advertises a
300-second OCPP heartbeat, verify how that timeout applies to persistent idle
WebSocket sessions before choosing Container Apps for OCPP. The service also
has listeners on three target ports. Container Apps supports additional TCP
ports when using TCP ingress; externally exposed TCP ingress requires a custom
VNet. Confirm that the required TLS termination, hostnames, and protocol
routing fit this setup.

AKS offers more control over listener ports, ingress rules, and long-running
WebSocket connections, at the cost of more cluster operations. Event Hubs can
reduce Kafka cluster operations, but requires Kafka TLS/SASL support and
validation of the event hub and consumer-group mapping. Provision its event
hubs and partitions through Azure infrastructure tooling rather than relying
on the local Compose topic initializer.

## Suggested delivery sequence

1. Add cloud-ready Kafka TLS/SASL configuration and PostgreSQL TLS settings.
2. Add readiness/liveness endpoints and define migration as a deployment job.
3. Select the target ingress and confirm long-lived WebSocket behavior,
   including idle timeout and task-draining behavior.
4. Define network, managed broker, PostgreSQL, secrets, registry, and ingress
   resources as infrastructure as code.
5. Deploy one app replica and validate HTTP, gRPC, OCPP BootNotification,
   StatusNotification, MeterValues, and OCPI Locations end to end.
6. Configure backups, monitoring, alerting, autoscaling, and rolling updates
   after the first cloud deployment works.

## Decisions to make

- What charger connection count and reconnect behavior must the ingress and
  scaling policy support?
- If an Azure deployment is considered later, is Kafka protocol compatibility
  through Event Hubs acceptable, or is an Apache Kafka service required?

## References

- [AWS Load Balancer Controller for EKS](https://docs.aws.amazon.com/eks/latest/userguide/lbc-helm.html)
- [ALB WebSocket support](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-listeners.html)
- [ALB idle timeout configuration](https://docs.aws.amazon.com/elasticloadbalancing/latest/application/edit-load-balancer-attributes.html)
- [Azure Event Hubs Kafka overview](https://learn.microsoft.com/azure/event-hubs/azure-event-hubs-kafka-overview)
- [Azure Container Apps ingress](https://learn.microsoft.com/azure/container-apps/ingress)
- [Azure Container Apps additional TCP ports](https://learn.microsoft.com/azure/container-apps/ingress-how-to)
- [Azure Database for PostgreSQL networking](https://learn.microsoft.com/en-us/azure/postgresql/network/how-to-networking)
