// Package importer loads legacy receiver history into the database so existing
// accounts carry hold history from day one, mirroring the CSV importer from the
// original ach-fraud example.
package importer

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/ports"
)

const (
	companyName        = "CSV Import"
	companyDescription = "CSV Import"
	tranCode           = 22
)

// Row is one receiver-history record from the CSV.
type Row struct {
	ReceiverName    string
	CustomerID      string
	Rdfi            string
	ReceiverAccount string
	EffectiveDate   time.Time
}

type headerKey struct {
	CustomerID    string
	EffectiveDate time.Time
}

// Result summarizes one import run.
type Result struct {
	Filename         string
	Headers          int
	Entries          int
	Holds            int
	SkippedExisting  int
	SkippedDuplicate int
}

// ParseCSV reads the semicolon-delimited CSV (6 fields, no header):
//
//	[0] unused, [1] receiver name, [2] customer id, [3] rdfi,
//	[4] receiver account, [5] effective date (YYYYMMDD; blank -> 2023-01-01).
func ParseCSV(path string) ([]Row, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer fd.Close()

	reader := csv.NewReader(fd)
	reader.Comma = ';'
	reader.FieldsPerRecord = 6
	reader.LazyQuotes = true

	var rows []Row
	line := 0
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line+1, err)
		}
		line++

		dateStr := strings.TrimSpace(record[5])
		var eff time.Time
		if dateStr == "" {
			eff = time.Date(2023, time.January, 1, 0, 0, 0, 0, time.UTC)
		} else {
			eff, err = time.Parse("20060102", dateStr)
			if err != nil {
				return nil, fmt.Errorf("line %d: invalid effective date %q: %w", line, record[5], err)
			}
		}

		rows = append(rows, Row{
			ReceiverName:    cleanUTF8(strings.TrimSpace(record[1])),
			CustomerID:      cleanUTF8(strings.TrimSpace(record[2])),
			Rdfi:            cleanUTF8(strings.TrimSpace(record[3])),
			ReceiverAccount: cleanUTF8(strings.TrimSpace(record[4])),
			EffectiveDate:   eff,
		})
	}
	return rows, nil
}

// Import records receiver history as one imported submission: a batch header
// per (customer, effective date) group, an entry per unique (rdfi, account),
// and an approved hold with an audit review for each entry. Rows whose
// (rdfi, account) already exists in the store are skipped, so a re-run is safe.
func Import(ctx context.Context, st ports.Store, path string, amount int64, actor string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("read file: %w", err)
	}
	rows, err := ParseCSV(path)
	if err != nil {
		return Result{}, err
	}
	if len(rows) == 0 {
		return Result{}, fmt.Errorf("no rows found in CSV")
	}
	if actor == "" {
		actor = "importer"
	}

	res := Result{Filename: filepath.Base(path)}

	// One submission for the whole file, created ready so the pipeline's
	// fix/import jobs never pick it up.
	sub, err := st.CreateSubmission(ctx, domain.Submission{
		Filename:       filepath.Base(path),
		SourceChecksum: fmt.Sprintf("%x", sha256.Sum256(data)),
		Status:         domain.SubmissionReady,
		ReceivedAt:     time.Now().UTC(),
	})
	if err != nil {
		return res, fmt.Errorf("create submission: %w", err)
	}

	groups := map[headerKey][]Row{}
	for _, r := range rows {
		k := headerKey{CustomerID: r.CustomerID, EffectiveDate: r.EffectiveDate}
		groups[k] = append(groups[k], r)
	}
	keys := make([]headerKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].EffectiveDate.Equal(keys[j].EffectiveDate) {
			return keys[i].CustomerID < keys[j].CustomerID
		}
		return keys[i].EffectiveDate.Before(keys[j].EffectiveDate)
	})

	seq := 0
	seen := map[string]bool{}

	if err := st.WithinTx(ctx, func(tx ports.Store) error {
		for _, k := range keys {
			bh, err := tx.CreateBatchHeader(ctx, domain.BatchHeader{
				SubmissionID:       sub.ID,
				CustomerID:         k.CustomerID,
				CompanyName:        companyName,
				CompanyDescription: companyDescription,
				EffectiveDate:      &k.EffectiveDate,
			})
			if err != nil {
				return fmt.Errorf("create batch header (customer=%s, date=%s): %w",
					k.CustomerID, k.EffectiveDate.Format("2006-01-02"), err)
			}
			res.Headers++

			for _, r := range groups[k] {
				key := r.Rdfi + "|" + r.ReceiverAccount
				if seen[key] {
					res.SkippedDuplicate++
					continue
				}
				exists, err := tx.HasEntryByRdfiAccount(ctx, r.Rdfi, r.ReceiverAccount)
				if err != nil {
					return err
				}
				if exists {
					res.SkippedExisting++
					seen[key] = true
					continue
				}

				seq++
				entry, err := tx.CreateBatchEntry(ctx, domain.BatchEntry{
					HeaderID:        bh.ID,
					Rdfi:            r.Rdfi,
					ReceiverName:    r.ReceiverName,
					ReceiverAccount: r.ReceiverAccount,
					Amount:          amount,
					TranCode:        tranCode,
					Trace:           fmt.Sprintf("%08d", seq),
				})
				if err != nil {
					return fmt.Errorf("create batch entry (%s): %w", key, err)
				}
				res.Entries++

				hold, err := tx.CreateHold(ctx, entry.ID, domain.HoldApproved, "imported receiver history")
				if err != nil {
					return fmt.Errorf("create hold for entry (%s): %w", key, err)
				}
				res.Holds++

				if err := tx.CreateReview(ctx, domain.Review{
					HoldID: hold.ID,
					Actor:  actor,
					Action: "approved",
					Note:   "imported legacy receiver history",
				}); err != nil {
					return err
				}

				seen[key] = true
			}
		}
		return nil
	}); err != nil {
		return res, err
	}

	return res, nil
}

// cleanUTF8 returns s unchanged if it is valid UTF-8, otherwise it re-decodes
// the bytes as latin-1 (ISO-8859-1) into UTF-8. This handles CSV files that
// were exported in a non-UTF-8 encoding.
func cleanUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	b := make([]rune, len(s))
	for i, c := range []byte(s) {
		b[i] = rune(c)
	}
	return string(b)
}
