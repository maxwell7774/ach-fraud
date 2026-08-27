package fakeports

import (
	"context"
	"testing"
	"time"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

func TestRequeueJobResetsFailed(t *testing.T) {
	st := NewStore()
	ctx := context.Background()
	ref := uuid.New()
	if err := st.EnqueueJob(ctx, domain.JobFixSubmission, ref, time.Now()); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := st.ClaimDueJob(ctx)
	if err != nil || job == nil {
		t.Fatalf("claim: %v %v", job, err)
	}
	if err := st.FailJob(ctx, job.ID, "boom"); err != nil {
		t.Fatalf("fail: %v", err)
	}
	if got := st.Job(domain.JobFixSubmission, ref).State; got != domain.JobFailed {
		t.Fatalf("state = %s, want failed", got)
	}

	if err := st.RequeueJob(ctx, job.ID); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	requeued := st.Job(domain.JobFixSubmission, ref)
	if requeued.State != domain.JobQueued || requeued.LastError != "" {
		t.Fatalf("after requeue: state=%s err=%q", requeued.State, requeued.LastError)
	}
}

func TestRequeueJobUnknown(t *testing.T) {
	st := NewStore()
	if err := st.RequeueJob(context.Background(), uuid.New()); err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSetHoldStatusIfOpenIsConditional(t *testing.T) {
	st := NewStore()
	ctx := context.Background()
	sub, err := st.CreateSubmission(ctx, domain.Submission{Filename: "a.ach", Status: domain.SubmissionReady, ReceivedAt: time.Now()})
	if err != nil {
		t.Fatalf("submission: %v", err)
	}
	hdr, err := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: sub.ID})
	if err != nil {
		t.Fatalf("header: %v", err)
	}
	entry, err := st.CreateBatchEntry(ctx, domain.BatchEntry{HeaderID: hdr.ID, ReceiverAccount: "acct"})
	if err != nil {
		t.Fatalf("entry: %v", err)
	}
	hold, err := st.CreateHold(ctx, entry.ID, domain.HoldPending, "velocity")
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	changed, err := st.SetHoldStatusIfOpen(ctx, hold.ID, domain.HoldApproved)
	if err != nil || !changed {
		t.Fatalf("first transition: changed=%v err=%v", changed, err)
	}
	changed, err = st.SetHoldStatusIfOpen(ctx, hold.ID, domain.HoldDeclined)
	if err != nil || changed {
		t.Fatalf("stale transition: changed=%v err=%v", changed, err)
	}
}

func TestListCombosBySubmission(t *testing.T) {
	st := NewStore()
	ctx := context.Background()
	now := time.Now()
	sub, err := st.CreateSubmission(ctx, domain.Submission{Filename: "a.ach", Status: domain.SubmissionReady, ReceivedAt: now})
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	eff := now.AddDate(0, 0, -1)
	hdr, err := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: sub.ID, CustomerID: "c1", EffectiveDate: &eff})
	if err != nil {
		t.Fatalf("hdr: %v", err)
	}
	en1, err := st.CreateBatchEntry(ctx, domain.BatchEntry{HeaderID: hdr.ID, Rdfi: "231380104", ReceiverAccount: "acct1", TranCode: 22})
	if err != nil {
		t.Fatalf("entry: %v", err)
	}
	en2, err := st.CreateBatchEntry(ctx, domain.BatchEntry{HeaderID: hdr.ID, Rdfi: "231380104", ReceiverAccount: "acct2", TranCode: 22})
	if err != nil {
		t.Fatalf("entry: %v", err)
	}
	if _, err := st.CreateHold(ctx, en1.ID, domain.HoldApproved, ""); err != nil {
		t.Fatalf("hold: %v", err)
	}
	if _, err := st.CreateHold(ctx, en2.ID, domain.HoldDeclined, ""); err != nil {
		t.Fatalf("hold: %v", err)
	}

	combos, err := st.ListCombosBySubmission(ctx, sub.ID, now.AddDate(0, 0, -30))
	if err != nil {
		t.Fatalf("combos: %v", err)
	}
	if len(combos) != 2 {
		t.Fatalf("expected 2 combos, got %d", len(combos))
	}
	byAcct := map[string]domain.HoldCombo{}
	for _, c := range combos {
		byAcct[c.ReceiverAccount] = c
	}
	if byAcct["acct1"].LatestStatus != "approved" {
		t.Fatalf("acct1 should be whitelisted (approved): %+v", byAcct["acct1"])
	}
	if byAcct["acct2"].LatestStatus != "declined" {
		t.Fatalf("acct2 should be blacklisted (declined): %+v", byAcct["acct2"])
	}
}

func TestUpsertUserRoleSemantics(t *testing.T) {
	st := NewStore()
	ctx := context.Background()

	// New user without a role defaults to the least-privileged watcher.
	u, err := st.UpsertUser(ctx, domain.User{Subject: "sub-1", UPN: "a@corp.com", Name: "Alice", Email: "a@corp.com"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if u.ID == uuid.Nil || u.Role != domain.RoleWatcher {
		t.Fatalf("created user = %+v", u)
	}

	// Re-upsert with an empty role: profile refreshes, role is preserved.
	u2, err := st.UpsertUser(ctx, domain.User{Subject: "sub-1", UPN: "a@corp.com", Name: "Alice A.", Email: "a@corp.com"})
	if err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if u2.ID != u.ID || u2.Name != "Alice A." {
		t.Fatalf("re-upsert = %+v", u2)
	}
	if u2.Role != domain.RoleWatcher {
		t.Fatalf("role not preserved on empty: %q", u2.Role)
	}

	// Re-upsert with a role from Entra updates it.
	u3, err := st.UpsertUser(ctx, domain.User{Subject: "sub-1", UPN: "a@corp.com", Name: "Alice A.", Email: "a@corp.com", Role: domain.RoleAdmin})
	if err != nil {
		t.Fatalf("role upsert: %v", err)
	}
	if u3.Role != domain.RoleAdmin {
		t.Fatalf("role not updated: %q", u3.Role)
	}
	got, err := st.GetUserByID(ctx, u.ID)
	if err != nil || got.Role != domain.RoleAdmin {
		t.Fatalf("GetUserByID = %+v, %v", got, err)
	}
}

func TestVerificationUpsert(t *testing.T) {
	st := NewStore()
	ctx := context.Background()
	id := uuid.New()
	if _, err := st.GetVerification(ctx, id); err != domain.ErrNotFound {
		t.Fatalf("expected not found, got %v", err)
	}
	if err := st.UpsertVerification(ctx, id, true, ""); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	v, err := st.GetVerification(ctx, id)
	if err != nil || !v.Verified {
		t.Fatalf("get = %+v, %v", v, err)
	}
	if err := st.UpsertVerification(ctx, id, false, "boom"); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	v, _ = st.GetVerification(ctx, id)
	if v.Verified || v.Issues != "boom" {
		t.Fatalf("update not applied: %+v", v)
	}
}

func TestRecipientCRUD(t *testing.T) {
	st := NewStore()
	ctx := context.Background()
	rec, err := st.CreateRecipient(ctx, domain.Recipient{
		Email: "ops@corp.com", Name: "Ops", Enabled: true, AlertTypes: []string{domain.AlertPendingHolds},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if rec.ID == uuid.Nil || rec.Email != "ops@corp.com" {
		t.Fatalf("created = %+v", rec)
	}
	recs, err := st.ListRecipients(ctx)
	if err != nil || len(recs) != 1 {
		t.Fatalf("list = %+v, %v", recs, err)
	}
	rec.Enabled = false
	rec.AlertTypes = []string{domain.AlertFailed}
	updated, err := st.UpdateRecipient(ctx, rec)
	if err != nil || updated.Enabled || len(updated.AlertTypes) != 1 {
		t.Fatalf("update = %+v, %v", updated, err)
	}
	if err := st.DeleteRecipient(ctx, rec.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := st.DeleteRecipient(ctx, rec.ID); err != domain.ErrNotFound {
		t.Fatalf("delete missing = %v, want ErrNotFound", err)
	}
}

func TestSessionsCRUD(t *testing.T) {
	st := NewStore()
	ctx := context.Background()
	u, err := st.UpsertUser(ctx, domain.User{Subject: "sub-2", Name: "Bob"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	sess, err := st.CreateSession(ctx, domain.Session{UserID: u.ID, TokenHash: "hash-1", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	got, err := st.GetSessionByTokenHash(ctx, "hash-1")
	if err != nil || got.ID != sess.ID || got.UserID != u.ID {
		t.Fatalf("GetSessionByTokenHash = %+v, %v", got, err)
	}
	if _, err := st.GetSessionByTokenHash(ctx, "nope"); err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound for unknown hash, got %v", err)
	}
	if err := st.DeleteSession(ctx, sess.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.GetSessionByTokenHash(ctx, "hash-1"); err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}
