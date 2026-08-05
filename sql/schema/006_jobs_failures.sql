-- +goose Up
ALTER TABLE jobs RENAME COLUMN attempts TO failures;

-- +goose Down
ALTER TABLE jobs RENAME COLUMN failures TO attempts;
