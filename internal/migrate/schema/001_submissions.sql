-- +goose Up
CREATE TABLE submissions(
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    filename TEXT NOT NULL,
    source_checksum TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'received',
    failed_reason TEXT NOT NULL DEFAULT '',
    received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX submissions_source_checksum_idx
    ON submissions(source_checksum);
CREATE INDEX submissions_status_idx
    ON submissions(status);

-- +goose Down
DROP TABLE submissions;
