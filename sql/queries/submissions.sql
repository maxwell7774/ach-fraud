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

-- name: CountSubmissionsFiltered :one
SELECT COUNT(*)::bigint FROM submissions
WHERE ($1::text = '' OR status = $1)
  AND ($2::text = '' OR filename ILIKE '%' || $2 || '%'
       OR source_checksum ILIKE '%' || $2 || '%'
       OR failed_reason ILIKE '%' || $2 || '%')
  AND ($3::date IS NULL OR received_at >= $3::date)
  AND ($4::date IS NULL OR received_at <= $4::date);

-- name: ListSubmissionsFiltered :many
SELECT * FROM submissions
WHERE ($1::text = '' OR status = $1)
  AND ($2::text = '' OR filename ILIKE '%' || $2 || '%'
       OR source_checksum ILIKE '%' || $2 || '%'
       OR failed_reason ILIKE '%' || $2 || '%')
  AND ($3::date IS NULL OR received_at >= $3::date)
  AND ($4::date IS NULL OR received_at <= $4::date)
ORDER BY
  CASE WHEN $5::text = 'filename' AND $6::text = 'desc' THEN filename END DESC,
  CASE WHEN $5::text = 'filename' AND $6::text <> 'desc' THEN filename END ASC,
  CASE WHEN $5::text = 'received' AND $6::text = 'desc' THEN received_at END DESC,
  CASE WHEN $5::text = 'received' AND $6::text <> 'desc' THEN received_at END ASC,
  CASE WHEN $5::text = 'status' AND $6::text = 'desc' THEN status END DESC,
  CASE WHEN $5::text = 'status' AND $6::text <> 'desc' THEN status END ASC,
  received_at DESC,
  id DESC
LIMIT $7 OFFSET $8;
