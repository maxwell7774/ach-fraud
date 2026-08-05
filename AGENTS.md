# AGENTS.md

ACH fraud pre-processor. Cobra CLI (`cmd/`) over moov-io/ach, built on a **ports-and-adapters** architecture: a pure `domain` model, `ports` interfaces, and concrete adapters (`pgstore`, `localfiles`, `sender`). State is a Postgres outbox: `submissions` + `artifacts` + a `jobs` queue drive the pipeline, and artifact bytes live in a content-addressed store keyed by sha256.

## Build & verify

- `go build ./...`, `go vet ./...`, and `go test ./...` are the verification; `gofmt -l .` must be clean. The Makefile wraps these (`make build`, `make vet`, `make test`, `make fmt`); `make sqlc` regenerates DB code, `make migrate-up` / `make migrate-reset` run goose (sourcing `.env`), and `make e2e` runs the end-to-end script.
- Regenerating DB code: `sqlc generate` (output `internal/pgstore`, pgx/v5). Do not hand-write generated code. Editing a query requires sqlc regen.
- Migrations are goose-format in `sql/schema/`. Run with the env vars in `.env` (already set): `goose up`. Reset the DB with `DROP SCHEMA public CASCADE; CREATE SCHEMA public;` before migrating.
- Tests use in-memory fakes in `internal/fakeports` (no DB, no disk) and ACH builders in `internal/testutil`. Pipeline logic is unit-tested in `internal/pipeline` and `internal/achp`; the full job chain is covered in `internal/worker`. Keep it that way: new stages should be testable without a database.

## Architecture

Dependency direction is one-way: `cmd/httpapi/worker → pipeline → ports ← pgstore/localfiles/sender`. `domain` imports nothing infrastructure.

- `internal/domain` — pure entities: Submission, Artifact, Hold, Review, Job, Event, Policy. No DB/FS imports.
- `internal/ports` — Store (submissions/artifacts/batches/holds/reviews/jobs/events + retention queries, with `WithinTx` for atomic multi-writes), Files (content-addressed Get/Put/Delete), Sender (transmit), Input (intake scan), Clock, Notifier.
- `internal/pgstore` — ports.Store over sqlc-generated SQL. Only package allowed to import the driver.
- `internal/localfiles` — content-addressed store: `<artifact_store_dir>/<checksum[:2]>/<checksum>`.
- `internal/localinput` — ports.Input over `input_dir` (*.ach files).
- `internal/sender` — ports.Sender writing atomically to `outgoing_dir/<kind>/<checksum>.ach`.
- `internal/notifier`, `internal/clock` — no-op notifier, real clock.
- `internal/httpapi` — thin JSON API for the review web UI: reads go through `ports.Store`, approve/decline delegate to `pipeline.ApproveHold/DeclineHold`. Handlers are tested with fakes (`internal/httpapi/server_test.go`). No auth yet; `actor` resolves from the request (`X-Actor` header or body), leaving a seam for the planned argon2id+sessions track. Endpoints: `/api/dashboard`, `/api/holds` (filtered/paginated/sorted via `ListHoldsFiltered`), `/api/holds/{id}`, `/api/holds/bulk` (`pipeline.ApproveHolds/DeclineHolds`), `/api/submissions`, `/api/entries`, `/api/headers`, `/api/artifacts/{id}/content`, `/api/events`. The UI style is ported from `ach-fraud-ui-example` (gold/navy palette, light+dark theme, badges, cards, flash, pagination, bulk bar, confirm dialog, file viewer).
- `internal/achp` — pure ACH logic: Read/Write, Rebuild, ParseBatches, VerifySame, MatchHolds, BuildCleaned, BuildRelease, VerifyConsistency. No I/O.
- `internal/pipeline` — stages as funcs over `pipeline.Deps{Store, Files, Sender, Notifier, Clock, Policy}`: Ingest, FixSubmission, ImportSubmission, ScreenSubmission, ProcessSubmission, PublishCleaned, PublishRelease, ArchiveSubmission, Prune, ApproveHold, DeclineHold. `ErrNotReady` marks a stage that must retry later.
- `internal/worker` — claims jobs from the outbox, dispatches to pipeline stages, completes/fails/re-queues them.
- `web/` — SolidJS + Solid Router SPA (no SSR), built with bun into `web/dist`. History routing; `ach serve` serves `/api/*` plus an SPA fallback (any non-`/api` path returns `index.html`) so client routes deep-link cleanly.

## Commands

- `ach run` — ingest `input_dir`, then dispatch every due job until the queue is empty. Idempotent; also finishes work left by a crashed/previous run. A startup sweep (`ReconcileReceived`) re-queues the fix job for any `received` submission that lost it in a crash, and stale `in_progress` jobs are requeued.
- `ach review list [status]` — list holds. `ach review approve|decline <hold-id> [--actor X] [--note Y]` — audit-trailed decisions; approve re-queues the release publish immediately.
- `ach serve [--addr :8090]` — runs the review web UI (JSON API + the built SPA from `web/dist`). `http_addr` config defaults the port; `make web` builds the frontend (bun).
- `ach seed` (`hack/seed.sh`) — DESTRUCTIVE: resets the DB and dirs, then seeds a browsable dataset across every UI state (pending/approved/declined/auto_declined holds, a velocity split, a blocked release, a failed submission, archived files).
- `ach jobs [state]` — list jobs (default queued) with failures/last_error. `ach jobs requeue <id>` resets a failed job to `queued` so the next run retries it (the escape hatch for transient failures).
- `ach import-history --file history.csv [--amount 100000] [--actor X]` — one-time legacy load of receiver history from a semicolon-delimited CSV (6 fields, no header: `<unused>;<receiver name>;<customer id>;<rdfi>;<receiver account>;<effective date YYYYMMDD>`). Creates one `ready` submission (so the pipeline never picks it up), a batch header per (customer, effective date) group, one entry per unique (rdfi, account), and an **approved** hold with an audit review for each. Rows whose (rdfi, account) already exists are skipped, so re-running is safe. Imported approved holds feed the combo history, so future deposits to those accounts are not re-held (unless velocity crosses).
- `ach prune --days N` — retention over archived+published artifacts.

## Data model & flow

- `submissions`: one per arriving file; `filename` is metadata, identity is id + `source_checksum`. Status: `received → ready → failed`.
- `artifacts`: one row per (submission_id, kind) — kind ∈ `original, fixed, cleaned, release`; state ∈ `staged, published, archived, pruned`. Checksum is content sha256; bytes shared across rows are stored once.
- `holds`: pending/approved/declined/auto_declined, linked to a `release_artifact_id`, with a `reason` recorded at screening (velocity / single-amount / previously-declined). `reviews` record every decision (actor, action, note).
- `jobs`: outbox, UNIQUE(kind, ref), state queued/in_progress/done/failed. `EnqueueJob` resets state to `queued` on conflict (re-enqueue re-runs). Claims run in `(run_at, created_at)` order so files in the same run are processed in arrival order deterministically. Stale `in_progress` jobs are requeued by `RequeueStaleJobs`.
- `events`: append-only occurrences (submission_created, hold_created, velocity_crossed, intercept_published, ...) feeding alerts/logs/metrics. `velocity_crossed` carries the split-group payload (account, total, prior, held) so a leaked first leg is visible.

Job chain per submission: `fix_submission → import → screen → process → publish_cleaned → [publish_release × N] → archive`. Process groups pending/approved holds into **release units by velocity group** (account/RDFI/effective date/customer), builds one release artifact per group (a single hold = a one-leg release; a velocity split = one multi-leg release), and links each hold to its group's artifact via `release_artifact_id`. `publish_cleaned` ships the intercept immediately; each `publish_release` job (ref = the release artifact id) is gated on every hold linked to ITS artifact being `approved` (returns `ErrNotReady` to poll; a declined hold completes the job as **blocked** with a `release_blocked` event — never an error), so an approved velocity group ships together while unrelated holds release independently. `archive` runs once the cleaned artifact is published and every release artifact is resolved (published or blocked), and retires original+fixed; a blocked release is archived by its `publish_release` job, so it is a prune candidate too. Cleaned/release bytes stay `published` (transmitted) until prune.

## Hold criteria (pipeline/screen.go, Go — not SQL)

- Credit entries (tran 22/32), `effective_date` within `hold_days`, RDFI != `holding_rdfi`.
- Pending: no prior hold on the receiver account/RDFI combo, and amount ≥ `hold_single_amount` OR same-day velocity (per account/RDFI/date/customer) ≥ `hold_velocity_amount` (defaults 100000 cents = $1,000).
- Velocity is computed across ALL ready submissions (`SumVelocity`), not per file, so funds split across multiple files on the same day cannot duck the rule. When a group crosses the threshold, EVERY entry in it is held for review — even accounts that were already flagged — and a `velocity_crossed` event is emitted with the group total, how much already shipped from earlier files (`prior`), and how much is being held now (`held`).
- Auto-declined: combo with a prior *declined* hold, when its group does not cross the velocity threshold. Combos with any prior hold are otherwise skipped (no duplicate holds). Both `SumVelocity` and the combo lookups (`ListCombosBySubmission`) are **bounded to the groups present in the submission being screened**: the SQL still consults the full day's ready submissions and the full hold history (no expiry), but only for the receiver pairs this file touches, so screening cost is proportional to one file, not the whole database.
- Caveat: files arriving in separate runs are screened in arrival order, so the first file of a day ships before a later split is detected (asymmetry); the first-leg leak is bounded below the velocity threshold.

## Config (`.achfraudconfig.json`, gitignored)

`db_url`, `input_dir`, `artifact_store_dir`, `outgoing_dir`, `hold_days`, `holding_account`, `holding_rdfi`, `hold_single_amount`, `hold_velocity_amount`, `dedup_window_days`. Policy defaults apply when values are 0 (`hold_days` 30, `dedup_window_days` 365, thresholds 100000 cents = $1,000).

## Gotchas

- Effective dates parse from the header `YYMMDD`; a nil effective date is never held.
- `prune` deletes bytes only when every artifact sharing the checksum is pruned (content-addressing), so a newer generation of the same file is never clobbered. Failed submissions keep a `staged` original that prune never touches.
- The build-cleaned/release split is subtle: `achp.BuildCleaned` builds the cleaned file from *copies* of the entries (redirecting only the held copies), so the fixed file is never mutated and `VerifyConsistency` genuinely checks that held entries were redirected and non-held entries kept their destination. `BuildRelease` and the consistency checks use the `Orig*` receiver fields captured by `MatchHolds`, so release legs always target the real receiver regardless of later transforms. The tests in `internal/achp` guard all of this.
- Ingest stores the intake bytes first (content-addressed) and registers the submission + original artifact + fix job in one transaction under a Postgres advisory lock (`WithIngestLock`), so a crash cannot leave a `received` submission with no job and two overlapping runs cannot both register the same file; the intake file is consumed only after that commits. An unfixable file becomes a `failed` submission with a reason. Its original artifact stays `staged` and is never pruned, so it remains inspectable.
- Config is validated at startup (`config.Validate`, called by `cmd/root.go open`): a missing/malformed `holding_rdfi` or `holding_account` fails the CLI immediately instead of surfacing as a failed process job later.
- `publish_release` polls: while any linked hold is pending it returns `ErrNotReady` and the worker re-queues it with a 1-minute delay; if a linked hold is declined after process, the job COMPLETES as **blocked** (a `release_blocked` event, never a failure): it retires the release artifact (state `archived`, so it is a prune candidate) and re-runs the archive gate, so the file still retires original+fixed — the intercept stays at the holding account for manual handling while the release never ships. Approve/decline both re-enqueue the release job so the outcome lands immediately. A review decision is atomic per velocity group: `pipeline.decide` approves/declines every hold sharing the hold's `release_artifact_id` in one transaction, because a split is only ever approved or declined as a unit. The Holds UI surfaces velocity groups: holds sharing a group key (account/RDFI/date/customer) show a "N in group" badge, and the confirm dialog lists every member the decision will cascade to — a fully-approved group then ships as one release.
