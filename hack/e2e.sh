#!/usr/bin/env bash
# End-to-end verification of the ach pipeline against a real Postgres.
# Resets the database and directories, so only run this when the environment
# is disposable.
set -euo pipefail
cd "$(dirname "$0")/.."

DB="postgres://postgres:@localhost:5432/ach_fraud?sslmode=disable"
BIN=./bin/ach

fail() { echo "FAIL: $1" >&2; exit 1; }
assert_match() { # reads stdin; args: pattern, label
  grep -q "$1" || fail "${2:-match} (want match for '$1')"
}

echo "==> build"
go build -o bin/ach .

echo "==> reset database"
psql "$DB" -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;" >/dev/null
GOOSE_DRIVER=postgres GOOSE_DBSTRING="$DB" GOOSE_MIGRATION_DIR=./sql/schema goose up >/dev/null

echo "==> clean directories"
rm -rf input_files artifact_store outgoing_files
mkdir -p input_files

echo "==> generate test files"
mkdir -p gen_out
ACH_GEN_OUT=gen_out python3 testdata/gen/gen.py
ACH_GEN_OUT=gen_out python3 testdata/gen/gen2.py
ACH_GEN_OUT=gen_out python3 testdata/gen/gen3.py
ACH_GEN_OUT=gen_out python3 testdata/gen/gen4.py
ACH_GEN_OUT=gen_out python3 testdata/gen/genmulti.py
cp gen_out/*.ach input_files/
rm -rf gen_out

echo "==> run 1: ingest + screen"
OUT1=$($BIN run)
echo "$OUT1"
assert_match "ingested 5" "run 1 ingest" <<<"$OUT1"
assert_match "failed 0" "run 1 failures" <<<"$OUT1"
# 5 release units (credits 2, credits2 2, multi 1) each poll for their pending
# holds, so the run ends with 5 waiting.
assert_match "waiting 5" "run 1 waiting" <<<"$OUT1"

PENDING=$($BIN review list pending | awk '{print $1}')
# 6 pending: credits 3, credits2 2 (its $2,000 to 123450001 is now velocity-crossed
# by credits' same-day entries), multi 1.
[ "$(echo "$PENDING" | grep -c .)" = "6" ] || fail "expected 6 pending holds"
[ -z "$(ls input_files/)" ] || fail "input dir not drained"

echo "==> approve all holds + rerun"
for id in $PENDING; do $BIN review approve "$id" --actor e2e; done
OUT2=$($BIN run)
echo "$OUT2"
assert_match "failed 0" "run 2 failures" <<<"$OUT2"
assert_match "waiting 0" "run 2 waiting" <<<"$OUT2"

# One release file per velocity group: credits 2, credits2 2, multi 1.
[ "$(ls outgoing_files/release/ | grep -c .)" = "5" ] || fail "expected 5 release files"
[ "$(ls outgoing_files/cleaned/ | grep -c .)" = "5" ] || fail "expected 5 cleaned files"
[ -n "$(ls artifact_store/)" ] || fail "artifact store is empty"

echo "==> run 3: idempotent no-op"
OUT3=$($BIN run)
echo "$OUT3"
assert_match "ingested 0, skipped 0, completed 0, failed 0, waiting 0" "idempotent run" <<<"$OUT3"

echo "==> unfixable file fails cleanly"
printf 'not an ACH file\n' > input_files/broken.ach
OUT4=$($BIN run)
echo "$OUT4"
assert_match "failed 1" "broken file" <<<"$OUT4"
[ "$(psql "$DB" -tAc "SELECT status FROM submissions WHERE filename='broken.ach'")" = "failed" ] \
  || fail "broken.ach not marked failed"
[ "$(psql "$DB" -tAc "SELECT state FROM jobs WHERE kind='fix_submission' AND state='failed'")" = "failed" ] \
  || fail "broken fix job not marked failed"
# NOTE: ingest consumes broken.ach; its bytes stay in the content store.

echo "==> prune"
OUT5=$($BIN prune --days 0)
echo "$OUT5"
assert_match "artifacts deleted" "prune" <<<"$OUT5"
# Only broken.ach's staged original survives (failed submissions are never
# archived, so prune leaves them for inspection).
[ "$(find artifact_store -type f | grep -c .)" = "1" ] || fail "expected only broken.ach bytes to remain"

echo "PASS: full pipeline e2e"
