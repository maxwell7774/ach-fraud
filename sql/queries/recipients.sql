-- name: ListRecipients :many
SELECT * FROM recipients ORDER BY email;

-- name: CreateRecipient :one
INSERT INTO recipients(email, name, enabled)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateRecipient :one
UPDATE recipients SET email = $1, name = $2, enabled = $3
 WHERE id = $4
RETURNING *;

-- name: DeleteRecipient :exec
DELETE FROM recipients WHERE id = $1;

-- name: ListRecipientAlerts :many
SELECT recipient_id, alert_type FROM recipient_alerts;

-- name: AddRecipientAlert :exec
INSERT INTO recipient_alerts(recipient_id, alert_type)
VALUES ($1, $2);

-- name: ClearRecipientAlerts :exec
DELETE FROM recipient_alerts WHERE recipient_id = $1;
