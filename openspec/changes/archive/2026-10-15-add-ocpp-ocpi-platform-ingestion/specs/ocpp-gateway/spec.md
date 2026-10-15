## ADDED Requirements

### Requirement: Authenticated OCPP 1.6J station connection
The system MUST accept OCPP 1.6J JSON WebSocket connections only for registered CPO stations with valid station credentials and the `ocpp1.6` WebSocket subprotocol.

#### Scenario: Registered station connects
- **WHEN** a registered station presents valid credentials and negotiates `ocpp1.6`
- **THEN** the gateway associates the connection with that station
- **AND** credentials are not included in logs or published events

#### Scenario: Unknown or unsupported station connection
- **WHEN** a connection uses invalid credentials, an unknown station identity, or an unsupported subprotocol
- **THEN** the gateway rejects the WebSocket upgrade
- **AND** no normalized event is published

### Requirement: OCPP 1.6J status and meter handling
The system MUST implement OCPP 1.6J BootNotification, Heartbeat, StatusNotification, and MeterValues call and result messages, validate supported payload fields, and publish supported station events as versioned unified events to Kafka.

#### Scenario: Boot notification and heartbeat
- **WHEN** an authenticated station sends a valid BootNotification or Heartbeat call
- **THEN** the gateway publishes the station lifecycle event and replies with a successful OCPP result after Kafka acknowledges the event

#### Scenario: EVSE status notification
- **WHEN** an authenticated station sends a valid StatusNotification for a known station connector
- **THEN** the gateway publishes the status event with station, connector, status, error code, and OCPP timestamp
- **AND** it sends the OCPP result after Kafka acknowledges publication

#### Scenario: Meter values notification
- **WHEN** an authenticated station sends valid MeterValues samples
- **THEN** the gateway publishes each normalized meter sample with station, connector, measurand, value, unit, context, and sample timestamp
- **AND** it sends the OCPP result after Kafka acknowledges publication

#### Scenario: Malformed or unsupported OCPP action
- **WHEN** an authenticated station sends a malformed payload or unsupported action
- **THEN** the gateway returns the corresponding OCPP CallError without publishing an invalid event
- **AND** it records a bounded-cardinality rejection metric

### Requirement: Bounded OCPP connection handling
The system MUST enforce configured WebSocket frame size, active connection, and message processing limits.

#### Scenario: Connection or frame limit is exceeded
- **WHEN** a station exceeds a configured connection or frame limit
- **THEN** the gateway rejects or closes the connection with a protocol-appropriate failure
- **AND** remaining station connections retain bounded processing capacity
