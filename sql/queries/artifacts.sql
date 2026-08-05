-- name: CreateArtifact :one
INSERT INTO artifacts(submission_id, kind, checksum, state)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetArtifactBySubmissionKind :one
SELECT * FROM artifacts WHERE submission_id = $1 AND kind = $2;

-- name: GetArtifactByID :one
SELECT * FROM artifacts WHERE id = $1;

-- name: ListArtifactsBySubmission :many
SELECT * FROM artifacts WHERE submission_id = $1 ORDER BY created_at;

-- name: SetArtifactState :exec
UPDATE artifacts SET state = $1, updated_at = NOW() WHERE id = $2;

-- name: ListArtifactsByStateOlderThan :many
SELECT * FROM artifacts
WHERE state = $1 AND updated_at < $2
ORDER BY updated_at;

-- name: ListArtifactsByChecksum :many
SELECT * FROM artifacts WHERE checksum = $1 ORDER BY created_at;
