package achp

import (
	"strings"
	"testing"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/testutil"

	"github.com/google/uuid"
	"github.com/moov-io/ach"
)

// savingsHeld builds a single savings-credit entry file and matches one hold.
func savingsHeld(t *testing.T) (*ach.File, []HeldEntry) {
	t.Helper()
	data, err := testutil.CreditFile([]testutil.Entry{
		{Account: "111111", Name: "Saver", Amount: 75000, RDFI: "231380104", Trace: 1, TranCode: ach.SavingsCredit},
	}, "260101")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	f := read(t, data)
	if got := f.Batches[0].GetEntries()[0].TransactionCode; got != ach.SavingsCredit {
		t.Fatalf("fixture tran = %d, want savings credit %d", got, ach.SavingsCredit)
	}
	held, err := MatchHolds(f, []domain.Hold{
		{ID: uuid.New(), EntryTrace: f.Batches[0].GetEntries()[0].TraceNumberField(), EntryRdfi: "231380104", EntryReceiverAcct: "111111", EntryAmount: 75000, Status: domain.HoldPending},
	})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	return f, held
}

// TestHoldingLegsUseCheckingCodes: a held savings credit parks in the holding
// account as a checking credit (22), the release returns the savings credit
// untouched, and the summary debit out of holding is a checking debit (27).
func TestHoldingLegsUseCheckingCodes(t *testing.T) {
	f, held := savingsHeld(t)

	cleaned, err := BuildCleaned(f, held, policy)
	if err != nil {
		t.Fatalf("cleaned: %v", err)
	}
	got := cleaned.Batches[0].GetEntries()[0]
	if got.RDFIIdentification+got.CheckDigit != policy.HoldingRDFI || got.DFIAccountNumber != policy.HoldingAccount {
		t.Fatalf("redirect wrong: %s %s", got.RDFIIdentification+got.CheckDigit, got.DFIAccountNumber)
	}
	if got.TransactionCode != ach.CheckingCredit {
		t.Fatalf("redirect tran = %d, want checking credit %d", got.TransactionCode, ach.CheckingCredit)
	}

	release, legs, _, err := BuildRelease(f, held, policy)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if legs != 1 {
		t.Fatalf("legs = %d, want 1", legs)
	}
	entries := release.Batches[0].GetEntries()
	if entries[0].TransactionCode != ach.SavingsCredit {
		t.Fatalf("release leg tran = %d, want savings credit %d", entries[0].TransactionCode, ach.SavingsCredit)
	}
	if entries[1].TransactionCode != ach.CheckingDebit {
		t.Fatalf("summary debit tran = %d, want checking debit %d", entries[1].TransactionCode, ach.CheckingDebit)
	}

	if err := VerifyConsistency(f, cleaned, release, held, policy); err != nil {
		t.Fatalf("consistency: %v", err)
	}
}

// TestVerifyRejectsWrongHoldingCodes: a redirect or summary debit carrying a
// non-checking tran code must fail verification.
func TestVerifyRejectsWrongHoldingCodes(t *testing.T) {
	f, held := savingsHeld(t)

	cleaned, err := BuildCleaned(f, held, policy)
	if err != nil {
		t.Fatalf("cleaned: %v", err)
	}
	cleaned.Batches[0].GetEntries()[0].TransactionCode = ach.SavingsCredit
	if err := VerifyConsistency(f, cleaned, nil, held, policy); err == nil || !strings.Contains(err.Error(), "tran") {
		t.Fatalf("expected a tran-code consistency failure, got %v", err)
	}

	release, _, _, err := BuildRelease(f, held, policy)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	release.Batches[0].GetEntries()[1].TransactionCode = ach.SavingsDebit
	if err := VerifyRelease(release, held, policy); err == nil || !strings.Contains(err.Error(), "tran") {
		t.Fatalf("expected a tran-code release failure, got %v", err)
	}
}
