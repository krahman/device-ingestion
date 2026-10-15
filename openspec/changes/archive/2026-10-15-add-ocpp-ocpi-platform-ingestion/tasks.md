## 1. Protocol and domain foundation

- [x] 1.1 Define the OCPP 1.6J station, connector, status, and meter-sample domain types and validation.
- [x] 1.2 Define the versioned unified event envelope and supported event type payloads.
- [x] 1.3 Add PostgreSQL migrations and idempotent repositories for station/connector current state and append-only meter samples.
- [x] 1.4 Add Kafka publishing and bounded event processing for unified charging events with retry and dead-letter handling.

## 2. OCPP 1.6J gateway

- [x] 2.1 Add WebSocket listener configuration, station credentials, connection limits, and graceful shutdown.
- [x] 2.2 Implement OCPP 1.6J WebSocket negotiation and CALL/CALLRESULT/CALLERROR message handling.
- [x] 2.3 Handle BootNotification, Heartbeat, StatusNotification, and MeterValues; publish normalized events before success responses.
- [x] 2.4 Add bounded-cardinality OCPP connection, validation, publish, and failure metrics.

## 3. OCPI 2.2.1 CPO Locations interface

- [x] 3.1 Add OCPI version discovery and version details for the CPO Locations Sender endpoint.
- [x] 3.2 Add provisioned eMSP token authentication and return OCPI response envelopes with request/correlation headers.
- [x] 3.3 Implement authenticated paginated Locations and nested EVSE/connector reads, including live status and last-updated filters.
- [x] 3.4 Add bounded-cardinality OCPI request, authorization, and failure metrics.

## 4. Integrate and document

- [x] 4.1 Wire OCPP, OCPI, Kafka event processing, repositories, listener settings, and graceful shutdown.
- [x] 4.2 Document the supported protocol subset, station and partner provisioning, Kafka topics, database migration, deployment, and rollback.
- [x] 4.3 Update the README sequence diagram and configuration examples to match the implemented CPO workflow.
- [x] 4.4 Add focused unit and adapter integration coverage for validation, normalization, protocol responses, retries, DLQ behavior, authorization, and persistence idempotency.
- [x] 4.5 Run formatting, package tests, and build validation; resolve failures before enabling protocol ingress.
