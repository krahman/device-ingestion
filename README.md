# Device ingestion

The service accepts telemetry over HTTP (`:8080`) and gRPC (`:50051`), validates
the packet, and publishes it to Kafka topic `device.telemetry`. It acknowledges
an ingestion request only after Kafka confirms the write. Consumer offsets are
committed only after the database write succeeds. Validation rejects missing or
oversized device IDs, non-finite measurements, and negative measurements before
publishing.

The example database writer is a stub. Replace it with a bounded, idempotent
database operation before production use. Records with invalid JSON
are written to `device.telemetry.dlq`; offsets are committed only after the
dead-letter write succeeds.

## Architecture

The project follows ports and adapters (Hexagonal Architecture):

- `internal/domain` contains telemetry types without infrastructure dependencies.
- `internal/application` contains ingestion and processing use cases plus the
  publisher and repository ports they need.
- `internal/adapters/httpapi` and `internal/adapters/grpcapi` translate inbound
  requests into application calls.
- `internal/adapters/kafka` implements the publisher port and runs Kafka
  consumers.
- `internal/adapters/persistence/stub` implements the repository port for the
  example. Replace this adapter with a real database implementation.
- `app` is the composition root: it selects adapters, connects them to the
  application, and manages server startup and shutdown.
- `cmd/server` loads configuration and starts the app.

Dependencies point inward: adapters depend on application and domain packages;
the domain has no dependencies on transports, Kafka, or persistence.

## Kafka setup

Set `KAFKA_BROKERS` to comma-separated bootstrap addresses. Before starting the
service, create both `device.telemetry` and `device.telemetry.dlq`. For
production, use a replication factor of at least 3 and
`min.insync.replicas=2`; the producer waits for all in-sync replicas. Keep
retention long enough to cover database outages and recovery. Use enough
partitions for the desired consumer parallelism. Records are keyed by device ID
to preserve per-device partition ordering.

For example, on a three-broker Kafka cluster:

```sh
kafka-topics.sh --bootstrap-server "$KAFKA_BROKERS" \
  --create --if-not-exists --topic device.telemetry \
  --partitions 12 --replication-factor 3 \
  --config min.insync.replicas=2 \
  --config retention.ms=604800000

kafka-topics.sh --bootstrap-server "$KAFKA_BROKERS" \
  --create --if-not-exists --topic device.telemetry.dlq \
  --partitions 3 --replication-factor 3 \
  --config min.insync.replicas=2 \
  --config retention.ms=2592000000
```

## Run

Start a local Kafka broker and create its topics:

```sh
make kafka-up
make kafka-topics
```

Then fetch and vendor dependencies, and run the service:

```sh
make tidy
make vendor
KAFKA_BROKERS=localhost:9092 KAFKA_DB_WORKERS=4 make run
```

Set `KAFKA_DB_WORKERS` to control the number of consumer group members. It must
be between 1 and 32, and should not exceed the input topic partition count for
useful parallelism. Each worker processes one message at a time; the Kafka
reader queue holds at most one additional message, and each broker fetch is
limited to 1 MiB. This caps concurrent repository calls at the worker count
and keeps the in-memory backlog bounded while Kafka retains remaining records
during slowdowns. The single-node Docker broker is for local development;
production should use a replicated Kafka cluster.

## Send test telemetry

Send a packet over HTTP:

```sh
curl -i http://localhost:8080/telemetry \
  -H "Content-Type: application/json" \
  -d '{"device_id":"charger-ev-001","voltage":230.5,"current":16.0,"power_kw":3.68}'
```

Or send it over gRPC with `grpcurl` installed:

```sh
grpcurl -plaintext \
  -import-path . \
  -proto proto/telemetry.proto \
  -d '{"deviceId":"charger-ev-001","voltage":230.5,"current":16.0,"powerKw":3.68}' \
  localhost:50051 ingestion.IngestionService/IngestTelemetry
```

Both should return a queued response after Kafka acknowledges the write. To
inspect records, run:

```sh
docker exec -it device-ingestion-kafka \
  /opt/kafka/bin/kafka-console-consumer.sh \
  --bootstrap-server localhost:9092 \
  --topic device.telemetry \
  --from-beginning
```
