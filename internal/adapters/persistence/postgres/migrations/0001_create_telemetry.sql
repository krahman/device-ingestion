CREATE TABLE telemetry (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    device_id VARCHAR(128) NOT NULL,
    idempotency_key VARCHAR(255) NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    voltage DOUBLE PRECISION NOT NULL CHECK (voltage >= 0 AND voltage < 'Infinity'::DOUBLE PRECISION),
    current DOUBLE PRECISION NOT NULL CHECK (current >= 0 AND current < 'Infinity'::DOUBLE PRECISION),
    power_kw DOUBLE PRECISION NOT NULL CHECK (power_kw >= 0 AND power_kw < 'Infinity'::DOUBLE PRECISION),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT telemetry_device_idempotency_key_unique UNIQUE (device_id, idempotency_key)
);
