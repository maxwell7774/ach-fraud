// Package testutil builds ACH files for tests.
package testutil

import (
	"bytes"
	"fmt"
	"time"

	"github.com/moov-io/ach"
)

// Entry describes one credit entry in a built file.
type Entry struct {
	Account        string // DFI account number
	Name           string // individual name
	Identification string // IndividualIdentification (receiver tax id / SSN)
	Amount         int    // cents
	RDFI           string // 9-digit routing number
	Trace          int    // sequence for the trace number
	TranCode       int    // entry tran code; defaults to checking credit (22) when 0
}

// CreditFile builds a single-batch PPD credit file with the given entries and
// returns the serialized bytes. effectiveDate is a YYMMDD string.
func CreditFile(entries []Entry, effectiveDate string) ([]byte, error) {
	if effectiveDate == "" {
		effectiveDate = time.Now().Format("060102")
	}

	file := ach.NewFile()
	hdr := ach.NewFileHeader()
	hdr.ImmediateDestination = "121042882"
	hdr.ImmediateOrigin = "231380104"
	hdr.ImmediateDestinationName = "My Bank"
	hdr.ImmediateOriginName = "My Bank"
	hdr.FileCreationDate = effectiveDate
	file.SetHeader(hdr)

	bh := ach.NewBatchHeader()
	bh.ServiceClassCode = ach.CreditsOnly
	bh.CompanyName = "Acme Corp"
	bh.CompanyIdentification = "12104288"
	bh.StandardEntryClassCode = ach.PPD
	bh.CompanyEntryDescription = "REG.SALARY"
	bh.ODFIIdentification = "12104288"
	bh.EffectiveEntryDate = effectiveDate

	batch, err := ach.NewBatch(bh)
	if err != nil {
		return nil, fmt.Errorf("creating batch: %w", err)
	}
	for i, e := range entries {
		if len(e.RDFI) != 9 {
			return nil, fmt.Errorf("entry %d: rdfi must be 9 digits, got %q", i, e.RDFI)
		}
		ed := ach.NewEntryDetail()
		ed.TransactionCode = e.TranCode
		if ed.TransactionCode == 0 {
			ed.TransactionCode = ach.CheckingCredit
		}
		ed.RDFIIdentification = e.RDFI[:8]
		ed.CheckDigit = e.RDFI[8:]
		ed.DFIAccountNumber = e.Account
		ed.Amount = e.Amount
		ed.IdentificationNumber = e.Identification
		ed.IndividualName = e.Name
		seq := e.Trace
		if seq == 0 {
			seq = i + 1
		}
		ed.SetTraceNumber(bh.ODFIIdentification, seq)
		batch.AddEntry(ed)
	}
	if err := batch.Create(); err != nil {
		return nil, fmt.Errorf("building batch: %w", err)
	}
	file.AddBatch(batch)
	if err := file.Create(); err != nil {
		return nil, fmt.Errorf("building file: %w", err)
	}

	var buf bytes.Buffer
	w := ach.NewWriter(&buf)
	if err := w.Write(file); err != nil {
		return nil, err
	}
	if err := w.Flush(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
