-- +goose Up
CREATE TABLE verifications(
    submission_id UUID PRIMARY KEY REFERENCES submissions(id) ON DELETE CASCADE,
    verified BOOLEAN NOT NULL DEFAULT FALSE,
    issues TEXT NOT NULL DEFAULT '',
    checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE verifications;
