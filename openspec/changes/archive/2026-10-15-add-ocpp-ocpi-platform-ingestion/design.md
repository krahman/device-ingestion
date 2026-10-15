## Context

The current Go service accepts a narrow telemetry packet over HTTP/gRPC, writes it to Kafka, then persists it in PostgreSQL with idempotent inserts and bounded workers. It has no station identity, charging session, location/EVSE, partner identity, or protocol lifecycle. The first increment is a single CPO/CSMS: stations connect over OCPP 1.6J; eMSPs authenticate through OCPI 2.2.1 and pull the CPO's Locations and live EVSE status. OCPP MeterValues are retained as telemetry; they are not represented as OCPI sessions or CDRs.

## Goals / Non-Goals

**Goals:**
- Accept OCPP 1.6J station connections and BootNotification, Heartbeat, StatusNotification, and MeterValues actions over WebSocket.
- Provide OCPI 2.2.1 version discovery and the CPO Sender interface for Locations reads by eMSPs.
- Authenticate each peer, validate protocol payloads, and translate accepted data into a versioned internal envelope.
- Publish normalized events to Kafka and process them with bounded concurrency and retry-safe persistence.
- Keep protocol-specific concerns in adapters and expose charging-domain use cases through application ports.

**Non-Goals:**
- Implement every OCPP action or every OCPI module in the first increment.
- Implement roaming settlement, payment processing, driver applications, or utility smart charging.
- Replace Kafka with NATS or operate two brokers. Kafka is the initial transport, consistent with this repository.
- Claim protocol certification solely from application-level tests.

## Decisions

1. **Keep OCPP and OCPI as separate adapters around one CPO domain.** OCPP is a long-lived, bidirectional station connection; OCPI exposes version/credential discovery and CPO-owned Locations to eMSPs. The OCPP adapter authenticates stations and emits events. The OCPI adapter authenticates parties and reads the shared charging domain; OCPI read requests do not create events.

2. **Use a versioned canonical envelope over Kafka.** The initial event types are station boot/heartbeat, EVSE status change, and meter sample. The envelope carries event ID, schema version, event type, source protocol/version, CPO/station identity, occurrence/receipt timestamps, correlation/idempotency key, and typed payload. Do not retain raw payloads in the first increment; retain only normalized fields needed for operations and replay. Consumers reject unsupported schema versions into a dead-letter path and acknowledge offsets only after durable processing, following the existing consumer pattern.

3. **Reuse the existing Kafka and bounded-worker pattern.** Kafka remains the durable buffer and PostgreSQL the initial system of record. OCPP connection handling validates and publishes before sending a success response; a database outage therefore does not block the gateway while Kafka remains available. OCPI Locations requests are read-only and served from current PostgreSQL state. Worker counts and database pools remain bounded by configuration.

4. **Introduce explicit station and CPO location identities.** Model OCPP charge point identity separately from generic `DeviceID`; model the CPO's location, EVSE, and connector identifiers with OCPI 2.2.1 fields and stable ownership. Persist current EVSE status separately from append-only meter telemetry. Tokens, sessions, and CDRs are deferred.

5. **Pin the initial protocol profiles.** Use OCPP 1.6J over WebSocket and OCPI 2.2.1. Implement only BootNotification, Heartbeat, StatusNotification, and MeterValues on OCPP, and Versions plus the CPO Locations Sender interface on OCPI. Provision eMSP party tokens through deployment secrets for this first increment; defer the OCPI Credentials registration handshake. Keep codecs version-specific so later editions do not leak into the domain model.

6. **Keep credentials and network boundaries adapter-owned.** Station credentials and OCPI party tokens must be stored as secrets or hashes appropriate to their use, compared without logging, and never included in events. Require TLS at production ingress (or trusted TLS termination), apply request/frame size and connection limits, and expose separate health/metrics labels for each protocol.

## Risks / Trade-offs

- **[Protocol breadth can dominate the project]** → Keep the first release to the pinned action/module subset and return explicit protocol errors for unsupported requests.
- **[OCPP is bidirectional while the initial event path is asynchronous]** → Separate command/response handling from durable event processing; define which station responses are sent after broker acknowledgement and which need synchronous application decisions.
- **[OCPI status can lag station reports]** → Serve the latest durably processed station state and expose update timestamps so eMSPs can detect staleness.
- **[A canonical model can lose protocol-specific meaning]** → Keep source protocol/version and typed payloads; retain raw payloads only under an explicit data-retention and privacy policy.
- **[Partner token configuration can drift from OCPI registration state]** → Provision tokens through the deployment secret mechanism for the pilot and implement the Credentials handshake as a follow-up capability.
- **[Partner resource ownership can leak data]** → Bind every partner request to an authenticated party and enforce ownership at application use cases and storage boundaries.
- **[Kafka event persistence and relational state can diverge]** → Make consumers replay-safe, use transactional inserts/upserts and dead-letter handling, and document reconciliation behavior.

## Migration Plan

1. Add additive schema migrations for unified charging events, current connector status, and append-only meter samples; keep station/location metadata in deployment configuration and retain existing telemetry tables/endpoints.
2. Deploy Kafka topics and consumer code before enabling protocol ingress. Ensure topic retention, replication, partitions, and DLQ policies are suitable for the deployment.
3. Deploy OCPP and OCPI listeners disabled or restricted to a pilot tenant; provision credentials and TLS routing.
4. Enable one protocol profile and resource subset, observe protocol-specific rejection, retry, queue-lag, and persistence metrics, then expand.
5. Roll back by disabling protocol listeners and consumers. Keep additive data and topics for diagnosis/replay; do not remove existing ingestion behavior.

## Open Questions

- What station credential provisioning and deployment secret mechanism should be used in production? (The first implementation reads credentials from environment configuration.)
- What data-retention period should apply to normalized meter samples?
- Does the first increment preserve the existing HTTP/gRPC telemetry API as a supported parallel ingress? (Assumed yes.)
