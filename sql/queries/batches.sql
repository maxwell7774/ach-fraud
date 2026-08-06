-- name: CreateBatchHeader :one
INSERT INTO batch_headers(submission_id, customer_id, company_name, company_description, effective_date)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateBatchEntry :one
INSERT INTO batch_entries(header_id, rdfi, receiver_name, receiver_account, amount, tran_code, trace)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: HasEntryByRdfiAccount :one
SELECT EXISTS(
    SELECT 1 FROM batch_entries
    WHERE rdfi = $1 AND receiver_account = $2
) AS "exists";

-- name: ListEntriesBySubmission :many
SELECT be.*, bh.customer_id, bh.effective_date
FROM batch_entries be
JOIN batch_headers bh ON bh.id = be.header_id
WHERE bh.submission_id = $1
ORDER BY be.created_at;

-- name: ListEntriesFiltered :many
SELECT be.*, bh.customer_id, bh.effective_date, s.filename
FROM batch_entries be
JOIN batch_headers bh ON bh.id = be.header_id
JOIN submissions s ON s.id = bh.submission_id
WHERE ($1::text = '' OR be.trace ILIKE '%' || $1 || '%'
       OR be.rdfi ILIKE '%' || $1 || '%'
       OR be.receiver_name ILIKE '%' || $1 || '%'
       OR be.receiver_account ILIKE '%' || $1 || '%'
       OR s.filename ILIKE '%' || $1 || '%')
  AND ($2::date IS NULL OR bh.effective_date >= $2::date)
  AND ($3::date IS NULL OR bh.effective_date <= $3::date)
ORDER BY be.created_at DESC
LIMIT $4;

-- name: CountEntriesFiltered :one
SELECT COUNT(*)::bigint
FROM batch_entries be
JOIN batch_headers bh ON bh.id = be.header_id
JOIN submissions s ON s.id = bh.submission_id
WHERE ($1::text = '' OR be.trace ILIKE '%' || $1 || '%'
       OR be.rdfi ILIKE '%' || $1 || '%'
       OR be.receiver_name ILIKE '%' || $1 || '%'
       OR be.receiver_account ILIKE '%' || $1 || '%'
       OR s.filename ILIKE '%' || $1 || '%')
  AND ($2::date IS NULL OR bh.effective_date >= $2::date)
  AND ($3::date IS NULL OR bh.effective_date <= $3::date);

-- name: ListHeadersFiltered :many
SELECT bh.*, s.filename
FROM batch_headers bh
JOIN submissions s ON s.id = bh.submission_id
WHERE ($1::text = '' OR bh.company_name ILIKE '%' || $1 || '%'
       OR bh.customer_id ILIKE '%' || $1 || '%'
       OR bh.company_description ILIKE '%' || $1 || '%'
       OR s.filename ILIKE '%' || $1 || '%')
  AND ($2::date IS NULL OR bh.effective_date >= $2::date)
  AND ($3::date IS NULL OR bh.effective_date <= $3::date)
ORDER BY bh.created_at DESC
LIMIT $4;

-- name: CountHeadersFiltered :one
SELECT COUNT(*)::bigint
FROM batch_headers bh
JOIN submissions s ON s.id = bh.submission_id
WHERE ($1::text = '' OR bh.company_name ILIKE '%' || $1 || '%'
       OR bh.customer_id ILIKE '%' || $1 || '%'
       OR bh.company_description ILIKE '%' || $1 || '%'
       OR s.filename ILIKE '%' || $1 || '%')
  AND ($2::date IS NULL OR bh.effective_date >= $2::date)
  AND ($3::date IS NULL OR bh.effective_date <= $3::date);

-- name: SumVelocity :many
-- Same-day velocity per receiver account/RDFI/date/customer, summed across all
-- ready submissions but restricted to the velocity groups that appear in the
-- submission being screened, so the work is proportional to one file rather
-- than the whole day.
WITH target_groups AS (
    SELECT DISTINCT be2.receiver_account, be2.rdfi, bh2.effective_date, bh2.customer_id
    FROM batch_entries be2
    JOIN batch_headers bh2 ON bh2.id = be2.header_id
    WHERE bh2.submission_id = $2
      AND be2.tran_code IN (22, 32)
      AND bh2.effective_date >= $1::date
)
SELECT be.receiver_account, be.rdfi, bh.effective_date, bh.customer_id,
       SUM(be.amount)::bigint AS total
FROM batch_entries be
JOIN batch_headers bh ON bh.id = be.header_id
JOIN submissions s ON s.id = bh.submission_id
JOIN target_groups tg
  ON tg.receiver_account = be.receiver_account
 AND tg.rdfi = be.rdfi
 AND tg.effective_date = bh.effective_date
 AND tg.customer_id = bh.customer_id
WHERE s.status IN ('ready', 'archived')
  AND be.tran_code IN (22, 32)
  AND bh.effective_date >= $1::date
  AND NOT EXISTS (
    -- A submission whose process job failed never built (or shipped) an
    -- intercept, so its entries are not real same-day exposure. This is
    -- self-healing: requeuing the process job includes it again.
    SELECT 1 FROM jobs j
    WHERE j.kind = 'process' AND j.ref = s.id AND j.state = 'failed'
  )
GROUP BY be.receiver_account, be.rdfi, bh.effective_date, bh.customer_id;
