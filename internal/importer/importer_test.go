package importer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/fakeports"
)

func writeCSV(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "history.csv")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write csv: %v", err)
	}
	return path
}

func TestImportCreatesHistory(t *testing.T) {
	st := fakeports.NewStore()
	path := writeCSV(t, `;Alice A;1001;231380104;111000001;20260115
;Bob B;1002;231380104;111000002;20260116
;Carol C;1001;231380104;111000003;20260115
`)
	res, err := Import(context.Background(), st, path, 100000, "data-migration")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Headers != 2 {
		t.Fatalf("expected 2 batch headers (grouped by customer/date), got %d", res.Headers)
	}
	if res.Entries != 3 || res.Holds != 3 {
		t.Fatalf("expected 3 entries and 3 holds, got entries=%d holds=%d", res.Entries, res.Holds)
	}
	if res.SkippedExisting != 0 || res.SkippedDuplicate != 0 {
		t.Fatalf("unexpected skips: existing=%d dup=%d", res.SkippedExisting, res.SkippedDuplicate)
	}

	subs := st.Submissions()
	if len(subs) != 1 {
		t.Fatalf("expected 1 submission, got %d", len(subs))
	}
	if subs[0].Status != domain.SubmissionReady {
		t.Fatalf("submission status = %s, want ready", subs[0].Status)
	}
	if subs[0].Filename != "history.csv" {
		t.Fatalf("submission filename = %s", subs[0].Filename)
	}

	holds := st.HoldsFor(subs[0].ID)
	if len(holds) != 3 {
		t.Fatalf("expected 3 holds, got %d", len(holds))
	}
	for _, h := range holds {
		if h.Status != domain.HoldApproved {
			t.Fatalf("hold status = %s, want approved", h.Status)
		}
		if h.Reason != "imported receiver history" {
			t.Fatalf("hold reason = %q", h.Reason)
		}
	}
	reviews := st.Reviews()
	if len(reviews) != 3 {
		t.Fatalf("expected 3 reviews, got %d", len(reviews))
	}
	for _, r := range reviews {
		if r.Actor != "data-migration" || r.Action != "approved" {
			t.Fatalf("review = %+v", r)
		}
	}
}

func TestImportDedupsWithinFile(t *testing.T) {
	st := fakeports.NewStore()
	path := writeCSV(t, `;Alice A;1001;231380104;111000001;20260115
;Alice Again;1001;231380104;111000001;20260115
`)
	res, err := Import(context.Background(), st, path, 100000, "")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Entries != 1 || res.Holds != 1 || res.SkippedDuplicate != 1 {
		t.Fatalf("expected 1 entry/1 hold + 1 dup skip, got %+v", res)
	}
}

func TestImportRerunSkipsExisting(t *testing.T) {
	st := fakeports.NewStore()
	path := writeCSV(t, `;Alice A;1001;231380104;111000001;20260115
`)
	if _, err := Import(context.Background(), st, path, 100000, ""); err != nil {
		t.Fatalf("first import: %v", err)
	}
	res, err := Import(context.Background(), st, path, 100000, "")
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if res.Entries != 0 || res.Holds != 0 || res.SkippedExisting != 1 {
		t.Fatalf("expected second run to skip the existing account, got %+v", res)
	}
}

func TestParseCSVBlankDateDefaults(t *testing.T) {
	path := writeCSV(t, `;Alice A;1001;231380104;111000001;
`)
	rows, err := ParseCSV(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	want := time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC)
	if !rows[0].EffectiveDate.Equal(want) {
		t.Fatalf("effective date = %s, want %s", rows[0].EffectiveDate, want)
	}
}
