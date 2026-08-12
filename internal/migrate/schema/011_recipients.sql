-- +goose Up
CREATE TABLE recipients(
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE recipient_alerts(
    recipient_id UUID NOT NULL REFERENCES recipients(id) ON DELETE CASCADE,
    alert_type TEXT NOT NULL,
    PRIMARY KEY (recipient_id, alert_type)
);

-- +goose Down
DROP TABLE recipient_alerts;
DROP TABLE recipients;
