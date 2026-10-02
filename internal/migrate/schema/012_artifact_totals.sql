-- +goose Up
-- Per-artifact debit/credit splits, computed from the file bytes when the
-- artifact row is created. NULL means never computed (pre-migration rows, or
-- an original artifact awaiting fix); a genuine zero is stored as 0.
ALTER TABLE artifacts ADD COLUMN debit_total BIGINT;
ALTER TABLE artifacts ADD COLUMN credit_total BIGINT;
ALTER TABLE artifacts ADD COLUMN debit_entries INTEGER;
ALTER TABLE artifacts ADD COLUMN credit_entries INTEGER;

-- +goose Down
ALTER TABLE artifacts DROP COLUMN credit_entries;
ALTER TABLE artifacts DROP COLUMN debit_entries;
ALTER TABLE artifacts DROP COLUMN credit_total;
ALTER TABLE artifacts DROP COLUMN debit_total;
