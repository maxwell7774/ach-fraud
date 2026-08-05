-- name: CreateSubmission :one
INSERT INTO submissions(filename, source_checksum, status, received_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetSubmissionByID :one
SELECT * FROM submissions WHERE id = $1;

-- name: FindSubmissionsBySourceChecksum :many
SELECT * FROM submissions WHERE source_checksum = $1 ORDER BY received_at DESC;

-- name: SetSubmissionStatus :exec
UPDATE submissions
SET status = $1, failed_reason = $2, updated_at = NOW()
WHERE id = $3;

-- name: ListReceivedWithoutFixJob :many
SELECT s.* FROM submissions s
WHERE s.status = 'received'
  AND NOT EXISTS (
    SELECT 1 FROM jobs j WHERE j.kind = 'fix_submission' AND j.ref = s.id
  );

-- name: ListSubmissions :many
SELECT * FROM submissions
WHERE ($1::text = '' OR status = $1)
ORDER BY received_at DESC
LIMIT $2;
