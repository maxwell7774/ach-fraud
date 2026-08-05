-- +goose Up
CREATE TABLE artifacts(
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    submission_id UUID NOT NULL REFERENCES submissions(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    checksum TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'staged',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (submission_id, kind)
);

CREATE INDEX artifacts_checksum_idx
    ON artifacts(checksum);
CREATE INDEX artifacts_submission_idx
    ON artifacts(submission_id);

-- +goose Down
DROP TABLE artifacts;
