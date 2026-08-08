-- +goose Up
CREATE TABLE holds(
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entry_id UUID NOT NULL UNIQUE REFERENCES batch_entries(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending',
    release_artifact_id UUID REFERENCES artifacts(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX holds_status_idx
    ON holds(status);
CREATE INDEX holds_release_artifact_idx
    ON holds(release_artifact_id);

CREATE TABLE reviews(
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hold_id UUID NOT NULL REFERENCES holds(id) ON DELETE CASCADE,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX reviews_hold_idx
    ON reviews(hold_id);

-- +goose Down
DROP TABLE reviews;
DROP TABLE holds;
