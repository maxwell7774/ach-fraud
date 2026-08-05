#!/usr/bin/env bash
# Seed the database with a realistic, browsable dataset across every UI state:
# pending/approved/declined/auto_declined holds, a velocity split, a blocked
# release, a failed submission, and archived no-hold files.
#
# DESTRUCTIVE: resets the database and artifact dirs.
set -euo pipefail
cd "$(dirname "$0")/.."

DB="postgres://postgres:@localhost:5432/ach_fraud?sslmode=disable"
BIN=./bin/ach
GEN=testdata/gen/seed.py
OUT=seed_out
D=$(date +%y%m%d)
D1=$(date -d "-1 day" +%y%m%d 2>/dev/null || echo "$D")
D2=$(date -d "-2 days" +%y%m%d 2>/dev/null || echo "$D")
D5=$(date -d "-5 days" +%y%m%d 2>/dev/null || echo "$D")

echo "==> build"
go build -o bin/ach .

echo "==> reset database"
psql "$DB" -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;" >/dev/null
GOOSE_DRIVER=postgres GOOSE_DBSTRING="$DB" GOOSE_MIGRATION_DIR=./sql/schema goose up >/dev/null

echo "==> clean directories"
rm -rf input_files artifact_store outgoing_files "$OUT"
mkdir -p input_files "$OUT"

# gen OUT DATE [RDFI] --batch ... : file name, effective date, optional rdfi,
# then one or more --batch specs (entries separated by ';').
gen() {
  local out="$1" date="$2" rdfi="${3:-231380104}"
  shift 3
  python3 "$GEN" "$OUT/$out" --date "$date" --rdfi "$rdfi" "$@"
}

echo "==> generate files"
gen 01_pending_a.ach   "$D"  "" --batch "111000001,150000,Alice A"      # pending: high value
gen 02_pending_b.ach   "$D"  "" --batch "111000002,200000,Bob B"        # pending: high value
gen 03_pending_c.ach   "$D"  "" --batch "111000003,500000,Carol C"      # pending: high value
gen 04_split_x.ach     "$D"  "" --batch "222000001,60000,Split One"     # cross-file velocity split
gen 05_split_y.ach     "$D"  "" --batch "222000001,60000,Split Two"     # -> whole-group hold
gen 06_small.ach       "$D1" "" --batch "333000001,2500,Tiny"           # no hold -> archived
gen 07_medium.ach      "$D1" "" --batch "333000002,50000,Medium"        # no hold -> archived
gen 08_holding.ach     "$D"  "333333334" --batch "444000001,200000,Holding"  # holding RDFI -> skipped
gen 09_older.ach       "$D5" "" --batch "555000001,180000,Old Date"     # pending (older effective date)
gen 10_repeat.ach      "$D1" "" --batch "555000001,2500,Repeat"         # declined-combo -> auto_declined
# Multi-entry single batch: one high-value hold plus two small entries.
gen 11_multi_entry.ach "$D"  "" --batch "111000004,100000,Hold Entry;111000005,25000,Small A;111000006,50000,Medium B"
# Multi-batch file: a single hold, a velocity pair split across batches, and a small entry.
gen 12_multi_batch.ach "$D"  "" \
  --batch "111000007,120000,Batch1 Hold" \
  --batch "111000008,60000,Vel A" \
  --batch "111000008,60000,Vel B" \
  --batch "111000010,2500,Tiny"

cp "$OUT"/*.ach input_files/
# 10_repeat must screen AFTER the decline in run 2 to become auto_declined.
rm -f input_files/10_repeat.ach
rm -f "$OUT"/*.ach
rmdir "$OUT"

echo "==> run 1: screen + process"
$BIN run

echo "==> approve two, decline one (leaves a blocked release)"
H1=$(psql "$DB" -tAqc "SELECT h.id FROM holds h JOIN batch_entries be ON be.id=h.entry_id WHERE be.receiver_account='111000001';" | head -1)
H2=$(psql "$DB" -tAqc "SELECT h.id FROM holds h JOIN batch_entries be ON be.id=h.entry_id WHERE be.receiver_account='111000002';" | head -1)
H9=$(psql "$DB" -tAqc "SELECT h.id FROM holds h JOIN batch_entries be ON be.id=h.entry_id WHERE be.receiver_account='555000001';" | head -1)
$BIN review approve "$H1" --actor seed
$BIN review approve "$H2" --actor seed
$BIN review decline "$H9" --actor seed

echo "==> run 2: auto-declined repeat + blocked-release attempt"
mkdir -p seed_out && python3 "$GEN" seed_out/10_repeat.ach --date "$D1" --rdfi "231380104" --batch "555000001,2500,Repeat"
cp seed_out/10_repeat.ach input_files/
rm -rf seed_out
$BIN run || true
echo "==> run 3: failed submission"
printf 'this is not an ACH file\n' > input_files/broken.ach
$BIN run || true
rm -f input_files/broken.ach

echo "==> summary"
psql "$DB" -c "SELECT status, COUNT(*) FROM holds GROUP BY status ORDER BY status;"
psql "$DB" -c "SELECT status, COUNT(*) FROM submissions GROUP BY status ORDER BY status;"
echo
echo "Seeded. Start the UI with:  ./bin/ach serve  (then open http://localhost:8090)"
echo "Note: the broken file is intentional, so 'ach run' reports 1 failure. The declined hold blocks its release (completed as blocked, not a failure); that file still archives."
