package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/27actions/ach/internal/checksum"
	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/testutil"
)

// TestFixStampsTotals: the original row is created without totals at ingest,
// and FixSubmission stamps both the original and the fixed rows from the
// parsed bytes.
func TestFixStampsTotals(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	d, st := screenDeps(t, now)
	ctx := context.Background()

	data, err := testutil.CreditFile([]testutil.Entry{
		{Account: account1, Name: "A", Amount: 5000, RDFI: rdfi},
		{Account: account2, Name: "B", Amount: 90000, RDFI: rdfi},
	}, "260115")
	if err != nil {
		t.Fatalf("credit file: %v", err)
	}
	sum := checksum.Bytes(data)
	if err := d.Files.Put(ctx, sum, data); err != nil {
		t.Fatalf("put: %v", err)
	}
	sub, err := st.CreateSubmission(ctx, domain.Submission{
		Filename: "totals.ach", Status: domain.SubmissionReceived, ReceivedAt: now,
	})
	if err != nil {
		t.Fatalf("submission: %v", err)
	}
	orig, err := st.CreateArtifact(ctx, domain.Artifact{
		SubmissionID: sub.ID, Kind: domain.ArtifactOriginal, Checksum: sum, State: domain.ArtifactStaged,
	})
	if err != nil {
		t.Fatalf("original: %v", err)
	}
	if orig.DebitTotal != nil || orig.CreditTotal != nil {
		t.Fatalf("original totals = %v/%v, want nil before fix", orig.DebitTotal, orig.CreditTotal)
	}

	if err := FixSubmission(ctx, d, sub.ID); err != nil {
		t.Fatalf("fix: %v", err)
	}

	for _, kind := range []domain.ArtifactKind{domain.ArtifactOriginal, domain.ArtifactFixed} {
		a, err := st.GetArtifactBySubmissionKind(ctx, sub.ID, kind)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		checkTotal(t, kind, "debit_total", a.DebitTotal, 0)
		checkTotal(t, kind, "credit_total", a.CreditTotal, 95000)
		checkCount(t, kind, "debit_entries", a.DebitEntries, 0)
		checkCount(t, kind, "credit_entries", a.CreditEntries, 2)
	}
}

func checkTotal(t *testing.T, kind domain.ArtifactKind, name string, got *int64, want int64) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s %s = nil, want %d", kind, name, want)
	}
	if *got != want {
		t.Fatalf("%s %s = %d, want %d", kind, name, *got, want)
	}
}

func checkCount(t *testing.T, kind domain.ArtifactKind, name string, got *int, want int) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s %s = nil, want %d", kind, name, want)
	}
	if *got != want {
		t.Fatalf("%s %s = %d, want %d", kind, name, *got, want)
	}
}
