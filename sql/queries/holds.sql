-- name: CreateHold :one
INSERT INTO holds(entry_id, status, reason)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListHoldsBySubmission :many
SELECT h.*, be.trace AS entry_trace, be.rdfi AS entry_rdfi,
       be.receiver_name AS entry_receiver_name,
       be.receiver_account AS entry_receiver_account,
       be.amount AS entry_amount, be.tran_code AS entry_tran_code,
       bh.effective_date, bh.submission_id, bh.customer_id,
       s.filename
FROM holds h
JOIN batch_entries be ON be.id = h.entry_id
JOIN batch_headers bh ON bh.id = be.header_id
JOIN submissions s ON s.id = bh.submission_id
WHERE bh.submission_id = $1
ORDER BY h.created_at;

-- name: ListHoldsByReleaseArtifact :many
SELECT h.*, be.trace AS entry_trace, be.rdfi AS entry_rdfi,
       be.receiver_name AS entry_receiver_name,
       be.receiver_account AS entry_receiver_account,
       be.amount AS entry_amount, be.tran_code AS entry_tran_code,
       bh.effective_date, bh.submission_id, bh.customer_id,
       s.filename
FROM holds h
JOIN batch_entries be ON be.id = h.entry_id
JOIN batch_headers bh ON bh.id = be.header_id
JOIN submissions s ON s.id = bh.submission_id
WHERE h.release_artifact_id = $1
ORDER BY h.created_at;

-- name: ListAllHolds :many
SELECT h.*, be.trace AS entry_trace, be.rdfi AS entry_rdfi,
       be.receiver_name AS entry_receiver_name,
       be.receiver_account AS entry_receiver_account,
       be.amount AS entry_amount, be.tran_code AS entry_tran_code,
       bh.effective_date, bh.submission_id, bh.customer_id,
       s.filename
FROM holds h
JOIN batch_entries be ON be.id = h.entry_id
JOIN batch_headers bh ON bh.id = be.header_id
JOIN submissions s ON s.id = bh.submission_id
ORDER BY h.created_at;

-- name: ListHoldsByStatus :many
SELECT h.*, be.trace AS entry_trace, be.rdfi AS entry_rdfi,
       be.receiver_name AS entry_receiver_name,
       be.receiver_account AS entry_receiver_account,
       be.amount AS entry_amount, be.tran_code AS entry_tran_code,
       bh.effective_date, bh.submission_id, bh.customer_id,
       s.filename
FROM holds h
JOIN batch_entries be ON be.id = h.entry_id
JOIN batch_headers bh ON bh.id = be.header_id
JOIN submissions s ON s.id = bh.submission_id
WHERE ($1::text = '' OR h.status = $1)
ORDER BY h.created_at DESC
LIMIT $2;

-- name: CountHoldsByStatus :many
SELECT h.status, COUNT(*)::bigint AS count
FROM holds h
GROUP BY h.status
ORDER BY h.status;

-- name: ListHoldsFiltered :many
SELECT h.*, be.trace AS entry_trace, be.rdfi AS entry_rdfi,
       be.receiver_name AS entry_receiver_name,
       be.receiver_account AS entry_receiver_account,
       be.amount AS entry_amount, be.tran_code AS entry_tran_code,
       bh.effective_date, bh.submission_id, bh.customer_id,
       s.filename
FROM holds h
JOIN batch_entries be ON be.id = h.entry_id
JOIN batch_headers bh ON bh.id = be.header_id
JOIN submissions s ON s.id = bh.submission_id
WHERE ($1::text = '' OR h.status = $1)
  AND ($2::text = '' OR s.filename ILIKE '%' || $2 || '%'
       OR be.receiver_name ILIKE '%' || $2 || '%'
       OR be.receiver_account ILIKE '%' || $2 || '%'
       OR be.trace ILIKE '%' || $2 || '%')
  AND ($3::date IS NULL OR bh.effective_date >= $3::date)
  AND ($4::date IS NULL OR bh.effective_date <= $4::date)
ORDER BY h.created_at DESC
LIMIT $5;

-- name: CountHoldsFiltered :one
SELECT COUNT(*)::bigint
FROM holds h
JOIN batch_entries be ON be.id = h.entry_id
JOIN batch_headers bh ON bh.id = be.header_id
JOIN submissions s ON s.id = bh.submission_id
WHERE ($1::text = '' OR h.status = $1)
  AND ($2::text = '' OR s.filename ILIKE '%' || $2 || '%'
       OR be.receiver_name ILIKE '%' || $2 || '%'
       OR be.receiver_account ILIKE '%' || $2 || '%'
       OR be.trace ILIKE '%' || $2 || '%')
  AND ($3::date IS NULL OR bh.effective_date >= $3::date)
  AND ($4::date IS NULL OR bh.effective_date <= $4::date);

-- name: GetHoldByID :one
SELECT h.*, be.trace AS entry_trace, be.rdfi AS entry_rdfi,
       be.receiver_name AS entry_receiver_name,
       be.receiver_account AS entry_receiver_account,
       be.amount AS entry_amount, be.tran_code AS entry_tran_code,
       bh.effective_date, bh.submission_id, bh.customer_id,
       s.filename
FROM holds h
JOIN batch_entries be ON be.id = h.entry_id
JOIN batch_headers bh ON bh.id = be.header_id
JOIN submissions s ON s.id = bh.submission_id
WHERE h.id = $1;

-- name: ListCombosBySubmission :many
-- Whitelist/blacklist status for the receiver account/RDFI pairs present in the
-- submission being screened. Full hold history (no expiry) is consulted via the
-- EXISTS subqueries, but only for the pairs that matter to this file. A combo
-- is whitelisted by a prior APPROVED hold and blacklisted by a prior DECLINED
-- hold; pending/auto_declined holds count for neither (undecided, re-screened).
WITH my_combos AS (
    SELECT DISTINCT be.rdfi, be.receiver_account
    FROM batch_entries be
    JOIN batch_headers bh ON bh.id = be.header_id
    WHERE bh.submission_id = $1
      AND be.tran_code IN (22, 32)
      AND bh.effective_date >= $2::date
)
SELECT mc.rdfi, mc.receiver_account,
       EXISTS (
           SELECT 1 FROM holds h
           JOIN batch_entries be2 ON be2.id = h.entry_id
           WHERE be2.rdfi = mc.rdfi AND be2.receiver_account = mc.receiver_account
             AND h.status = 'approved'
       ) AS has_approved,
       EXISTS (
           SELECT 1 FROM holds h
           JOIN batch_entries be2 ON be2.id = h.entry_id
           WHERE be2.rdfi = mc.rdfi AND be2.receiver_account = mc.receiver_account
             AND h.status = 'declined'
       ) AS has_declined
FROM my_combos mc;

-- name: SetHoldStatus :exec
UPDATE holds SET status = $1, updated_at = NOW() WHERE id = $2;

-- name: SetHoldReleaseArtifact :exec
UPDATE holds SET release_artifact_id = $1, updated_at = NOW() WHERE id = $2;

-- name: CreateReview :one
INSERT INTO reviews(hold_id, actor, action, note)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListReviewsByHold :many
SELECT * FROM reviews WHERE hold_id = $1 ORDER BY created_at;
