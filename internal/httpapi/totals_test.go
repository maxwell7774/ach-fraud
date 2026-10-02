package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/27actions/ach/internal/checksum"
	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/testutil"
)

// TestVerifyComputesAndHealsTotals: artifact rows without stored splits get
// live-computed totals from the bytes, and the stored rows are healed.
func TestVerifyComputesAndHealsTotals(t *testing.T) {
	s, st := testServer(t)
	ctx := context.Background()
	sub, err := st.CreateSubmission(ctx, domain.Submission{Filename: "totals.ach", Status: domain.SubmissionReady, ReceivedAt: time.Now()})
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	data, err := testutil.CreditFile([]testutil.Entry{
		{Account: "100000001", Name: "Jane Doe", Amount: 5000, RDFI: "231380104", Trace: 1},
		{Account: "100000002", Name: "John Doe", Amount: 90000, RDFI: "231380104", Trace: 2},
	}, "260115")
	if err != nil {
		t.Fatalf("credit file: %v", err)
	}
	sum := checksum.Bytes(data)
	for _, kind := range []domain.ArtifactKind{domain.ArtifactOriginal, domain.ArtifactFixed} {
		if _, err := st.CreateArtifact(ctx, domain.Artifact{
			SubmissionID: sub.ID, Kind: kind, Checksum: sum, State: domain.ArtifactPublished,
		}); err != nil {
			t.Fatalf("artifact %s: %v", kind, err)
		}
	}
	if err := s.deps.Files.Put(ctx, sum, data); err != nil {
		t.Fatalf("put: %v", err)
	}

	var v submissionVerify
	rr := get(t, s, "/api/submissions/"+sub.ID.String()+"/verify")
	if rr.Code != http.StatusOK {
		t.Fatalf("verify = %d %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, kind := range []string{"original", "fixed"} {
		sum := v.Artifacts[kind]
		if sum.DebitTotal == nil || *sum.DebitTotal != 0 {
			t.Fatalf("%s debit_total = %v, want 0", kind, sum.DebitTotal)
		}
		if sum.CreditTotal == nil || *sum.CreditTotal != 95000 {
			t.Fatalf("%s credit_total = %v, want 95000", kind, sum.CreditTotal)
		}
		if sum.DebitEntries == nil || *sum.DebitEntries != 0 || sum.CreditEntries == nil || *sum.CreditEntries != 2 {
			t.Fatalf("%s entries = %v/%v, want 0/2", kind, sum.DebitEntries, sum.CreditEntries)
		}
	}

	// Lazy heal: the stored rows now carry the splits.
	for _, kind := range []domain.ArtifactKind{domain.ArtifactOriginal, domain.ArtifactFixed} {
		a, err := st.GetArtifactBySubmissionKind(ctx, sub.ID, kind)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if a.CreditTotal == nil || *a.CreditTotal != 95000 {
			t.Fatalf("stored %s credit_total = %v, want 95000", kind, a.CreditTotal)
		}
	}
}

// TestVerifyUsesStoredTotalsWhenPruned: with bytes gone, the splits stamped
// at creation are still reported.
func TestVerifyUsesStoredTotalsWhenPruned(t *testing.T) {
	s, st := testServer(t)
	ctx := context.Background()
	sub, err := st.CreateSubmission(ctx, domain.Submission{Filename: "pruned.ach", Status: domain.SubmissionReady, ReceivedAt: time.Now()})
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	debit, credit, debitN, creditN := int64(0), int64(95000), 0, 2
	if _, err := st.CreateArtifact(ctx, domain.Artifact{
		SubmissionID: sub.ID, Kind: domain.ArtifactOriginal, Checksum: "gone",
		State:      domain.ArtifactPruned,
		DebitTotal: &debit, CreditTotal: &credit, DebitEntries: &debitN, CreditEntries: &creditN,
	}); err != nil {
		t.Fatalf("artifact: %v", err)
	}

	var v submissionVerify
	rr := get(t, s, "/api/submissions/"+sub.ID.String()+"/verify")
	if rr.Code != http.StatusOK {
		t.Fatalf("verify = %d %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	sum := v.Artifacts["original"]
	if !sum.Pruned {
		t.Fatalf("pruned = false, want true")
	}
	if sum.CreditTotal == nil || *sum.CreditTotal != 95000 {
		t.Fatalf("credit_total = %v, want 95000", sum.CreditTotal)
	}
	if sum.DebitTotal == nil || *sum.DebitTotal != 0 {
		t.Fatalf("debit_total = %v, want 0", sum.DebitTotal)
	}
}
