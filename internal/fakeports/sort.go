package fakeports

import (
	"sort"
	"time"

	"github.com/27actions/ach/internal/domain"
)

func applyPage[T any](rows []T, offset, limit int) []T {
	if offset >= len(rows) {
		return nil
	}
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	return rows[offset:end]
}

func timeOrEmpty(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

func sortHoldsFor(rows []domain.Hold, col, dir string) {
	asc := dir != "desc"
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		var less bool
		switch col {
		case "amount":
			less = a.EntryAmount < b.EntryAmount
		case "filename":
			less = a.Filename < b.Filename
		case "effective":
			less = timeOrEmpty(a.EffectiveDate) < timeOrEmpty(b.EffectiveDate)
		case "receiver":
			less = a.EntryReceiverName < b.EntryReceiverName
		case "account":
			less = a.EntryReceiverAcct < b.EntryReceiverAcct
		case "rdfi":
			less = a.EntryRdfi < b.EntryRdfi
		case "status":
			less = a.Status < b.Status
		default:
			less = a.CreatedAt.After(b.CreatedAt)
		}
		if asc {
			return less
		}
		return !less
	})
}

func sortEntriesFor(rows []domain.BatchEntry, col, dir string) {
	asc := dir != "desc"
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		var less bool
		switch col {
		case "amount":
			less = a.Amount < b.Amount
		case "tran_code":
			less = a.TranCode < b.TranCode
		case "trace":
			less = a.Trace < b.Trace
		case "rdfi":
			less = a.Rdfi < b.Rdfi
		case "receiver":
			less = a.ReceiverName < b.ReceiverName
		case "account":
			less = a.ReceiverAccount < b.ReceiverAccount
		case "effective":
			less = timeOrEmpty(a.EffectiveDate) < timeOrEmpty(b.EffectiveDate)
		case "filename":
			less = a.Filename < b.Filename
		default:
			less = a.Trace < b.Trace
		}
		if asc {
			return less
		}
		return !less
	})
}

func sortHeadersFor(rows []domain.BatchHeader, col, dir string) {
	asc := dir != "desc"
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		var less bool
		switch col {
		case "company_name":
			less = a.CompanyName < b.CompanyName
		case "customer_id":
			less = a.CustomerID < b.CustomerID
		case "effective":
			less = timeOrEmpty(a.EffectiveDate) < timeOrEmpty(b.EffectiveDate)
		case "filename":
			less = a.Filename < b.Filename
		default:
			less = a.CustomerID < b.CustomerID
		}
		if asc {
			return less
		}
		return !less
	})
}

func sortSubmissionsFor(rows []domain.Submission, col, dir string) {
	asc := dir != "desc"
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		var less bool
		switch col {
		case "filename":
			less = a.Filename < b.Filename
		case "received":
			less = a.ReceivedAt.Before(b.ReceivedAt)
		case "status":
			less = a.Status < b.Status
		default:
			less = a.ReceivedAt.After(b.ReceivedAt)
		}
		if asc {
			return less
		}
		return !less
	})
}

func sortEventsFor(rows []domain.Event, col, dir string) {
	asc := dir != "desc"
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		var less bool
		switch col {
		case "type":
			less = a.Type < b.Type
		case "when":
			less = a.CreatedAt.Before(b.CreatedAt)
		default:
			less = a.CreatedAt.After(b.CreatedAt)
		}
		if asc {
			return less
		}
		return !less
	})
}
