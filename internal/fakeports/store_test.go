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
	if _, err := st.CreateHold(ctx, en1.ID, domain.HoldPending, ""); err != nil {
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
	if !byAcct["acct1"].HasHold || byAcct["acct1"].HasDeclined {
		t.Fatalf("acct1 should have a hold but no declined hold: %+v", byAcct["acct1"])
	}
	if !byAcct["acct2"].HasHold || !byAcct["acct2"].HasDeclined {
		t.Fatalf("acct2 should be flagged declined: %+v", byAcct["acct2"])
	}
}
