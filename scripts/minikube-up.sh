#!/usr/bin/env bash
set -euo pipefail

profile="device-ingestion"
namespace="device-ingestion-staging"
release="device-ingestion"
credentials_file="deploy/kubernetes/minikube/credentials.env"
run_id="local-$(date +%s)"

if [[ ! -f "$credentials_file" ]]; then
	cat >&2 <<EOF
Missing $credentials_file.
Create it from deploy/kubernetes/minikube/credentials.env.example and adjust
the local test credentials if needed.
EOF
	exit 1
fi

if ! minikube status --profile "$profile" >/dev/null 2>&1; then
	minikube start --profile "$profile" --driver=docker
fi

for image in postgres:17-alpine apache/kafka:4.3.1; do
	if ! docker image inspect "$image" >/dev/null 2>&1; then
		docker pull "$image"
	fi
	minikube image load --profile "$profile" "$image"
done

kubectl --context "$profile" create namespace "$namespace" --dry-run=client -o yaml | kubectl --context "$profile" apply -f -
kubectl --context "$profile" apply -f deploy/kubernetes/minikube/dependencies.yaml
kubectl --context "$profile" -n "$namespace" create secret generic "$release-credentials" \
	--from-env-file="$credentials_file" --dry-run=client -o yaml | kubectl --context "$profile" apply -f -

kubectl --context "$profile" -n "$namespace" rollout status deployment/device-ingestion-postgres --timeout=180s
kubectl --context "$profile" -n "$namespace" rollout status deployment/device-ingestion-kafka --timeout=300s

minikube image build --profile "$profile" -t device-ingestion:local .

helm upgrade --install "$release" deploy/kubernetes/device-ingestion \
	--kube-context "$profile" --namespace "$namespace" --create-namespace \
	-f deploy/kubernetes/minikube/values.yaml \
	--set replicaCount=0 --set jobs.enabled=false --wait --timeout 5m

helm upgrade "$release" deploy/kubernetes/device-ingestion \
	--kube-context "$profile" --namespace "$namespace" \
	-f deploy/kubernetes/minikube/values.yaml \
	--set replicaCount=0 \
	--set jobs.enabled=true --set jobs.smoke.enabled=false \
	--set-string "jobs.runId=$run_id" --wait --timeout 5m

kubectl --context "$profile" -n "$namespace" wait \
	--for=condition=complete "job/$release-kafka-init-$run_id" "job/$release-migrate-$run_id" \
	--timeout=5m

helm upgrade "$release" deploy/kubernetes/device-ingestion \
	--kube-context "$profile" --namespace "$namespace" \
	-f deploy/kubernetes/minikube/values.yaml \
	--set replicaCount=1 --set jobs.enabled=true --set jobs.smoke.enabled=false \
	--set-string "jobs.runId=$run_id" --wait --timeout 5m

helm upgrade "$release" deploy/kubernetes/device-ingestion \
	--kube-context "$profile" --namespace "$namespace" \
	-f deploy/kubernetes/minikube/values.yaml \
	--set replicaCount=1 --set jobs.enabled=true --set jobs.smoke.enabled=true \
	--set-string "jobs.runId=$run_id" --wait --timeout 5m

kubectl --context "$profile" -n "$namespace" wait \
	--for=condition=complete "job/$release-smoke-$run_id" --timeout=3m

cat <<EOF
Minikube deployment and protocol/persistence smoke checks passed.
Forward the service ports in another terminal:
  kubectl --context $profile -n $namespace port-forward svc/$release 8080:8080 8081:8081 50051:50051
EOF
