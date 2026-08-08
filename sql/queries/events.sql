-- name: AppendEvent :one
INSERT INTO events(type, ref, payload)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListEvents :many
SELECT * FROM events ORDER BY created_at DESC LIMIT $1 OFFSET $2;

-- name: CountEvents :one
SELECT COUNT(*)::bigint FROM events;
-- name: CountEventsFiltered :one
SELECT COUNT(*)::bigint FROM events
WHERE ($1::text = '' OR type ILIKE '%' || $1 || '%'
       OR ref::text ILIKE '%' || $1 || '%'
       OR payload::text ILIKE '%' || $1 || '%')
  AND ($2::date IS NULL OR created_at >= $2::date)
  AND ($3::date IS NULL OR created_at <= $3::date);

-- name: ListEventsFiltered :many
SELECT * FROM events
WHERE ($1::text = '' OR type ILIKE '%' || $1 || '%'
       OR ref::text ILIKE '%' || $1 || '%'
       OR payload::text ILIKE '%' || $1 || '%')
  AND ($2::date IS NULL OR created_at >= $2::date)
  AND ($3::date IS NULL OR created_at <= $3::date)
ORDER BY
  CASE WHEN $4::text = 'type' AND $5::text = 'desc' THEN type END DESC,
  CASE WHEN $4::text = 'type' AND $5::text <> 'desc' THEN type END ASC,
  CASE WHEN $4::text = 'when' AND $5::text = 'desc' THEN created_at END DESC,
  CASE WHEN $4::text = 'when' AND $5::text <> 'desc' THEN created_at END ASC,
  created_at DESC,
  id DESC
LIMIT $6 OFFSET $7;
