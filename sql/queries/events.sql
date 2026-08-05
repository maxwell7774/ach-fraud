-- name: AppendEvent :one
INSERT INTO events(type, ref, payload)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListEvents :many
SELECT * FROM events ORDER BY created_at DESC LIMIT $1 OFFSET $2;

-- name: CountEvents :one
SELECT COUNT(*)::bigint FROM events;
