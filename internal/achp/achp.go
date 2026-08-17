// Package achp holds the pure ACH parsing and rewriting logic of the pipeline.
// It operates on parsed *ach.File values and domain types; it never touches
// the database, the filesystem, or any other infrastructure, so every rule here
// is unit-testable in isolation.
package achp

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
	"github.com/moov-io/ach"
)

// Read parses ACH bytes. When lenient is true, batch-level validation is
// skipped so files with bad control totals can be read and repaired. Company
// field checks in the batch header are skipped too, since the rebuild path
// runs Header.Validate() directly and those checks are gated on
// SkipBatchHeaderCompanyValidation rather than BypassBatchValidation.
func Read(data []byte, lenient bool) (*ach.File, error) {
	reader := ach.NewReader(bytes.NewReader(data))
	if lenient {
		reader.SetValidation(&ach.ValidateOpts{
			BypassBatchValidation:            true,
			SkipBatchHeaderCompanyValidation: true,
		})
	}
	file, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading ACH: %w", err)
	}
	return &file, nil
}

// Write serializes an ACH file.
func Write(file *ach.File) ([]byte, error) {
	var buf bytes.Buffer
	w := ach.NewWriter(&buf)
	if err := w.Write(file); err != nil {
		return nil, fmt.Errorf("writing ACH: %w", err)
	}
	if err := w.Flush(); err != nil {
		return nil, fmt.Errorf("writing ACH: %w", err)
	}
	return buf.Bytes(), nil
}

// Rebuild reconstructs an ACH file from its batches so trace numbers, batch
// numbers, and batch/file control totals are recomputed. IAT batches are copied
// through unchanged since ach.NewBatch does not support the IAT SEC code.
func Rebuild(file *ach.File) (*ach.File, error) {
	return rebuild(file, nil)
}

// rebuild reconstructs an ACH file from its batches so trace numbers, batch
// numbers, and batch/file control totals are recomputed. When transform is
// non-nil it is called for each entry and may return a replacement entry;
// the caller's file is never mutated.
func rebuild(file *ach.File, transform func(*ach.EntryDetail) *ach.EntryDetail) (*ach.File, error) {
	out := ach.NewFile()
	out.SetHeader(file.Header)
	out.SetValidation(file.GetValidation())

	for _, batch := range file.Batches {
		header := *batch.GetHeader()
		header.BatchNumber = 0

		fixed, err := ach.NewBatch(&header)
		if err != nil {
			return nil, fmt.Errorf("creating batch: %w", err)
		}
		// The source file may have been read leniently (e.g. a CCD entry with
		// more addenda records than the batch type allows); the rebuilt batch
		// must carry the same validation opts so Create() does not reject it.
		fixed.SetValidation(file.GetValidation())
		for _, entry := range batch.GetEntries() {
			e := entry
			if transform != nil {
				e = transform(entry)
			}
			fixed.AddEntry(e)
		}
		if err := fixed.Create(); err != nil {
			return nil, fmt.Errorf("rebuilding batch: %w", err)
		}
		out.AddBatch(fixed)
	}
	for _, iat := range file.IATBatches {
		out.AddIATBatch(iat)
	}

	if err := out.Create(); err != nil {
		return nil, fmt.Errorf("rebuilding file: %w", err)
	}
	return out, nil
}

// ParsedBatch is a batch header plus its entries, ready for import.
type ParsedBatch struct {
	Header  domain.BatchHeader
	Entries []domain.BatchEntry
}

// ParseBatches converts the file's batches into domain batch data. IAT batches
// are skipped (they are not imported into batch_headers/batch_entries).
func ParseBatches(file *ach.File) ([]ParsedBatch, error) {
	var out []ParsedBatch
	for i, batch := range file.Batches {
		hdr := batch.GetHeader()
		eff, err := parseEffectiveDate(hdr.EffectiveEntryDate)
		if err != nil {
			return nil, fmt.Errorf("batch %d: %w", i+1, err)
		}
		pb := ParsedBatch{
			Header: domain.BatchHeader{
				CustomerID:         hdr.CompanyIdentification,
				CompanyName:        hdr.CompanyName,
				CompanyDescription: hdr.CompanyEntryDescription,
				EffectiveDate:      eff,
			},
		}
		for _, entry := range batch.GetEntries() {
			pb.Entries = append(pb.Entries, domain.BatchEntry{
				Rdfi:            entry.RDFIIdentification + entry.CheckDigit,
				ReceiverName:    strings.TrimSpace(entry.IndividualName),
				ReceiverAccount: strings.TrimSpace(entry.DFIAccountNumber),
				Amount:          int64(entry.Amount),
				TranCode:        entry.TransactionCode,
				Trace:           entry.TraceNumberField(),
			})
		}
		out = append(out, pb)
	}
	return out, nil
}

func parseEffectiveDate(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse("060102", s)
	if err != nil {
		return nil, fmt.Errorf("effective date %q: %w", s, err)
	}
	return &t, nil
}

// EntryRef is a batch entry with a pointer to its owning header.
type EntryRef struct {
	Header *ach.BatchHeader
	Entry  *ach.EntryDetail
}

// IndexEntries groups a file's entries by trace number.
func IndexEntries(file *ach.File) map[string][]*EntryRef {
	byTrace := map[string][]*EntryRef{}
	for _, batch := range file.Batches {
		for _, entry := range batch.GetEntries() {
			byTrace[entry.TraceNumberField()] = append(byTrace[entry.TraceNumberField()], &EntryRef{
				Header: batch.GetHeader(),
				Entry:  entry,
			})
		}
	}
	return byTrace
}

// HeldEntry is a hold matched to its entry in the fixed file. The Orig* fields
// capture the receiver before the entry is redirected to the holding account
// (BuildCleaned mutates the entry in place), so release legs and consistency
// checks always target the real receiver.
type HeldEntry struct {
	Hold   domain.Hold
	Header *ach.BatchHeader
	Entry  *ach.EntryDetail

	OrigRDFI     string
	OrigCheck    string
	OrigAccount  string
	OrigName     string
	OrigAmount   int
	OrigTranCode int
}

// MatchHolds resolves each hold to its entry by trace number, cross-checking
// the routing number, account, and amount so identical trace numbers across
// batches resolve to the right entry. Every hold must match or the file is
// inconsistent with its holds.
func MatchHolds(file *ach.File, holds []domain.Hold) ([]HeldEntry, error) {
	byTrace := IndexEntries(file)
	used := map[*EntryRef]bool{}
	out := make([]HeldEntry, 0, len(holds))
	for _, h := range holds {
		ref := matchOne(byTrace[h.EntryTrace], used, h)
		if ref == nil {
			return nil, fmt.Errorf("hold %s references entry %q not found in file", h.ID, h.EntryTrace)
		}
		out = append(out, HeldEntry{
			Hold: h, Header: ref.Header, Entry: ref.Entry,
			OrigRDFI:     ref.Entry.RDFIIdentification,
			OrigCheck:    ref.Entry.CheckDigit,
			OrigAccount:  ref.Entry.DFIAccountNumber,
			OrigName:     ref.Entry.IndividualName,
			OrigAmount:   ref.Entry.Amount,
			OrigTranCode: ref.Entry.TransactionCode,
		})
	}
	return out, nil
}

func matchOne(refs []*EntryRef, used map[*EntryRef]bool, h domain.Hold) *EntryRef {
	for _, ref := range refs {
		if used[ref] {
			continue
		}
		if ref.Entry.RDFIIdentification+ref.Entry.CheckDigit == h.EntryRdfi &&
			strings.TrimSpace(ref.Entry.DFIAccountNumber) == h.EntryReceiverAcct &&
			int64(ref.Entry.Amount) == h.EntryAmount {
			used[ref] = true
			return ref
		}
	}
	return nil
}

// VerifySame confirms the fixed file only repairs control totals: batch and
// entry counts match the original, and every entry keeps its amount, routing
// number, and account. No entries are rewritten by fix.
func VerifySame(original, fixed *ach.File) error {
	if len(fixed.Batches) != len(original.Batches) {
		return fmt.Errorf("fixed has %d batches, original has %d", len(fixed.Batches), len(original.Batches))
	}
	for bi, fb := range fixed.Batches {
		ob := original.Batches[bi]
		oe, fe := ob.GetEntries(), fb.GetEntries()
		if ob.GetHeader().CompanyIdentification != fb.GetHeader().CompanyIdentification ||
			ob.GetHeader().CompanyName != fb.GetHeader().CompanyName {
			return fmt.Errorf("batch %d: sender differs from original", bi+1)
		}
		if len(fe) != len(oe) {
			return fmt.Errorf("batch %d: fixed has %d entries, original has %d", bi+1, len(fe), len(oe))
		}
		for ei := range oe {
			if oe[ei].Amount != fe[ei].Amount {
				return fmt.Errorf("batch %d entry %d: amount %d in original != %d in fixed", bi+1, ei+1, oe[ei].Amount, fe[ei].Amount)
			}
			if oe[ei].RDFIIdentification+oe[ei].CheckDigit != fe[ei].RDFIIdentification+fe[ei].CheckDigit {
				return fmt.Errorf("batch %d entry %d: routing number differs from original", bi+1, ei+1)
			}
			if oe[ei].DFIAccountNumber != fe[ei].DFIAccountNumber {
				return fmt.Errorf("batch %d entry %d: account differs from original", bi+1, ei+1)
			}
		}
	}
	return nil
}

// BuildCleaned rewrites every held entry to the holding account at the holding
// RDFI and rebuilds the file. The input file is left pristine: held entries are
// copied, the redirect is applied to the copies, and non-held entries are
// carried through untouched. All entries keep their amounts; the money just
// stops at the holding account instead of reaching the receiver.
func BuildCleaned(file *ach.File, held []HeldEntry, policy domain.Policy) (*ach.File, error) {
	if len(policy.HoldingRDFI) != 9 {
		return nil, fmt.Errorf("holding_rdfi must be 9 digits, got %q", policy.HoldingRDFI)
	}
	if policy.HoldingAccount == "" {
		return nil, fmt.Errorf("holding_account is not configured")
	}
	heldSet := make(map[*ach.EntryDetail]bool, len(held))
	for _, hr := range held {
		heldSet[hr.Entry] = true
	}
	return rebuild(file, func(entry *ach.EntryDetail) *ach.EntryDetail {
		if !heldSet[entry] {
			return entry
		}
		cp := *entry
		cp.RDFIIdentification = policy.HoldingRDFI[:8]
		cp.CheckDigit = string(policy.HoldingRDFI[8])
		cp.DFIAccountNumber = policy.HoldingAccount
		return &cp
	})
}

// BuildRelease creates a file that moves the funds of pending/approved holds
// from the holding account back to their original receivers. releaseDate is the
// effective entry date for the release batches (YYMMDD), supplied by the caller
// so this package stays free of wall-clock time. It returns nil when there is
// nothing to release, along with the hold IDs covered by the release legs.
func BuildRelease(file *ach.File, held []HeldEntry, policy domain.Policy, releaseDate string) (*ach.File, int, []uuid.UUID, error) {
	type releaseGroup struct {
		header *ach.BatchHeader
		legs   []HeldEntry
	}
	groups := map[*ach.BatchHeader]*releaseGroup{}
	var order []*ach.BatchHeader
	for _, hr := range held {
		if hr.Hold.Status != domain.HoldPending && hr.Hold.Status != domain.HoldApproved {
			continue
		}
		g, ok := groups[hr.Header]
		if !ok {
			g = &releaseGroup{header: hr.Header}
			groups[hr.Header] = g
			order = append(order, hr.Header)
		}
		g.legs = append(g.legs, hr)
	}
	if len(order) == 0 {
		return nil, 0, nil, nil
	}

	out := ach.NewFile()
	hdr := file.Header
	hdr.ImmediateOrigin = policy.HoldingRDFI
	out.SetHeader(hdr)
	out.SetValidation(file.GetValidation())

	ids := make([]uuid.UUID, 0, len(held))
	legs := 0
	for _, header := range order {
		g := groups[header]
		// Preserve the original batch header; only the release's own
		// characteristics change: the money originates from the holding
		// account on the release date.
		bh := *g.header
		bh.ODFIIdentification = policy.HoldingRDFI[:8]
		bh.EffectiveEntryDate = releaseDate
		bh.BatchNumber = 0

		batch, err := ach.NewBatch(&bh)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("creating release batch: %w", err)
		}
		for _, hr := range g.legs {
			// Copy the original entry wholesale so the tax id/SSN, name, and
			// discretionary data survive, and restore the real receiver
			// regardless of any prior in-place redirect. Only the trace number
			// and addenda are regenerated.
			e := *hr.Entry
			e.RDFIIdentification = hr.OrigRDFI
			e.CheckDigit = hr.OrigCheck
			e.DFIAccountNumber = hr.OrigAccount
			e.IndividualName = hr.OrigName
			e.Amount = hr.OrigAmount
			e.TransactionCode = hr.OrigTranCode
			e.TraceNumber = ""
			// Releases are fresh credits: no addenda carry over.
			e.Addenda02 = nil
			e.Addenda05 = nil
			e.Addenda98 = nil
			e.Addenda98Refused = nil
			e.Addenda99 = nil
			e.Addenda99Contested = nil
			e.Addenda99Dishonored = nil
			e.AddendaRecordIndicator = 0
			batch.AddEntry(&e)
			ids = append(ids, hr.Hold.ID)
			legs++
		}
		if err := batch.Create(); err != nil {
			return nil, 0, nil, fmt.Errorf("creating release batch: %w", err)
		}
		out.AddBatch(batch)
	}
	if err := out.Create(); err != nil {
		return nil, 0, nil, fmt.Errorf("creating release file: %w", err)
	}
	return out, legs, ids, nil
}

// VerifyConsistency confirms the money moving through the cleaned and release
// files matches the fixed file: every entry keeps its amount, held entries are
// redirected to the holding account, and each release leg returns the exact
// amount to the original receiver account/RDFI.
func VerifyConsistency(fixed, cleaned, release *ach.File, held []HeldEntry, policy domain.Policy) error {
	heldByTrace := map[string][]HeldEntry{}
	for _, hr := range held {
		key := hr.Entry.TraceNumberField()
		heldByTrace[key] = append(heldByTrace[key], hr)
	}

	if len(cleaned.Batches) != len(fixed.Batches) {
		return fmt.Errorf("cleaned has %d batches, fixed has %d", len(cleaned.Batches), len(fixed.Batches))
	}
	var fixedTotal, cleanTotal int
	for bi, cb := range cleaned.Batches {
		fb := fixed.Batches[bi]
		fe, ce := fb.GetEntries(), cb.GetEntries()
		if fb.GetHeader().CompanyIdentification != cb.GetHeader().CompanyIdentification ||
			fb.GetHeader().CompanyName != cb.GetHeader().CompanyName {
			return fmt.Errorf("batch %d: sender differs from fixed", bi+1)
		}
		if len(fe) != len(ce) {
			return fmt.Errorf("batch %d: cleaned has %d entries, fixed has %d", bi+1, len(ce), len(fe))
		}
		for ei := range fe {
			fixedTotal += fe[ei].Amount
			cleanTotal += ce[ei].Amount
			if fe[ei].Amount != ce[ei].Amount {
				return fmt.Errorf("batch %d entry %d: amount %d in fixed != %d in cleaned", bi+1, ei+1, fe[ei].Amount, ce[ei].Amount)
			}
			hr := popHeld(heldByTrace, fe[ei].TraceNumberField(), fe[ei].Amount, fe[ei].RDFIIdentification+fe[ei].CheckDigit, fe[ei].DFIAccountNumber)
			if hr != nil {
				if ce[ei].RDFIIdentification+ce[ei].CheckDigit != policy.HoldingRDFI || ce[ei].DFIAccountNumber != policy.HoldingAccount {
					return fmt.Errorf("batch %d entry %d: held entry not redirected to the holding account", bi+1, ei+1)
				}
			} else if ce[ei].RDFIIdentification != fe[ei].RDFIIdentification ||
				ce[ei].CheckDigit != fe[ei].CheckDigit ||
				ce[ei].DFIAccountNumber != fe[ei].DFIAccountNumber {
				return fmt.Errorf("batch %d entry %d: destination changed for a non-held entry", bi+1, ei+1)
			}
		}
	}
	if fixedTotal != cleanTotal {
		return fmt.Errorf("cleaned total %d != fixed total %d", cleanTotal, fixedTotal)
	}

	if release != nil {
		if err := VerifyRelease(release, held, policy); err != nil {
			return err
		}
	}
	return nil
}

// VerifyRelease confirms a release file carries exactly the legs for the held
// entries it was built from: one leg per pending/approved hold, each moving the
// exact amount back to the original receiver account/RDFI with its tax id/SSN
// and a preserved batch header description, with totals equal.
func VerifyRelease(release *ach.File, held []HeldEntry, policy domain.Policy) error {
	if release.Header.ImmediateOrigin != policy.HoldingRDFI {
		return fmt.Errorf("release file origin %s != holding rdfi %s", release.Header.ImmediateOrigin, policy.HoldingRDFI)
	}
	expected := map[string]int{}
	expTotal := 0
	srcHeaders := map[string]bool{}
	for _, hr := range held {
		if hr.Hold.Status != domain.HoldPending && hr.Hold.Status != domain.HoldApproved {
			continue
		}
		key := fmt.Sprintf("%d|%s|%s|%s", hr.OrigAmount, hr.OrigRDFI+hr.OrigCheck, hr.OrigAccount, hr.Entry.IdentificationNumber)
		expected[key]++
		expTotal += hr.OrigAmount
		if hr.Header != nil {
			srcHeaders[releaseHeaderKey(hr.Header)] = true
		}
	}
	relTotal := 0
	for _, b := range release.Batches {
		if b.GetHeader().ODFIIdentification != policy.HoldingRDFI[:8] {
			return fmt.Errorf("release batch ODFI %s != holding rdfi %s", b.GetHeader().ODFIIdentification, policy.HoldingRDFI[:8])
		}
		if !srcHeaders[releaseHeaderKey(b.GetHeader())] {
			return fmt.Errorf("release batch header does not match a source batch header")
		}
		for _, leg := range b.GetEntries() {
			key := fmt.Sprintf("%d|%s|%s|%s", leg.Amount, leg.RDFIIdentification+leg.CheckDigit, leg.DFIAccountNumber, leg.IdentificationNumber)
			if expected[key] == 0 {
				return fmt.Errorf("release has an unexpected leg for %s", key)
			}
			expected[key]--
			relTotal += leg.Amount
		}
	}
	for key, n := range expected {
		if n > 0 {
			return fmt.Errorf("release is missing %d leg(s) for %s", n, key)
		}
	}
	if relTotal != expTotal {
		return fmt.Errorf("release total %d != held total %d", relTotal, expTotal)
	}
	return nil
}

// releaseHeaderKey fingerprints the batch fields a release must preserve from
// its source header: the sender (customer id + company name) and the entry
// description. ODFI and effective date are intentionally rewritten for the
// release, so they are not part of the fingerprint.
func releaseHeaderKey(h *ach.BatchHeader) string {
	return h.CompanyIdentification + "|" + h.CompanyName + "|" + h.CompanyEntryDescription
}

func popHeld(m map[string][]HeldEntry, trace string, amount int, rdfi, account string) *HeldEntry {
	list := m[trace]
	for i := range list {
		hr := &list[i]
		if hr.OrigAmount == amount && hr.OrigRDFI+hr.OrigCheck == rdfi && hr.OrigAccount == account {
			m[trace] = append(list[:i], list[i+1:]...)
			return hr
		}
	}
	return nil
}

// TotalEntries counts the entries across all regular batches.
func TotalEntries(file *ach.File) int {
	n := 0
	for _, batch := range file.Batches {
		n += len(batch.GetEntries())
	}
	return n
}
