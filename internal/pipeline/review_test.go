package pipeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/fakeports"
	"github.com/27actions/ach/internal/notifier"

	"github.com/google/uuid"
)

func overrideDeps(t *testing.T, now time.Time) (Deps, *fakeports.Store) {
	t.Helper()
	st := fakeports.NewStore()
	d := Deps{
		Store:    st,
		Files:    fakeports.NewFiles(),
		Sender:   fakeports.NewSender(),
		Notifier: notifier.Noop{},
		Clock:    fakeports.NewClock(now),
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

// seedHeld creates a submission, a credited entry, and an approved hold on it,
// returning the submission and hold IDs.
func seedHeld(t *testing.T, st *fakeports.Store, now time.Time, acct string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	sub, err := st.CreateSubmission(ctx, domain.Submission{Filename: "a.ach", Status: domain.SubmissionReady, ReceivedAt: now})
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	hdr, err := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: sub.ID, CustomerID: "12104288", EffectiveDate: &now})
	if err != nil {
		t.Fatalf("hdr: %v", err)
	}
	en, err := st.CreateBatchEntry(ctx, domain.BatchEntry{HeaderID: hdr.ID, Rdfi: rdfi, ReceiverAccount: acct, Amount: 200000, TranCode: 22, Trace: "t" + acct})
	if err != nil {
		t.Fatalf("entry: %v", err)
	}
	h, err := st.CreateHold(ctx, en.ID, domain.HoldApproved, "")
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	return sub.ID, h.ID
}

func TestOverrideHoldStatusFlipsAndAudits(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	d, st := overrideDeps(t, now)
	_, holdID := seedHeld(t, st, now, account1)

	n, err := OverrideHoldStatus(context.Background(), d, holdID, "admin", "flip", domain.HoldDeclined, true)
	if err != nil {
		t.Fatalf("override: %v", err)
	}
	if n != 1 {
		t.Fatalf("changed = %d, want 1", n)
	}
	h, err := st.GetHold(context.Background(), holdID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if h.Status != domain.HoldDeclined {
		t.Fatalf("status = %s, want declined", h.Status)
	}
	reviews, err := st.ListReviewsByHold(context.Background(), holdID)
	if err != nil {
		t.Fatalf("reviews: %v", err)
	}
	if len(reviews) != 1 || reviews[0].Action != "declined" || reviews[0].Actor != "admin" {
		t.Fatalf("expected one decline review by admin, got %+v", reviews)
	}
}

func TestOverrideHoldStatusRejectsInvalid(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	d, st := overrideDeps(t, now)
	_, holdID := seedHeld(t, st, now, account1)

	_, err := OverrideHoldStatus(context.Background(), d, holdID, "admin", "", domain.HoldStatus("bogus"), true)
	if !errors.Is(err, ErrInvalidHoldStatus) {
		t.Fatalf("expected ErrInvalidHoldStatus, got %v", err)
	}
	h, _ := st.GetHold(context.Background(), holdID)
	if h.Status != domain.HoldApproved {
		t.Fatalf("status = %s, want approved (unchanged)", h.Status)
	}
}

func TestOverrideHoldStatusFlippingGroup(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	d, st := overrideDeps(t, now)
	subID, holdID := seedHeld(t, st, now, account1)

	// Create a second approved hold in the same submission and link both holds
	// to a shared release artifact so they form a velocity group.
	ctx := context.Background()
	hdr, _ := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: subID, CustomerID: "12104288", EffectiveDate: &now})
	en2, _ := st.CreateBatchEntry(ctx, domain.BatchEntry{HeaderID: hdr.ID, Rdfi: rdfi, ReceiverAccount: account1, Amount: 200000, TranCode: 22, Trace: "t2" + account1})
	h2, err := st.CreateHold(ctx, en2.ID, domain.HoldApproved, "")
	if err != nil {
		t.Fatalf("hold2: %v", err)
	}
	art, err := st.CreateArtifact(ctx, domain.Artifact{
		SubmissionID: subID, Kind: domain.ArtifactRelease, Checksum: "sum", State: domain.ArtifactPublished,
	})
	if err != nil {
		t.Fatalf("artifact: %v", err)
	}
	if err := st.SetHoldReleaseArtifact(ctx, holdID, art.ID); err != nil {
		t.Fatalf("link hold: %v", err)
	}
	if err := st.SetHoldReleaseArtifact(ctx, h2.ID, art.ID); err != nil {
		t.Fatalf("link hold2: %v", err)
	}

	n, err := OverrideHoldStatus(ctx, d, holdID, "admin", "flip group", domain.HoldDeclined, true)
	if err != nil {
		t.Fatalf("override: %v", err)
	}
	if n != 2 {
		t.Fatalf("changed = %d, want 2 (group override)", n)
	}
	for _, id := range []uuid.UUID{holdID, h2.ID} {
		got, _ := st.GetHold(ctx, id)
		if got.Status != domain.HoldDeclined {
			t.Fatalf("hold %s status = %s, want declined (whole group flipped)", id, got.Status)
		}
	}
}

// TestOverrideHoldStatusSingleHold: with group=false only the targeted hold is
// flipped, even when it shares a release artifact with a sibling.
func TestOverrideHoldStatusSingleHold(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	d, st := overrideDeps(t, now)
	subID, holdID := seedHeld(t, st, now, account1)

	ctx := context.Background()
	hdr, _ := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: subID, CustomerID: "12104288", EffectiveDate: &now})
	en2, _ := st.CreateBatchEntry(ctx, domain.BatchEntry{HeaderID: hdr.ID, Rdfi: rdfi, ReceiverAccount: account1, Amount: 200000, TranCode: 22, Trace: "t2" + account1})
	h2, _ := st.CreateHold(ctx, en2.ID, domain.HoldApproved, "")
	art, _ := st.CreateArtifact(ctx, domain.Artifact{
		SubmissionID: subID, Kind: domain.ArtifactRelease, Checksum: "sum", State: domain.ArtifactPublished,
	})
	_ = st.SetHoldReleaseArtifact(ctx, holdID, art.ID)
	_ = st.SetHoldReleaseArtifact(ctx, h2.ID, art.ID)

	n, err := OverrideHoldStatus(ctx, d, holdID, "admin", "this hold only", domain.HoldDeclined, false)
	if err != nil {
		t.Fatalf("override: %v", err)
	}
	if n != 1 {
		t.Fatalf("changed = %d, want 1 (single override)", n)
	}
	got, _ := st.GetHold(ctx, holdID)
	if got.Status != domain.HoldDeclined {
		t.Fatalf("target hold status = %s, want declined", got.Status)
	}
	sibling, _ := st.GetHold(ctx, h2.ID)
	if sibling.Status != domain.HoldApproved {
		t.Fatalf("sibling status = %s, want approved (untouched)", sibling.Status)
	}
}

func TestManuallyCreateHold(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	d, st := overrideDeps(t, now)
	ctx := context.Background()
	sub, _ := st.CreateSubmission(ctx, domain.Submission{Filename: "a.ach", Status: domain.SubmissionReady, ReceivedAt: now})
	hdr, _ := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: sub.ID, CustomerID: "12104288", EffectiveDate: &now})
	en, _ := st.CreateBatchEntry(ctx, domain.BatchEntry{HeaderID: hdr.ID, Rdfi: rdfi, ReceiverAccount: account1, Amount: 200000, TranCode: 22, Trace: "t1"})

	holdID, err := ManuallyCreateHold(ctx, d, en.ID, "analyst", "manual flag", domain.HoldApproved)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	h, _ := st.GetHold(ctx, holdID)
	if h.Status != domain.HoldApproved || h.EntryID != en.ID {
		t.Fatalf("unexpected hold: %+v", h)
	}
	reviews, _ := st.ListReviewsByHold(ctx, holdID)
	if len(reviews) != 1 || reviews[0].Actor != "analyst" || reviews[0].Action != "approved" {
		t.Fatalf("unexpected reviews: %+v", reviews)
	}
}

func TestManuallyCreateHoldRejectsInvalidStatus(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	d, st := overrideDeps(t, now)
	ctx := context.Background()
	sub, _ := st.CreateSubmission(ctx, domain.Submission{Filename: "a.ach", Status: domain.SubmissionReady, ReceivedAt: now})
	hdr, _ := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: sub.ID, CustomerID: "12104288", EffectiveDate: &now})
	en, _ := st.CreateBatchEntry(ctx, domain.BatchEntry{HeaderID: hdr.ID, Rdfi: rdfi, ReceiverAccount: account1, Amount: 200000, TranCode: 22, Trace: "t1"})

	_, err := ManuallyCreateHold(ctx, d, en.ID, "analyst", "", domain.HoldStatus("bogus"))
	if !errors.Is(err, ErrInvalidHoldStatus) {
		t.Fatalf("expected ErrInvalidHoldStatus, got %v", err)
	}
}

func TestManuallyCreateHoldRejectsPending(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	d, st := overrideDeps(t, now)
	ctx := context.Background()
	sub, _ := st.CreateSubmission(ctx, domain.Submission{Filename: "a.ach", Status: domain.SubmissionReady, ReceivedAt: now})
	hdr, _ := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: sub.ID, CustomerID: "12104288", EffectiveDate: &now})
	en, _ := st.CreateBatchEntry(ctx, domain.BatchEntry{HeaderID: hdr.ID, Rdfi: rdfi, ReceiverAccount: account1, Amount: 200000, TranCode: 22, Trace: "t1"})

	_, err := ManuallyCreateHold(ctx, d, en.ID, "analyst", "", domain.HoldPending)
	if !errors.Is(err, ErrInvalidHoldStatus) {
		t.Fatalf("expected ErrInvalidHoldStatus for pending, got %v", err)
	}
}

func TestManuallyCreateHoldRejectsAutoDeclined(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	d, st := overrideDeps(t, now)
	ctx := context.Background()
	sub, _ := st.CreateSubmission(ctx, domain.Submission{Filename: "a.ach", Status: domain.SubmissionReady, ReceivedAt: now})
	hdr, _ := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: sub.ID, CustomerID: "12104288", EffectiveDate: &now})
	en, _ := st.CreateBatchEntry(ctx, domain.BatchEntry{HeaderID: hdr.ID, Rdfi: rdfi, ReceiverAccount: account1, Amount: 200000, TranCode: 22, Trace: "t1"})

	_, err := ManuallyCreateHold(ctx, d, en.ID, "analyst", "", domain.HoldAutoDeclined)
	if !errors.Is(err, ErrInvalidHoldStatus) {
		t.Fatalf("expected ErrInvalidHoldStatus for auto_declined, got %v", err)
	}
}

func TestManuallyCreateHoldNotFound(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	d, _ := overrideDeps(t, now)
	_, err := ManuallyCreateHold(context.Background(), d, uuid.New(), "analyst", "", domain.HoldApproved)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
