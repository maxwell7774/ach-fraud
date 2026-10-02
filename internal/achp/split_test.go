package achp

import (
	"testing"

	"github.com/27actions/ach/internal/testutil"

	"github.com/moov-io/ach"
)

func TestSplitTotalsCreditOnly(t *testing.T) {
	data := mustCredit(t, []testutil.Entry{
		{Account: "111111", Name: "A", Amount: 5000, RDFI: "231380104"},
		{Account: "222222", Name: "B", Amount: 90000, RDFI: "231380104"},
	})
	d, c, dn, cn := SplitTotals(read(t, data))
	if d != 0 || dn != 0 {
		t.Fatalf("debit = %d (%d entries), want 0 (0)", d, dn)
	}
	if c != 95000 || cn != 2 {
		t.Fatalf("credit = %d (%d entries), want 95000 (2)", c, cn)
	}
}

func TestSplitTotalsMixed(t *testing.T) {
	file := ach.NewFile()
	hdr := ach.NewFileHeader()
	hdr.ImmediateDestination = "121042882"
	hdr.ImmediateOrigin = "231380104"
	hdr.ImmediateDestinationName = "My Bank"
	hdr.ImmediateOriginName = "My Bank"
	hdr.FileCreationDate = "260101"
	file.SetHeader(hdr)

	bh := ach.NewBatchHeader()
	bh.ServiceClassCode = ach.MixedDebitsAndCredits
	bh.CompanyName = "Acme Corp"
	bh.CompanyIdentification = "12104288"
	bh.StandardEntryClassCode = ach.PPD
	bh.CompanyEntryDescription = "MIXED"
	bh.ODFIIdentification = "12104288"
	bh.EffectiveEntryDate = "260101"

	batch, err := ach.NewBatch(bh)
	if err != nil {
		t.Fatalf("creating batch: %v", err)
	}
	add := func(code int, amount int, seq int) {
		ed := ach.NewEntryDetail()
		ed.TransactionCode = code
		ed.RDFIIdentification = "23138010"
		ed.CheckDigit = "4"
		ed.DFIAccountNumber = "111111"
		ed.Amount = amount
		ed.IndividualName = "A"
		ed.SetTraceNumber(bh.ODFIIdentification, seq)
		batch.AddEntry(ed)
	}
	add(ach.CheckingCredit, 5000, 1)
	add(ach.CheckingDebit, 3000, 2)
	add(ach.SavingsCredit, 7000, 3)
	if err := batch.Create(); err != nil {
		t.Fatalf("building batch: %v", err)
	}
	file.AddBatch(batch)
	if err := file.Create(); err != nil {
		t.Fatalf("building file: %v", err)
	}
	out, err := Write(file)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	d, c, dn, cn := SplitTotals(read(t, out))
	if d != 3000 || dn != 1 {
		t.Fatalf("debit = %d (%d entries), want 3000 (1)", d, dn)
	}
	if c != 12000 || cn != 2 {
		t.Fatalf("credit = %d (%d entries), want 12000 (2)", c, cn)
	}
}
