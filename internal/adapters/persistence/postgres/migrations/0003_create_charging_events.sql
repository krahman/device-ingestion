CREATE TABLE charging_events (
    event_id TEXT PRIMARY KEY,
    schema_version INTEGER NOT NULL,
    station_id TEXT NOT NULL,
    source_protocol TEXT NOT NULL,
    protocol_version TEXT NOT NULL,
    cpo_country_code CHAR(2) NOT NULL,
    cpo_party_id CHAR(3) NOT NULL,
    idempotency_key TEXT NOT NULL,
    correlation_id TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    UNIQUE (station_id, idempotency_key)
);

CREATE TABLE charging_connector_status (
    station_id TEXT NOT NULL,
    connector_id INTEGER NOT NULL CHECK (connector_id >= 0),
    status TEXT NOT NULL,
    error_code TEXT NOT NULL,
    status_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (station_id, connector_id)
);

CREATE TABLE charging_meter_samples (
    event_id TEXT NOT NULL REFERENCES charging_events(event_id),
    sample_index INTEGER NOT NULL CHECK (sample_index >= 0),
    station_id TEXT NOT NULL,
    connector_id INTEGER NOT NULL CHECK (connector_id >= 0),
    sampled_at TIMESTAMPTZ NOT NULL,
    measurand TEXT NOT NULL,
    value NUMERIC NOT NULL,
    unit TEXT NOT NULL,
    context TEXT NOT NULL,
    format TEXT NOT NULL,
    phase TEXT NOT NULL,
    location TEXT NOT NULL,
    PRIMARY KEY (event_id, sample_index)
);

CREATE INDEX charging_meter_samples_station_time_idx
    ON charging_meter_samples (station_id, sampled_at DESC);
