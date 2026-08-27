package achp

import (
	"strings"
	"testing"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/testutil"

	"github.com/google/uuid"
	"github.com/moov-io/ach"
)

var policy = domain.Policy{
	HoldingRDFI:    "333333334",
	HoldingAccount: "555555",
}

func read(t *testing.T, data []byte) *ach.File {
	t.Helper()
	f, err := Read(data, true)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return f
}

func mustCredit(t *testing.T, entries []testutil.Entry) []byte {
	t.Helper()
	data, err := testutil.CreditFile(entries, "260101")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return data
}

func TestRebuildRoundTrips(t *testing.T) {
	data := mustCredit(t, []testutil.Entry{
		{Account: "111111", Name: "A", Amount: 5000, RDFI: "231380104"},
		{Account: "222222", Name: "B", Amount: 90000, RDFI: "231380104"},
	})
	f := read(t, data)

	fixed, err := Rebuild(f)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if TotalEntries(fixed) != 2 {
		t.Fatalf("expected 2 entries, got %d", TotalEntries(fixed))
	}
	out, err := Write(fixed)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Read(out, false); err != nil {
		t.Fatalf("rebuilt file does not parse strictly: %v", err)
	}
}

// TestRebuildToleratesExtraAddenda: a file read leniently may carry more
// addenda records than its batch type allows (e.g. a CCD entry with two
// Addenda05 records). Rebuild must preserve the lenient validation rather than
// rejecting the reconstructed batch during Create().
func TestRebuildToleratesExtraAddenda(t *testing.T) {
	file := ach.NewFile()
	hdr := ach.NewFileHeader()
	hdr.ImmediateDestination = "121042882"
	hdr.ImmediateOrigin = "231380104"
	hdr.ImmediateDestinationName = "My Bank"
	hdr.ImmediateOriginName = "My Bank"
	hdr.FileCreationDate = "260101"
	file.SetHeader(hdr)

	bh := ach.NewBatchHeader()
	bh.ServiceClassCode = ach.CreditsOnly
	bh.CompanyName = "Acme Corp"
	bh.CompanyIdentification = "12104288"
	bh.StandardEntryClassCode = ach.CCD
	bh.CompanyEntryDescription = "PAYMENT"
	bh.ODFIIdentification = "12104288"
	bh.EffectiveEntryDate = "260101"

	ed := ach.NewEntryDetail()
	ed.TransactionCode = ach.CheckingCredit
	ed.RDFIIdentification = "23138010"
	ed.CheckDigit = "4"
	ed.DFIAccountNumber = "111111"
	ed.Amount = 5000
	ed.IndividualName = "A"
	ed.SetTraceNumber("12104288", 1)
	ed.AddendaRecordIndicator = 1

	a1 := ach.NewAddenda05()
	a1.PaymentRelatedInformation = "first"
	a2 := ach.NewAddenda05()
	a2.PaymentRelatedInformation = "second"
	ed.AddAddenda05(a1)
	ed.AddAddenda05(a2)

	batch, err := ach.NewBatch(bh)
	if err != nil {
		t.Fatalf("creating batch: %v", err)
	}
	batch.SetValidation(&ach.ValidateOpts{BypassBatchValidation: true})
	batch.AddEntry(ed)
	if err := batch.Create(); err != nil {
		t.Fatalf("building batch: %v", err)
	}
	file.AddBatch(batch)
	if err := file.Create(); err != nil {
		t.Fatalf("building file: %v", err)
	}

	var buf strings.Builder
	w := ach.NewWriter(&buf)
	if err := w.Write(file); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	parsed := read(t, []byte(buf.String()))
	entries := parsed.Batches[0].GetEntries()
	if len(entries) != 1 || len(entries[0].Addenda05) != 2 {
		t.Fatalf("expected 1 entry with 2 addenda, got %d entries / %d addenda", len(entries), len(entries[0].Addenda05))
	}

	fixed, err := Rebuild(parsed)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if TotalEntries(fixed) != 1 {
		t.Fatalf("expected 1 entry, got %d", TotalEntries(fixed))
	}
}

// TestRebuildToleratesBadCompanyDescription: the batch header's company
// description may be all zeros ("000"), which passes the lenient read but would
// fail the rebuilt batch's Create() because Header.Validate() checks it
// directly. Rebuild must carry the lenient opts so the fixed file round-trips.
func TestRebuildToleratesBadCompanyDescription(t *testing.T) {
	file := ach.NewFile()
	hdr := ach.NewFileHeader()
	hdr.ImmediateDestination = "121042882"
	hdr.ImmediateOrigin = "231380104"
	hdr.ImmediateDestinationName = "My Bank"
	hdr.ImmediateOriginName = "My Bank"
	hdr.FileCreationDate = "260101"
	file.SetHeader(hdr)

	bh := ach.NewBatchHeader()
	bh.ServiceClassCode = ach.CreditsOnly
	bh.CompanyName = "Acme Corp"
	bh.CompanyIdentification = "12104288"
	bh.StandardEntryClassCode = ach.PPD
	bh.CompanyEntryDescription = "000"
	bh.ODFIIdentification = "12104288"
	bh.EffectiveEntryDate = "260101"

	ed := ach.NewEntryDetail()
	ed.TransactionCode = ach.CheckingCredit
	ed.RDFIIdentification = "23138010"
	ed.CheckDigit = "4"
	ed.DFIAccountNumber = "111111"
	ed.Amount = 5000
	ed.IndividualName = "A"
	ed.SetTraceNumber("12104288", 1)

	batch, err := ach.NewBatch(bh)
	if err != nil {
		t.Fatalf("creating batch: %v", err)
	}
	batch.SetValidation(&ach.ValidateOpts{SkipBatchHeaderCompanyValidation: true})
	batch.AddEntry(ed)
	if err := batch.Create(); err != nil {
		t.Fatalf("building batch: %v", err)
	}
	file.AddBatch(batch)
	if err := file.Create(); err != nil {
		t.Fatalf("building file: %v", err)
	}

	var buf strings.Builder
	w := ach.NewWriter(&buf)
	if err := w.Write(file); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	parsed := read(t, []byte(buf.String()))
	if got := parsed.Batches[0].GetHeader().CompanyEntryDescription; got != "000" {
		t.Fatalf("description = %q, want 000", got)
	}

	fixed, err := Rebuild(parsed)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if TotalEntries(fixed) != 1 {
		t.Fatalf("expected 1 entry, got %d", TotalEntries(fixed))
	}
}

func TestVerifySame(t *testing.T) {
	a := read(t, mustCredit(t, []testutil.Entry{{Account: "111111", Name: "A", Amount: 5000, RDFI: "231380104"}}))
	b := read(t, mustCredit(t, []testutil.Entry{{Account: "111111", Name: "A", Amount: 5000, RDFI: "231380104"}}))
	if err := VerifySame(a, b); err != nil {
		t.Fatalf("identical files differ: %v", err)
	}

	c := read(t, mustCredit(t, []testutil.Entry{{Account: "111111", Name: "A", Amount: 9999, RDFI: "231380104"}}))
	if err := VerifySame(a, c); err == nil {
		t.Fatal("expected amount mismatch to fail VerifySame")
	}
}

func TestMatchHoldsAndCleaned(t *testing.T) {
	data := mustCredit(t, []testutil.Entry{
		{Account: "111111", Name: "A", Amount: 200000, RDFI: "231380104", Trace: 1},
		{Account: "222222", Name: "B", Amount: 5000, RDFI: "231380104", Trace: 2},
	})
	f := read(t, data)
	byTrace := IndexEntries(f)
	if len(byTrace) != 2 {
		t.Fatalf("expected 2 trace groups, got %d", len(byTrace))
	}
	trace0 := f.Batches[0].GetEntries()[0].TraceNumberField()

	holds := []domain.Hold{
		{ID: uuid.New(), EntryTrace: trace0, EntryRdfi: "231380104", EntryReceiverAcct: "111111", EntryAmount: 200000, Status: domain.HoldPending},
	}
	held, err := MatchHolds(f, holds)
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(held) != 1 {
		t.Fatalf("expected 1 match, got %d", len(held))
	}

	cleaned, err := BuildCleaned(f, held, policy)
	if err != nil {
		t.Fatalf("cleaned: %v", err)
	}
	ce := cleaned.Batches[0].GetEntries()
	if ce[0].RDFIIdentification+ce[0].CheckDigit != "333333334" || ce[0].DFIAccountNumber != "555555" {
		t.Fatalf("held entry not redirected: %s %s", ce[0].RDFIIdentification+ce[0].CheckDigit, ce[0].DFIAccountNumber)
	}
	if ce[1].RDFIIdentification+ce[1].CheckDigit != "231380104" || ce[1].DFIAccountNumber != "222222" {
		t.Fatalf("non-held entry was altered: %s %s", ce[1].RDFIIdentification+ce[1].CheckDigit, ce[1].DFIAccountNumber)
	}
	if ce[0].Amount != 200000 || ce[1].Amount != 5000 {
		t.Fatalf("amounts changed")
	}

	release, legs, ids, err := BuildRelease(f, held, policy)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if release == nil || legs != 1 || len(ids) != 1 {
		t.Fatalf("expected 1 release leg, got legs=%d ids=%d", legs, len(ids))
	}
	leg := release.Batches[0].GetEntries()[0]
	if leg.RDFIIdentification+leg.CheckDigit != "231380104" || leg.DFIAccountNumber != "111111" || leg.Amount != 200000 {
		t.Fatalf("release leg wrong: %s %s %d", leg.RDFIIdentification+leg.CheckDigit, leg.DFIAccountNumber, leg.Amount)
	}

	if err := VerifyConsistency(f, cleaned, release, held, policy); err != nil {
		t.Fatalf("consistency: %v", err)
	}
}

// TestBuildReleasePreservesEntryAndHeader: the release leg keeps the original
// entry's tax id/SSN, name, and discretionary data, and the batch header keeps
// the original description; only the ODFI (holding account) and effective date
// differ from the source file.
func TestBuildReleasePreservesEntryAndHeader(t *testing.T) {
	data, err := testutil.CreditFile([]testutil.Entry{
		{Account: "111111", Name: "Jane Doe", Identification: "123456789", Amount: 200000, RDFI: "231380104", Trace: 1},
		{Account: "222222", Name: "B", Amount: 5000, RDFI: "231380104", Trace: 2},
	}, "260215")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	f := read(t, data)
	trace0 := f.Batches[0].GetEntries()[0].TraceNumberField()
	held, err := MatchHolds(f, []domain.Hold{
		{ID: uuid.New(), EntryTrace: trace0, EntryRdfi: "231380104", EntryReceiverAcct: "111111", EntryAmount: 200000, Status: domain.HoldPending},
	})
	if err != nil {
		t.Fatalf("match: %v", err)
	}

	release, legs, _, err := BuildRelease(f, held, policy)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if release == nil || legs != 1 {
		t.Fatalf("expected 1 release leg, got %d", legs)
	}

	src := f.Batches[0].GetHeader()
	rh := release.Batches[0].GetHeader()
	if rh.CompanyEntryDescription != src.CompanyEntryDescription {
		t.Fatalf("description = %q, want %q", rh.CompanyEntryDescription, src.CompanyEntryDescription)
	}
	if rh.CompanyName != src.CompanyName || rh.CompanyIdentification != src.CompanyIdentification ||
		rh.StandardEntryClassCode != src.StandardEntryClassCode {
		t.Fatalf("batch header fields not preserved: %+v", rh)
	}
	if rh.ODFIIdentification != policy.HoldingRDFI[:8] {
		t.Fatalf("ODFI = %s, want holding rdfi", rh.ODFIIdentification)
	}
	if rh.EffectiveEntryDate != "260215" {
		t.Fatalf("effective date = %q, want original file date 260215", rh.EffectiveEntryDate)
	}

	leg := release.Batches[0].GetEntries()[0]
	if strings.TrimSpace(leg.IdentificationNumber) != "123456789" {
		t.Fatalf("tax id = %q, want 123456789", leg.IdentificationNumber)
	}
	if strings.TrimSpace(leg.IndividualName) != "Jane Doe" {
		t.Fatalf("name = %q, want Jane Doe", leg.IndividualName)
	}
	if leg.RDFIIdentification+leg.CheckDigit != "231380104" || leg.DFIAccountNumber != "111111" || leg.Amount != 200000 {
		t.Fatalf("release leg wrong: %s %s %d", leg.RDFIIdentification+leg.CheckDigit, leg.DFIAccountNumber, leg.Amount)
	}

	if err := VerifyRelease(release, held, policy); err != nil {
		t.Fatalf("verify release: %v", err)
	}

	// Mirror the pipeline's write + re-read round trip.
	relData, err := Write(release)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := VerifyRelease(read(t, relData), held, policy); err != nil {
		t.Fatalf("verify release after round trip: %v", err)
	}
}

// TestBuildCleanedLeavesInputUntouched: BuildCleaned must not mutate the fixed
// file, so VerifyConsistency can genuinely verify the redirect to the holding
// account rather than comparing two identically-mutated files.
func TestBuildCleanedLeavesInputUntouched(t *testing.T) {
	data := mustCredit(t, []testutil.Entry{
		{Account: "111111", Name: "A", Amount: 200000, RDFI: "231380104", Trace: 1},
		{Account: "222222", Name: "B", Amount: 5000, RDFI: "231380104", Trace: 2},
	})
	f := read(t, data)
	trace0 := f.Batches[0].GetEntries()[0].TraceNumberField()
	held, err := MatchHolds(f, []domain.Hold{
		{ID: uuid.New(), EntryTrace: trace0, EntryRdfi: "231380104", EntryReceiverAcct: "111111", EntryAmount: 200000, Status: domain.HoldPending},
	})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	cleaned, err := BuildCleaned(f, held, policy)
	if err != nil {
		t.Fatalf("cleaned: %v", err)
	}

	// The fixed file must still carry the original receiver.
	fe := f.Batches[0].GetEntries()
	if fe[0].RDFIIdentification+fe[0].CheckDigit != "231380104" || fe[0].DFIAccountNumber != "111111" {
		t.Fatalf("BuildCleaned mutated the fixed file: %s %s", fe[0].RDFIIdentification+fe[0].CheckDigit, fe[0].DFIAccountNumber)
	}

	// VerifyConsistency must now genuinely see the redirect.
	if err := VerifyConsistency(f, cleaned, nil, held, policy); err != nil {
		t.Fatalf("consistency: %v", err)
	}

	// And it must reject a cleaned file that did NOT redirect the held entry.
	bad, err := Rebuild(f)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if err := VerifyConsistency(f, bad, nil, held, policy); err == nil {
		t.Fatal("expected a missing redirect to fail consistency")
	}
}

func TestVerifyConsistencyDetectsMismatch(t *testing.T) {
	data := mustCredit(t, []testutil.Entry{{Account: "111111", Name: "A", Amount: 200000, RDFI: "231380104", Trace: 1}})
	f := read(t, data)
	trace := f.Batches[0].GetEntries()[0].TraceNumberField()
	held, err := MatchHolds(f, []domain.Hold{
		{ID: uuid.New(), EntryTrace: trace, EntryRdfi: "231380104", EntryReceiverAcct: "111111", EntryAmount: 200000, Status: domain.HoldPending},
	})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	cleaned, err := BuildCleaned(f, held, policy)
	if err != nil {
		t.Fatalf("cleaned: %v", err)
	}
	// A release that moves the money to the wrong account must be rejected.
	wrongRelease, _, _, err := BuildRelease(f, held, policy)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	wrongRelease.Batches[0].GetEntries()[0].DFIAccountNumber = "999999"
	if err := VerifyConsistency(f, cleaned, wrongRelease, held, policy); err == nil {
		t.Fatal("expected mismatch to fail consistency")
	}
}

func TestVerifySameDetectsSenderMismatch(t *testing.T) {
	data := mustCredit(t, []testutil.Entry{
		{Account: "111111", Name: "A", Amount: 200000, RDFI: "231380104", Trace: 1},
	})
	f := read(t, data)
	fixed := read(t, data)
	fixed.Batches[0].GetHeader().CompanyIdentification = "other-sender"
	if err := VerifySame(f, fixed); err == nil || !strings.Contains(err.Error(), "sender") {
		t.Fatalf("expected sender mismatch, got %v", err)
	}
}

func TestVerifyConsistencyDetectsSenderMismatch(t *testing.T) {
	data := mustCredit(t, []testutil.Entry{
		{Account: "111111", Name: "A", Amount: 200000, RDFI: "231380104", Trace: 1},
		{Account: "222222", Name: "B", Amount: 5000, RDFI: "231380104", Trace: 2},
	})
	f := read(t, data)
	trace0 := f.Batches[0].GetEntries()[0].TraceNumberField()
	held, err := MatchHolds(f, []domain.Hold{
		{ID: uuid.New(), EntryTrace: trace0, EntryRdfi: "231380104", EntryReceiverAcct: "111111", EntryAmount: 200000, Status: domain.HoldPending},
	})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	cleaned, err := BuildCleaned(f, held, policy)
	if err != nil {
		t.Fatalf("cleaned: %v", err)
	}
	cleaned.Batches[0].GetHeader().CompanyName = "wrong-company"
	if err := VerifyConsistency(f, cleaned, nil, held, policy); err == nil || !strings.Contains(err.Error(), "sender") {
		t.Fatalf("expected sender mismatch, got %v", err)
	}
}

func TestVerifyReleaseRejectsWrongSender(t *testing.T) {
	data := mustCredit(t, []testutil.Entry{
		{Account: "111111", Name: "A", Amount: 200000, RDFI: "231380104", Trace: 1},
	})
	f := read(t, data)
	trace0 := f.Batches[0].GetEntries()[0].TraceNumberField()
	held, err := MatchHolds(f, []domain.Hold{
		{ID: uuid.New(), EntryTrace: trace0, EntryRdfi: "231380104", EntryReceiverAcct: "111111", EntryAmount: 200000, Status: domain.HoldPending},
	})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	release, legs, _, err := BuildRelease(f, held, policy)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if release == nil || legs != 1 {
		t.Fatalf("expected 1 leg, got %d", legs)
	}
	release.Batches[0].GetHeader().CompanyIdentification = "wrong-sender"
	if err := VerifyRelease(release, held, policy); err == nil || !strings.Contains(err.Error(), "source batch") {
		t.Fatalf("expected source-header mismatch, got %v", err)
	}
}

func TestParseBatches(t *testing.T) {
	data := mustCredit(t, []testutil.Entry{{Account: "111111", Name: "A", Amount: 5000, RDFI: "231380104"}})
	f := read(t, data)
	batches, err := ParseBatches(f)
	if err != nil {
		t.Fatalf("parse batches: %v", err)
	}
	if len(batches) != 1 || len(batches[0].Entries) != 1 {
		t.Fatalf("unexpected batches: %+v", batches)
	}
	if batches[0].Entries[0].Rdfi != "231380104" || batches[0].Entries[0].Amount != 5000 {
		t.Fatalf("entry mapped wrong: %+v", batches[0].Entries[0])
	}
	if batches[0].Header.EffectiveDate == nil {
		t.Fatal("effective date not parsed")
	}
}
