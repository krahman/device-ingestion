# Device ingestion

The service accepts telemetry over HTTP (`:8080`) and gRPC (`:50051`), validates
the packet, and publishes it to Kafka topic `device.telemetry`. It acknowledges
an ingestion request only after Kafka confirms the write. Consumer offsets are
committed only after the database write succeeds. Validation rejects missing or
oversized device IDs, non-finite measurements, and negative measurements before
publishing.

Telemetry is stored in PostgreSQL by a bounded connection pool. Each insert
runs in a transaction at `READ COMMITTED`; a unique `(device_id,
idempotency_key)` constraint makes Kafka redelivery and client retries safe.
Reusing a key with different measurements sends the record to the DLQ. Kafka
offsets are committed only after the PostgreSQL transaction or DLQ write
succeeds. PostgreSQL provides durability through its WAL.

Because telemetry rows are append-only, the writer does not update shared rows
and does not need optimistic version checks or `SELECT FOR UPDATE`. The unique
constraint arbitrates concurrent retries safely. `DB_MAX_OPEN_CONNS` bounds
each process's PostgreSQL pool (maximum 32); size that pool and
`KAFKA_DB_WORKERS` against the total connection budget across service replicas.
The default pool size is `min(KAFKA_DB_WORKERS, 16)`. PostgreSQL is a
downstream consumer: the gateway can continue acknowledging Kafka-persisted
events while the database is unavailable, and the consumer retries without
committing offsets.

`make reconcile` replays the retained `device.telemetry` topic into PostgreSQL
using the same idempotent repository. It is safe to schedule periodically; it
uses a bounded worker count (`RECONCILE_WORKERS`, default 2) and does not change
the live consumer group's offsets. Each run scans retained topic history, so
schedule it at a cadence and time that fit the topic's size and database
capacity. Data older than Kafka retention cannot be repaired by replay.

## Architecture

The project follows ports and adapters (Hexagonal Architecture):

- `internal/domain` contains telemetry types without infrastructure dependencies.
- `internal/application` contains ingestion and processing use cases plus the
  publisher and repository ports they need.
- `internal/adapters/httpapi` and `internal/adapters/grpcapi` translate inbound
  requests into application calls.
- `internal/adapters/kafka` implements the publisher port and runs Kafka
  consumers.
- `internal/adapters/persistence/postgres` implements the repository port and
  applies versioned PostgreSQL migrations.
- `app` is the composition root: it selects adapters, connects them to the
  application, and manages server startup and shutdown.
- `cmd/server` loads configuration and starts the app.

Dependencies point inward: adapters depend on application and domain packages;
the domain has no dependencies on transports, Kafka, or persistence.

## Telemetry ingestion sequence

```mermaid
sequenceDiagram
    autonumber
    participant Device as IoT device
    participant Gateway as HTTP or gRPC gateway
    participant UseCase as Ingest use case
    participant Broker as Kafka broker
    participant Worker as Kafka consumer
    participant DB as PostgreSQL
    participant DLQ as Dead letter topic
    participant Scheduler as Job scheduler
    participant Reconciler as Reconciliation command

    Device->>Gateway: Send telemetry + idempotency key
    Gateway->>UseCase: Validate and ingest packet
    alt Invalid packet
        UseCase-->>Gateway: Validation error
        Gateway-->>Device: Rejected with invalid argument
    else Valid packet
        UseCase->>Broker: Publish to device.telemetry
        Broker-->>UseCase: Acknowledge write
        UseCase-->>Gateway: Queued
        Gateway-->>Device: Accepted and queued
        Broker->>Worker: Deliver telemetry record
        Worker->>DB: Begin READ COMMITTED transaction
        Worker->>DB: Insert using device ID + idempotency key
        alt New packet or matching retry
            DB-->>Worker: Inserted / already stored
            Worker->>DB: Commit transaction
            DB-->>Worker: Commit succeeds
            Worker->>Broker: Commit consumed offset
        else Database unavailable
            DB-->>Worker: Transaction fails
            Worker-->>Worker: Retry the record without committing its offset
        else Permanent invalid record or key conflict
            DB-->>Worker: Permanent processing error
            Worker->>DLQ: Publish failed record
            DLQ-->>Worker: DLQ write acknowledged
            Worker->>Broker: Commit original offset
        end
    end
    opt Scheduled repair pass
        Scheduler->>Reconciler: Run make reconcile
        Reconciler->>Broker: Read retained topic partitions
        Broker-->>Reconciler: Deliver records to bounded workers
        Reconciler->>DB: Persist with device ID and idempotency key
        DB-->>Reconciler: Insert or recognize matching duplicate
        Reconciler-->>Scheduler: Report replayed and skipped records
        Reconciler-->>Reconciler: Leave live consumer offsets unchanged
    end
```

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
make postgres-up
make migrate
```

Then fetch and vendor dependencies, and run the service:

```sh
make tidy
make vendor
KAFKA_BROKERS=localhost:9092 \
DATABASE_URL='postgres://device_ingestion:device_ingestion@127.0.0.1:5432/device_ingestion?sslmode=disable' \
KAFKA_DB_WORKERS=4 make run
```

`DATABASE_URL` defaults to the local URL shown above. Apply migrations with
`make migrate` before starting the service. In production, run migrations as a
deployment step and use credentials with only the privileges they need.

Set `KAFKA_DB_WORKERS` to control the number of consumer group members. It must
be between 1 and 32, and should not exceed the input topic partition count for
useful parallelism. Each worker processes one message at a time; the Kafka
reader queue holds at most one additional message, and each broker fetch is
limited to 1 MiB. This caps concurrent repository calls at the worker count
and keeps the in-memory backlog bounded while Kafka retains remaining records
during slowdowns. The single-node Docker broker is for local development;
production should use a replicated Kafka cluster.

Run a repair pass manually or schedule it with your job runner:

```sh
make reconcile
```

## Send test telemetry

Send a packet over HTTP:

```sh
curl -i http://localhost:8080/telemetry \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: charger-ev-001-telemetry-0001" \
  -d '{"device_id":"charger-ev-001","voltage":230.5,"current":16.0,"power_kw":3.68}'
```

Or send it over gRPC with `grpcurl` installed:

```sh
grpcurl -plaintext \
  -import-path . \
  -proto proto/telemetry.proto \
  -d '{"deviceId":"charger-ev-001","voltage":230.5,"current":16.0,"powerKw":3.68,"idempotencyKey":"charger-ev-001-telemetry-0001"}' \
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

## Troubleshooting workflow

Work incidents in this order: **Observe → Isolate → Mitigate → Find the root
cause → Apply a permanent fix.** Record timestamps and the affected component as
you go so logs and broker offsets can be compared across services.

1. **Observe (metrics and logs).** Check service and dependency logs, Kafka
   consumer lag, topic partition health, PostgreSQL availability, and host
   memory/CPU. For local dependencies, use `docker logs --tail=200
   device-ingestion-kafka` and `docker logs --tail=200
   device-ingestion-postgres`; inspect the terminal running `make run` for
   application logs, which are emitted as JSON with `slog`. Scrape
   `http://localhost:8080/metrics` (`curl -s localhost:8080/metrics`) for
   request totals and latency, Kafka publish outcomes, consumer
   processing/retry/DLQ counts, worker errors, and current PostgreSQL pool
   usage. Kafka consumer lag and broker health still come from Kafka
   monitoring; this service does not currently export those broker-side
   metrics.
2. **Isolate the bottleneck.** Follow one telemetry record through the path:
   gateway acknowledgment, `device.telemetry`, consumer processing, PostgreSQL
   commit, and Kafka offset commit. If the gateway returns an error, check the
   publisher and broker. If accepted records accumulate, compare consumer lag
   with PostgreSQL health and worker capacity. If records reach the DLQ, inspect
   the DLQ payload and processing error.
3. **Mitigate (stop the bleeding).** Preserve Kafka as the durable buffer.
   Restore broker or database availability, reduce incoming load if needed, or
   lower `KAFKA_DB_WORKERS` if PostgreSQL is saturated. Keep database workers
   and `DB_MAX_OPEN_CONNS` within the database's connection budget. Avoid
   shortening Kafka retention while records are waiting to be processed.
4. **Find the root cause.** Correlate timestamps, device IDs, topic partitions,
   offsets, and error messages. Check configuration and recent changes, then
   determine whether the failure came from validation, Kafka publishing,
   database transactions, capacity limits, or a permanent data conflict.
5. **Apply a permanent fix.** Correct the failing code or configuration, add
   coverage for the failure mode, and document any operational change. After
   recovery, run `make reconcile` to replay retained telemetry safely; it uses
   idempotent writes and does not move the live consumer group's offsets.
