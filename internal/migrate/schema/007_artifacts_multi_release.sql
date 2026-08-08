-- +goose Up
-- A submission now gets one release artifact PER velocity group of holds, so
-- release rows are no longer unique per (submission_id, kind). Keep the
-- one-per-submission guard for every other kind.
ALTER TABLE artifacts DROP CONSTRAINT artifacts_submission_id_kind_key;
CREATE UNIQUE INDEX artifacts_one_per_submission
    ON artifacts(submission_id, kind)
    WHERE kind <> 'release';

-- +goose Down
DROP INDEX artifacts_one_per_submission;
ALTER TABLE artifacts ADD CONSTRAINT artifacts_submission_id_kind_key UNIQUE (submission_id, kind);
