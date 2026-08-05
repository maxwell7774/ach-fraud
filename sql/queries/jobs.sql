-- name: EnqueueJob :exec
INSERT INTO jobs(kind, ref, state, run_at)
VALUES ($1, $2, 'queued', $3)
ON CONFLICT (kind, ref) DO UPDATE
SET run_at = $3, state = 'queued', updated_at = NOW();

-- name: ClaimDueJob :one
UPDATE jobs SET state = 'in_progress', updated_at = NOW()
WHERE id = (
    SELECT id FROM jobs
    WHERE state = 'queued' AND run_at <= NOW()
    ORDER BY run_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING *;

-- name: CompleteJob :exec
UPDATE jobs SET state = 'done', updated_at = NOW() WHERE id = $1;

-- name: FailJob :exec
UPDATE jobs SET state = 'failed', failures = failures + 1,
    last_error = $2, updated_at = NOW()
WHERE id = $1;

-- name: RequeueStaleJobs :exec
UPDATE jobs SET state = 'queued', updated_at = NOW()
WHERE state = 'in_progress' AND updated_at < NOW() - INTERVAL '5 minutes';

-- name: ListJobsByState :many
SELECT * FROM jobs WHERE state = $1 ORDER BY run_at LIMIT $2;

-- name: ListJobsByRef :many
SELECT * FROM jobs WHERE ref = $1 ORDER BY created_at;

-- name: RequeueJob :one
UPDATE jobs
SET state = 'queued', run_at = NOW(), last_error = '', updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: AcquireIngestLock :one
SELECT pg_advisory_xact_lock($1);
