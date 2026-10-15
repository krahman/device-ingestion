## Purpose
Provide a versioned event contract and durable, replay-safe processing path for charging station status and meter data.

## Requirements

### Requirement: Versioned unified event envelope
The system MUST represent accepted OCPP 1.6J station input in a versioned envelope containing a unique event identifier, schema version, event type, source protocol and version, CPO/station identity, event timestamps, an event idempotency key, an optional OCPP call correlation identifier, and a typed normalized payload.

#### Scenario: OCPP adapter publishes a station event
- **WHEN** the OCPP adapter accepts a supported BootNotification, Heartbeat, StatusNotification, or MeterValues message
- **THEN** it publishes an envelope conforming to the current schema version to the configured Kafka topic
- **AND** protocol credentials are excluded from the event

#### Scenario: Consumer receives an unknown envelope version
- **WHEN** a worker receives an unsupported schema version or invalid required metadata
- **THEN** it writes the record and failure metadata to the dead-letter topic
- **AND** it commits the source offset only after the dead-letter write succeeds

### Requirement: Bounded retry-safe event processing
The system MUST process unified events with a configured bounded worker pool and MUST make persistence safe against Kafka redelivery.

#### Scenario: Event persistence succeeds
- **WHEN** a worker persists a valid station or meter event or recognizes an equivalent duplicate
- **THEN** it commits the Kafka offset only after the database transaction succeeds
- **AND** replay does not create duplicate event or meter records

#### Scenario: Persistence is temporarily unavailable
- **WHEN** PostgreSQL cannot durably process an event
- **THEN** the worker retries or leaves the offset uncommitted according to the configured retry policy
- **AND** it does not acknowledge the event as persisted

#### Scenario: Permanent processing failure
- **WHEN** an event cannot be processed permanently
- **THEN** the worker writes the event and failure metadata to the dead-letter topic
- **AND** commits the original offset only after the dead-letter write succeeds

### Requirement: Separate current station state from append-only meter samples
The system MUST persist station and connector status as current charging state separately from append-only meter samples and generic telemetry packets.

#### Scenario: Status event updates connector state
- **WHEN** a valid StatusNotification is processed
- **THEN** the worker upserts the current status for the station connector with its OCPP occurrence timestamp
- **AND** an older replay does not overwrite a newer status

#### Scenario: Meter sample is processed
- **WHEN** a valid MeterValues event is processed
- **THEN** each normalized sample is stored append-only with station, connector, measurand, value, unit, context, and timestamp
- **AND** duplicate event delivery does not create duplicate samples

### Requirement: Event operational visibility
The system MUST expose bounded-cardinality metrics for event publication, processing, retries, dead-letter outcomes, and persistence latency, partitioned by protocol and result.

#### Scenario: Event processing is observed
- **WHEN** an event is published, processed, retried, dead-lettered, or persisted
- **THEN** the corresponding operational metric is updated
- **AND** station identifiers, credentials, or partner-controlled IDs are not metric labels
