# Local Kubernetes deployment with Minikube

This profile deploys the same application Helm chart used by the AWS staging
pilot. It runs a single-node Kafka broker and PostgreSQL in Minikube, creates a
Kubernetes Secret for local test credentials, and disables AWS Secrets Store
CSI and ALB ingress integrations. Helm still deploys the app, runs the Kafka
topic and database migration Jobs, and executes the protocol and persistence
smoke checks from inside the cluster.

## Prerequisites

- Docker Desktop running with enough memory for Minikube, Kafka, and the app.
- Minikube, kubectl, Helm 3, and Make.
- A local credentials file copied from the example. It is ignored by Git.

## Start the profile

```sh
cp deploy/kubernetes/minikube/credentials.env.example \
  deploy/kubernetes/minikube/credentials.env
make minikube-up
```

The script creates the `device-ingestion` Minikube profile using Docker,
deploys local dependencies, builds the service image inside Minikube, installs
the Helm release, waits for Kafka topic initialization and migrations, and
then runs the smoke Job. The smoke Job checks HTTP and OCPI, HTTP and gRPC
telemetry persistence, plus OCPP BootNotification, StatusNotification, and
MeterValues persistence.

To expose the service ports on the host, leave this command running in another
terminal:

```sh
kubectl --context device-ingestion -n device-ingestion-staging \
  port-forward svc/device-ingestion 8080:8080 8081:8081 50051:50051
```

Endpoints: `http://localhost:8080`,
`ws://localhost:8081/ocpp/charger-local-001`, and `localhost:50051` using
plaintext gRPC. Kubernetes Ingress and TLS termination are AWS-specific in this
pilot and are not part of this local profile.

## Inspect and stop

```sh
kubectl --context device-ingestion -n device-ingestion-staging get pods,jobs,svc
kubectl --context device-ingestion -n device-ingestion-staging logs job/device-ingestion-smoke-<run-id>
make minikube-down
```

The run ID is printed in the Job names returned by `kubectl get jobs`. The
Minikube profile and its data remain on disk after stopping. Delete the profile
with `minikube delete --profile device-ingestion` only when you want to remove
its local cluster and data.

If a pod does not become ready, inspect its events and logs:

```sh
kubectl --context device-ingestion -n device-ingestion-staging get pods
kubectl --context device-ingestion -n device-ingestion-staging describe pod <pod>
kubectl --context device-ingestion -n device-ingestion-staging logs <pod>
```
