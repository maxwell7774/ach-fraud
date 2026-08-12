package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/fakeports"
	"github.com/27actions/ach/internal/notifier"

	"github.com/google/uuid"
)

const (
	rdfi     = "231380104"
	holding  = "333333334"
	account1 = "100000001"
	account2 = "100000002"
)

func screenDeps(t *testing.T, now time.Time) (Deps, *fakeports.Store) {
	t.Helper()
	st := fakeports.NewStore()
	clk := fakeports.NewClock(now)
	d := Deps{
		Store:    st,
		Files:    fakeports.NewFiles(),
		Sender:   fakeports.NewSender(),
		Notifier: notifier.Noop{},
		Clock:    clk,
		Policy: domain.Policy{
			HoldDays:           30,
			HoldingRDFI:        holding,
			HoldingAccount:     "555555",
			HoldSingleAmount:   100000,
			HoldVelocityAmount: 100000,
		},
	}
	return d, st
}

func seedSubmission(t *testing.T, st *fakeports.Store, filename string, now time.Time) uuid.UUID {
	t.Helper()
	// Screen always runs after import, so submissions are seeded ready.
	sub, err := st.CreateSubmission(context.Background(), domain.Submission{
		Filename: filename, Status: domain.SubmissionReady, ReceivedAt: now,
	})
	if err != nil {
		t.Fatalf("seed submission: %v", err)
	}
	return sub.ID
}

func seedEntry(t *testing.T, st *fakeports.Store, subID uuid.UUID, acct string, amount int64, eff time.Time) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	hdr, err := st.CreateBatchHeader(ctx, domain.BatchHeader{
		SubmissionID: subID, CustomerID: "12104288", EffectiveDate: &eff,
	})
	if err != nil {
		t.Fatalf("seed header: %v", err)
	}
	en, err := st.CreateBatchEntry(ctx, domain.BatchEntry{
		HeaderID: hdr.ID, Rdfi: rdfi, ReceiverAccount: acct, Amount: amount, TranCode: 22, Trace: "t" + acct,
	})
	if err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	return en.ID
}

// TestScreenSingleHighValue: a single entry over the threshold is held.
func TestScreenSingleHighValue(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	eff := now.AddDate(0, 0, -1)
	d, st := screenDeps(t, now)
	sub := seedSubmission(t, st, "a.ach", now)
	seedEntry(t, st, sub, account1, 200000, eff)

	res, err := ScreenSubmission(context.Background(), d, sub)
	if err != nil {
		t.Fatalf("screen: %v", err)
	}
	if res.Pending != 1 {
		t.Fatalf("expected 1 pending, got %d", res.Pending)
	}
}

// TestScreenReasonLabels: a single entry held because its amount crosses the
// single-entry threshold is labeled "amount", even though the amount also trips
// the (same-threshold) velocity rule; only genuinely multi-entry velocity gets
// the "velocity" label.
func TestScreenReasonLabels(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	eff := now.AddDate(0, 0, -1)
	d, st := screenDeps(t, now)

	// One entry above the single threshold -> amount reason.
	subA := seedSubmission(t, st, "a.ach", now)
	seedEntry(t, st, subA, account1, 200000, eff)
	if _, err := ScreenSubmission(context.Background(), d, subA); err != nil {
		t.Fatalf("screen a: %v", err)
	}
	holdsA := st.HoldsFor(subA)
	if len(holdsA) != 1 {
		t.Fatalf("expected 1 hold, got %d", len(holdsA))
	}
	if !strings.Contains(holdsA[0].Reason, "amount:") {
		t.Fatalf("reason = %q, want an amount reason", holdsA[0].Reason)
	}

	// Two below-threshold entries to the same account sum over the velocity
	// threshold -> velocity reason.
	subB := seedSubmission(t, st, "b.ach", now)
	seedEntry(t, st, subB, account2, 60000, eff)
	seedEntry(t, st, subB, account2, 60000, eff)
	if _, err := ScreenSubmission(context.Background(), d, subB); err != nil {
		t.Fatalf("screen b: %v", err)
	}
	holdsB := st.HoldsFor(subB)
	if len(holdsB) != 2 {
		t.Fatalf("expected 2 velocity holds, got %d", len(holdsB))
	}
	for _, h := range holdsB {
		if !strings.Contains(h.Reason, "velocity:") {
			t.Fatalf("reason = %q, want a velocity reason", h.Reason)
		}
	}
}

// TestScreenCrossFileVelocity: funds split across two files on the same day to
// the same account sum together for the velocity rule.
func TestScreenCrossFileVelocity(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	eff := now.AddDate(0, 0, -1)
	d, st := screenDeps(t, now)
	subA := seedSubmission(t, st, "a.ach", now)
	seedEntry(t, st, subA, account1, 60000, eff)

	// A arrives first: screened with no same-day context yet, $600 is under
	// the threshold, so it is not held.
	if _, err := ScreenSubmission(context.Background(), d, subA); err != nil {
		t.Fatalf("screen a: %v", err)
	}
	if got := len(st.HoldsFor(subA)); got != 0 {
		t.Fatalf("expected no hold on the first file, got %d", got)
	}

	// B arrives later the same day with another $600 to the same account.
	subB := seedSubmission(t, st, "b.ach", now)
	seedEntry(t, st, subB, account1, 60000, eff)

	// B's screen sees A's same-day exposure combined: $1,200 >= threshold.
	if _, err := ScreenSubmission(context.Background(), d, subB); err != nil {
		t.Fatalf("screen b: %v", err)
	}
	if got := len(st.HoldsFor(subB)); got != 1 {
		t.Fatalf("expected 1 velocity hold on the second file, got %d", got)
	}
}

// TestScreenVelocity: two below-threshold entries on the same day to the same
// account/date/customer sum over the velocity threshold; both are held.
func TestScreenVelocity(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	eff := now.AddDate(0, 0, -1)
	d, st := screenDeps(t, now)
	sub := seedSubmission(t, st, "a.ach", now)
	seedEntry(t, st, sub, account1, 60000, eff)
	seedEntry(t, st, sub, account1, 60000, eff)

	res, err := ScreenSubmission(context.Background(), d, sub)
	if err != nil {
		t.Fatalf("screen: %v", err)
	}
	if res.Pending != 2 {
		t.Fatalf("expected 2 velocity holds, got %d", res.Pending)
	}
}

// TestScreenBelowThreshold: a small entry with no velocity is not held.
func TestScreenBelowThreshold(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	d, st := screenDeps(t, now)
	sub := seedSubmission(t, st, "a.ach", now)
	seedEntry(t, st, sub, account1, 2500, now.AddDate(0, 0, -1))

	res, err := ScreenSubmission(context.Background(), d, sub)
	if err != nil {
		t.Fatalf("screen: %v", err)
	}
	if res.Pending != 0 {
		t.Fatalf("expected no holds, got %d", res.Pending)
	}
}

// TestScreenStaleEffectiveDate: an entry outside the hold_days lookback is not
// held even when the amount is large.
func TestScreenStaleEffectiveDate(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	d, st := screenDeps(t, now)
	sub := seedSubmission(t, st, "a.ach", now)
	seedEntry(t, st, sub, account1, 200000, now.AddDate(0, 0, -31))

	res, err := ScreenSubmission(context.Background(), d, sub)
	if err != nil {
		t.Fatalf("screen: %v", err)
	}
	if res.Pending != 0 {
		t.Fatalf("expected no holds for stale date, got %d", res.Pending)
	}
}

// TestScreenHoldingRDFI: entries whose RDFI is the holding RDFI are skipped.
func TestScreenHoldingRDFI(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	d, st := screenDeps(t, now)
	sub := seedSubmission(t, st, "a.ach", now)
	// Entry to the holding RDFI.
	hdr, _ := st.CreateBatchHeader(context.Background(), domain.BatchHeader{
		SubmissionID: sub, CustomerID: "12104288", EffectiveDate: &now,
	})
	if _, err := st.CreateBatchEntry(context.Background(), domain.BatchEntry{
		HeaderID: hdr.ID, Rdfi: holding, ReceiverAccount: account1, Amount: 200000, TranCode: 22, Trace: "t",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, err := ScreenSubmission(context.Background(), d, sub)
	if err != nil {
		t.Fatalf("screen: %v", err)
	}
	if res.Pending != 0 || res.AutoDeclined != 0 {
		t.Fatalf("expected holding-rdfi entry skipped, got %+v", res)
	}
	if got := len(st.HoldsFor(sub)); got != 0 {
		t.Fatalf("expected no holds, got %d", got)
	}
}

// TestScreenWhitelistedComboPasses: a combo with a prior APPROVED hold is
// whitelisted — a new high-value entry to it sends without a hold.
func TestScreenWhitelistedComboPasses(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	d, st := screenDeps(t, now)
	sub := seedSubmission(t, st, "a.ach", now)
	// Prior approved hold on the combo (yesterday).
	other := seedSubmission(t, st, "b.ach", now)
	entry := seedEntry(t, st, other, account1, 200000, now.AddDate(0, 0, -1))
	if _, err := st.CreateHold(context.Background(), entry, domain.HoldApproved, ""); err != nil {
		t.Fatalf("seed hold: %v", err)
	}
	// A $2,000 entry today to the same combo.
	seedEntry(t, st, sub, account1, 200000, now.AddDate(0, 0, -2))

	res, err := ScreenSubmission(context.Background(), d, sub)
	if err != nil {
		t.Fatalf("screen: %v", err)
	}
	if res.Pending != 0 || res.AutoDeclined != 0 {
		t.Fatalf("expected whitelisted combo to pass, got %+v", res)
	}
}

// TestScreenPendingComboRescreened: a pending hold is not history — the combo
// is undecided, so a new high-value entry is held for review again.
func TestScreenPendingComboRescreened(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	d, st := screenDeps(t, now)
	sub := seedSubmission(t, st, "a.ach", now)
	other := seedSubmission(t, st, "b.ach", now)
	entry := seedEntry(t, st, other, account1, 200000, now.AddDate(0, 0, -1))
	if _, err := st.CreateHold(context.Background(), entry, domain.HoldPending, ""); err != nil {
		t.Fatalf("seed hold: %v", err)
	}
	seedEntry(t, st, sub, account1, 200000, now.AddDate(0, 0, -2))

	res, err := ScreenSubmission(context.Background(), d, sub)
	if err != nil {
		t.Fatalf("screen: %v", err)
	}
	if res.Pending != 1 {
		t.Fatalf("expected pending hold on rescreened combo, got %+v", res)
	}
}

// TestScreenAutoDeclined: a combo with a prior declined hold is auto-declined
// when its same-day velocity does not cross the threshold.
func TestScreenAutoDeclined(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	d, st := screenDeps(t, now)
	sub := seedSubmission(t, st, "a.ach", now)
	other := seedSubmission(t, st, "b.ach", now)
	entry := seedEntry(t, st, other, account2, 200000, now.AddDate(0, 0, -1))
	if _, err := st.CreateHold(context.Background(), entry, domain.HoldDeclined, ""); err != nil {
		t.Fatalf("seed hold: %v", err)
	}
	seedEntry(t, st, sub, account2, 2500, now.AddDate(0, 0, -2))

	res, err := ScreenSubmission(context.Background(), d, sub)
	if err != nil {
		t.Fatalf("screen: %v", err)
	}
	if res.AutoDeclined != 1 || res.Pending != 0 {
		t.Fatalf("expected 1 auto-declined, got %+v", res)
	}
}

// TestScreenVelocityHoldsWholeGroup: when a split group crosses the velocity
// threshold, every eligible entry in the group is held — the combo skip must
// not shadow it.
func TestScreenVelocityHoldsWholeGroup(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	eff := now.AddDate(0, 0, -1)
	d, st := screenDeps(t, now)
	subA := seedSubmission(t, st, "a.ach", now)
	subB := seedSubmission(t, st, "b.ach", now)
	seedEntry(t, st, subA, account1, 60000, eff)
	seedEntry(t, st, subB, account1, 60000, eff)

	// Both files are in the DB when either screens (same-run ingestion), so
	// each screen sees the combined $1,200 and holds its own entry.
	for _, sub := range []uuid.UUID{subA, subB} {
		if _, err := ScreenSubmission(context.Background(), d, sub); err != nil {
			t.Fatalf("screen: %v", err)
		}
	}
	if gotA, gotB := len(st.HoldsFor(subA)), len(st.HoldsFor(subB)); gotA != 1 || gotB != 1 {
		t.Fatalf("expected a hold on every crossed entry, got A=%d B=%d", gotA, gotB)
	}
}

// TestScreenVelocityAlert: when a split group crosses the threshold, a
// velocity_crossed event records the combined total and how much already
// shipped from earlier files.
func TestScreenVelocityAlert(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	eff := now.AddDate(0, 0, -1)
	d, st := screenDeps(t, now)
	subA := seedSubmission(t, st, "a.ach", now)
	seedEntry(t, st, subA, account1, 60000, eff)
	if _, err := ScreenSubmission(context.Background(), d, subA); err != nil {
		t.Fatalf("screen a: %v", err)
	}

	subB := seedSubmission(t, st, "b.ach", now)
	seedEntry(t, st, subB, account1, 60000, eff)
	if _, err := ScreenSubmission(context.Background(), d, subB); err != nil {
		t.Fatalf("screen b: %v", err)
	}

	var found *domain.Event
	for i := range st.Events() {
		if st.Events()[i].Type == EvVelocityCrossed {
			found = &st.Events()[i]
		}
	}
	if found == nil {
		t.Fatal("expected a velocity_crossed event")
	}
	var p velocityAlert
	if err := json.Unmarshal(found.Payload, &p); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if p.Account != account1 || p.Rdfi != rdfi {
		t.Fatalf("unexpected group: %+v", p)
	}
	if p.Total != 120000 || p.Prior != 60000 || p.Held != 60000 {
		t.Fatalf("unexpected split: %+v", p)
	}
}

// TestScreenNoLeakWhenEarlierFileHeld: two files each trip the single-entry
// threshold, so both are held. The second file's velocity_crossed event must
// not report the first file's held amount as an already-shipped leak.
func TestScreenNoLeakWhenEarlierFileHeld(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	eff := now.AddDate(0, 0, -1)
	d, st := screenDeps(t, now)

	subA := seedSubmission(t, st, "a.ach", now)
	seedEntry(t, st, subA, account1, 150000, eff)
	if _, err := ScreenSubmission(context.Background(), d, subA); err != nil {
		t.Fatalf("screen a: %v", err)
	}
	if got := len(st.HoldsFor(subA)); got != 1 {
		t.Fatalf("file A holds = %d, want 1", got)
	}

	subB := seedSubmission(t, st, "b.ach", now)
	seedEntry(t, st, subB, account1, 120000, eff)
	if _, err := ScreenSubmission(context.Background(), d, subB); err != nil {
		t.Fatalf("screen b: %v", err)
	}
	if got := len(st.HoldsFor(subB)); got != 1 {
		t.Fatalf("file B holds = %d, want 1", got)
	}

	var found *domain.Event
	for i := range st.Events() {
		if st.Events()[i].Type == EvVelocityCrossed {
			found = &st.Events()[i]
		}
	}
	if found == nil {
		t.Fatal("expected a velocity_crossed event")
	}
	var p velocityAlert
	if err := json.Unmarshal(found.Payload, &p); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if p.Total != 270000 {
		t.Fatalf("total = %d, want 270000", p.Total)
	}
	if p.Held != 120000 {
		t.Fatalf("held = %d, want 120000", p.Held)
	}
	if p.Prior != 0 {
		t.Fatalf("prior = %d, want 0 (file A's amount was held, not leaked)", p.Prior)
	}
}

// TestScreenTransactionalRollback: if hold creation fails mid-screen, the whole
// screen rolls back — no partial holds and no events.
func TestScreenTransactionalRollback(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	eff := now.AddDate(0, 0, -1)
	d, st := screenDeps(t, now)
	sub := seedSubmission(t, st, "a.ach", now)
	seedEntry(t, st, sub, account1, 200000, eff)
	boom := seedEntry(t, st, sub, account2, 200000, eff)
	st.HoldError = func(entryID uuid.UUID) error {
		if entryID == boom {
			return errors.New("boom")
		}
		return nil
	}

	if _, err := ScreenSubmission(context.Background(), d, sub); err == nil {
		t.Fatal("expected screen to fail")
	}
	if got := len(st.HoldsFor(sub)); got != 0 {
		t.Fatalf("expected all holds rolled back, got %d", got)
	}
	if got := len(st.Events()); got != 0 {
		t.Fatalf("expected no events after a rolled-back screen, got %d", got)
	}
}

// TestScreenIdempotent: re-screening a submission does not duplicate holds.
func TestScreenIdempotent(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	d, st := screenDeps(t, now)
	sub := seedSubmission(t, st, "a.ach", now)
	seedEntry(t, st, sub, account1, 200000, now.AddDate(0, 0, -1))

	if _, err := ScreenSubmission(context.Background(), d, sub); err != nil {
		t.Fatalf("screen: %v", err)
	}
	if _, err := ScreenSubmission(context.Background(), d, sub); err != nil {
		t.Fatalf("re-screen: %v", err)
	}
	if got := len(st.HoldsFor(sub)); got != 1 {
		t.Fatalf("expected 1 hold after re-screen, got %d", got)
	}
}

// TestScreenVelocitySeesArchivedFiles: a fully processed (archived) file still
// counts toward same-day velocity, so a later file on the same day cannot duck
// the rule just because the earlier one was retired.
func TestScreenVelocitySeesArchivedFiles(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	eff := now.AddDate(0, 0, -1)
	d, st := screenDeps(t, now)

	// File A shipped $600 and was retired (archived).
	subA := seedSubmission(t, st, "a.ach", now)
	seedEntry(t, st, subA, account1, 60000, eff)
	if err := st.SetSubmissionStatus(context.Background(), subA, domain.SubmissionArchived, ""); err != nil {
		t.Fatalf("archive: %v", err)
	}

	// File B lands $600 to the same account/day later: A's exposure must still
	// be counted, so $1,200 >= threshold and B is held.
	subB := seedSubmission(t, st, "b.ach", now)
	seedEntry(t, st, subB, account1, 60000, eff)
	if _, err := ScreenSubmission(context.Background(), d, subB); err != nil {
		t.Fatalf("screen b: %v", err)
	}
	if got := len(st.HoldsFor(subB)); got != 1 {
		t.Fatalf("expected 1 velocity hold despite A being archived, got %d", got)
	}
}
