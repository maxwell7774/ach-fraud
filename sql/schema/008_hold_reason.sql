-- +goose Up
ALTER TABLE holds ADD COLUMN reason TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE holds DROP COLUMN reason;
