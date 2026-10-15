## Why

The service currently ingests generic telemetry, but it cannot manage charging-station protocol traffic or exchange charging information with roaming partners. Adding OCPP and OCPI boundaries around a shared internal event and charging-data model creates a path from station telemetry to interoperable roaming services.

## What Changes

- Add an OCPP 1.6J WebSocket gateway that authenticates stations, handles BootNotification, Heartbeat, StatusNotification, and MeterValues, and translates station events into a versioned unified event envelope.
- Add an OCPI 2.2.1 CPO interface for version discovery and eMSP pull access to owned Locations, EVSEs, and live EVSE status, authenticated with provisioned partner tokens.
- Publish normalized events to the existing Kafka broker and process them through bounded workers into telemetry and charging-data persistence services.
- Persist station identity and live status separately from append-only meter telemetry; expose current location/status data through OCPI.
- Define delivery, deduplication, validation, and failure handling at both protocol boundaries.
- Scope the first implementation to one CPO/CSMS exchanging with eMSPs. OCPI Commands, Tokens, Sessions/CDRs, payments, roaming settlement, and OCPP remote control are out of scope.

## Capabilities

### New Capabilities
- `ocpp-gateway`: OCPP 1.6J WebSocket connections, station authentication, BootNotification/Heartbeat/StatusNotification/MeterValues handling, and normalization.
- `ocpi-gateway`: OCPI 2.2.1 version discovery plus read access to the CPO's Locations and live EVSE status for eMSPs, authenticated with provisioned partner tokens.
- `unified-charging-events`: A versioned internal event envelope, broker delivery, bounded processing, and persistence contracts.

### Modified Capabilities

## Impact

- Adds OCPP and OCPI inbound adapters alongside the existing HTTP and gRPC APIs.
- Extends the application/domain layer with charging-station, location, EVSE, session, and normalized event concepts.
- Reuses Kafka and PostgreSQL where suitable; introduces no second broker in the initial scope.
- Affects service configuration, database migrations, operational metrics, and deployment exposure for WebSocket and partner REST traffic.
- Adds the OCPP WebSocket dependency and optional listener, OCPI REST routes, Kafka event/topic handling, and additive PostgreSQL event/status/meter persistence; station/location catalog metadata and partner credentials are provisioned through configuration.
