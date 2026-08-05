-- name: UpsertUser :one
INSERT INTO users(subject, upn, email, name, role, last_login_at)
VALUES ($1, $2, $3, $4, COALESCE(NULLIF($5::text, ''), 'watcher'), NOW())
ON CONFLICT (subject) DO UPDATE
SET upn = EXCLUDED.upn,
    email = EXCLUDED.email,
    name = EXCLUDED.name,
    role = CASE WHEN EXCLUDED.role = '' THEN users.role ELSE EXCLUDED.role END,
    last_login_at = NOW(),
    updated_at = NOW()
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: CreateSession :one
INSERT INTO sessions(user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetSessionByTokenHash :one
SELECT * FROM sessions WHERE token_hash = $1;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = $1;
