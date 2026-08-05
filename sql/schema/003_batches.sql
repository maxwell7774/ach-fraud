-- +goose Up
CREATE TABLE batch_headers(
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    submission_id UUID NOT NULL REFERENCES submissions(id) ON DELETE CASCADE,
    customer_id TEXT NOT NULL DEFAULT '',
    company_name TEXT NOT NULL DEFAULT '',
    company_description TEXT NOT NULL DEFAULT '',
    effective_date DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX batch_headers_submission_idx
    ON batch_headers(submission_id);

CREATE TABLE batch_entries(
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    header_id UUID NOT NULL REFERENCES batch_headers(id) ON DELETE CASCADE,
    rdfi TEXT NOT NULL DEFAULT '',
    receiver_name TEXT NOT NULL DEFAULT '',
    receiver_account TEXT NOT NULL DEFAULT '',
    amount BIGINT NOT NULL DEFAULT 0,
    tran_code INTEGER NOT NULL DEFAULT 0,
    trace TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX batch_entries_header_idx
    ON batch_entries(header_id);
CREATE INDEX batch_entries_rdfi_account_idx
    ON batch_entries(rdfi, receiver_account);

-- +goose Down
DROP TABLE batch_entries;
DROP TABLE batch_headers;
