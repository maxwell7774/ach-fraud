-- name: UpsertVerification :one
INSERT INTO verifications(submission_id, verified, issues, checked_at)
VALUES ($1, $2, $3, NOW())
ON CONFLICT (submission_id)
DO UPDATE SET verified = EXCLUDED.verified, issues = EXCLUDED.issues, checked_at = NOW()
RETURNING *;

-- name: GetVerificationBySubmission :one
SELECT * FROM verifications WHERE submission_id = $1;
