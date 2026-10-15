## ADDED Requirements

### Requirement: OCPI 2.2.1 CPO discovery and partner authentication
The system MUST expose OCPI 2.2.1 version discovery and version details, and MUST authenticate Locations requests with a provisioned eMSP token associated with that partner party.

#### Scenario: eMSP discovers the CPO interface
- **WHEN** an eMSP requests the OCPI versions endpoint and follows the OCPI 2.2.1 version URL
- **THEN** the CPO returns supported version and endpoint information for the Locations Sender interface
- **AND** every response echoes the request ID and correlation ID required by OCPI

#### Scenario: Provisioned eMSP calls an endpoint
- **WHEN** an eMSP sends a request with its configured OCPI token
- **THEN** the gateway binds the request to that eMSP party and authorizes access to the configured CPO interface
- **AND** tokens are never written to logs, metrics, or unified events

#### Scenario: Invalid token or unsupported version
- **WHEN** a request has invalid credentials or targets an unsupported OCPI version
- **THEN** the gateway returns the OCPI-defined authorization or version error
- **AND** no protected data is returned

### Requirement: CPO Locations Sender interface
The system MUST implement the OCPI 2.2.1 CPO Sender interface for eMSPs to retrieve CPO-owned Locations, EVSEs, and connectors from provisioned station metadata, including current EVSE status from PostgreSQL.

#### Scenario: eMSP retrieves locations
- **WHEN** an authorized eMSP requests the CPO Locations endpoint
- **THEN** the gateway returns OCPI 2.2.1 Location resources with their EVSEs, connectors, and latest durably processed status
- **AND** pagination and last-updated filtering follow the OCPI 2.2.1 interface contract

#### Scenario: eMSP retrieves a specific EVSE or Location
- **WHEN** an authorized eMSP requests a supported Location or nested EVSE resource by identifier
- **THEN** the gateway returns the matching CPO-owned resource using OCPI identifiers and timestamps
- **AND** a missing resource returns the OCPI not-found response

#### Scenario: eMSP retrieves a specific connector
- **WHEN** an authorized eMSP requests a connector by its Location, EVSE, and connector identifiers
- **THEN** the gateway returns the matching OCPI connector resource
- **AND** a missing connector returns the OCPI not-found response

#### Scenario: Partner cannot access another party's data
- **WHEN** an eMSP requests data outside the configured CPO party scope
- **THEN** the gateway returns an authorization or not-found response without disclosing another party's resource

### Requirement: OCPI request observability
The system MUST expose OCPI request outcomes, latency, and failures using bounded-cardinality version and endpoint labels without credentials or partner-controlled resource identifiers.

#### Scenario: OCPI request metrics are recorded
- **WHEN** an OCPI request completes successfully or fails
- **THEN** the system records its outcome and duration
- **AND** authorization tokens and raw resource IDs are excluded from metric labels and logs
