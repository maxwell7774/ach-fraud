package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/ports"

	"github.com/google/uuid"
)

// ScreenResult summarizes one screening pass.
type ScreenResult struct {
	Pending      int
	AutoDeclined int
}

// ScreenSubmission applies the hold policy to the submission's imported
// entries and enqueues the process job. It is idempotent: entries that already
// carry a hold are skipped, so a retried screen never duplicates holds.
func ScreenSubmission(ctx context.Context, d Deps, submissionID uuid.UUID) (*ScreenResult, error) {
	now := d.Clock.Now()
	cutoff := now.AddDate(0, 0, -d.Policy.HoldDays)

	entries, err := d.Store.ListEntriesBySubmission(ctx, submissionID)
	if err != nil {
		return nil, err
	}
	existing, err := d.Store.ListHoldsBySubmission(ctx, submissionID)
	if err != nil {
		return nil, err
	}
	heldIDs := make(map[uuid.UUID]bool, len(existing))
	for _, h := range existing {
		heldIDs[h.EntryID] = true
	}

	// Whitelist/blacklist status for this submission's receiver pairs,
	// consulted against the full hold history (no expiry) in SQL. A combo is
	// whitelisted by a prior approved hold, blacklisted by a prior declined
	// hold; pending/auto_declined holds count for neither (undecided).
	combos, err := d.Store.ListCombosBySubmission(ctx, submissionID, cutoff)
	if err != nil {
		return nil, err
	}
	approved := make(map[string]bool, len(combos))
	declined := make(map[string]bool, len(combos))
	for _, c := range combos {
		key := c.Rdfi + "|" + c.ReceiverAccount
		switch domain.HoldStatus(c.LatestStatus) {
		case domain.HoldApproved:
			approved[key] = true
		case domain.HoldDeclined, domain.HoldAutoDeclined:
			declined[key] = true
		}
	}

	// Same-day velocity per receiver account/RDFI/date/customer, summed across
	// ALL ready submissions (not just this one) but only for the groups this
	// file touches, so funds split across multiple files on the same day cannot
	// duck the velocity rule.
	velocity := map[string]int64{}
	sums, err := d.Store.SumVelocity(ctx, cutoff, submissionID)
	if err != nil {
		return nil, err
	}
	for _, vs := range sums {
		velocity[velocityKey(vs.ReceiverAccount, vs.Rdfi, vs.CustomerID, vs.EffectiveDate)] += vs.Total
	}

	// Same-day held totals from OTHER submissions for those same groups, so the
	// alert's "prior" (already shipped) only counts amounts that were never
	// held — an earlier single-entry or velocity hold never shipped, so it must
	// not be reported as a leak.
	heldByGroup := map[string]int64{}
	heldSums, err := d.Store.SumHeldByGroup(ctx, cutoff, submissionID)
	if err != nil {
		return nil, err
	}
	for _, vs := range heldSums {
		heldByGroup[velocityKey(vs.ReceiverAccount, vs.Rdfi, vs.CustomerID, vs.EffectiveDate)] += vs.Total
	}

	// This submission's own contribution to each velocity group, used to split
	// a crossed group's total into what is held now vs. what already shipped.
	own := map[string]int64{}
	for _, e := range entries {
		if !creditEligible(e, cutoff) {
			continue
		}
		own[velocityKey(e.ReceiverAccount, e.Rdfi, e.CustomerID, e.EffectiveDate)] += e.Amount
	}

	res := &ScreenResult{}

	// Create all holds in one transaction so a failure mid-screen cannot leave
	// a partially-held submission; events are emitted only after it commits.
	type createdHold struct {
		id     uuid.UUID
		status domain.HoldStatus
		alert  *velocityAlert
	}
	var created []createdHold
	if err := d.Store.WithinTx(ctx, func(tx ports.Store) error {
		alerted := map[string]bool{}
		for _, e := range entries {
			if heldIDs[e.ID] {
				continue
			}
			if !creditEligible(e, cutoff) {
				continue
			}
			// Entries destined for our own routing number are inbound and
			// trusted; skip them entirely.
			if d.Policy.HoldingRDFI != "" && e.Rdfi == d.Policy.HoldingRDFI {
				continue
			}

			k := e.Rdfi + "|" + e.ReceiverAccount
			gkey := velocityKey(e.ReceiverAccount, e.Rdfi, e.CustomerID, e.EffectiveDate)
			groupVelocity := velocity[gkey]

			// Decision order:
			//   1. blacklisted combo (prior declined) -> auto-declined, any sum.
			//   2. whitelisted combo (prior approved) -> trusted, money sends.
			//   3. undecided combo whose same-day group sum crosses the
			//      threshold -> held for manual review. This catches a single
			//      $1,000+ entry (it is its own group sum) and a multi-entry
			//      structuring attack; only undecided combos are held.
			switch {
			case declined[k]:
				h, err := tx.CreateHold(ctx, e.ID, domain.HoldAutoDeclined, "account previously declined")
				if err != nil {
					return err
				}
				res.AutoDeclined++
				created = append(created, createdHold{id: h.ID, status: domain.HoldAutoDeclined})
			case approved[k]:
				// Whitelisted: the account was vetted; let the funds through.
				continue
			case groupVelocity >= d.Policy.HoldVelocityAmount:
				// The reason reflects what actually held THIS entry: an amount
				// that alone crosses the threshold is labeled as such, even
				// though it also trips the same-day sum. Only entries below
				// the single threshold get the velocity label.
				reason := fmt.Sprintf("velocity: same-day total %s crossed the threshold", cents(groupVelocity))
				if e.Amount >= d.Policy.HoldSingleAmount {
					reason = fmt.Sprintf("amount: %s exceeds the single-entry threshold", cents(e.Amount))
				}
				h, err := tx.CreateHold(ctx, e.ID, domain.HoldPending, reason)
				if err != nil {
					return err
				}
				res.Pending++
				created = append(created, createdHold{id: h.ID, status: domain.HoldPending})
				if !alerted[gkey] {
					alerted[gkey] = true
					prior := groupVelocity - own[gkey] - heldByGroup[gkey]
					if prior < 0 {
						prior = 0
					}
					created = append(created, createdHold{alert: velocityAlertFor(e, groupVelocity, own[gkey], prior)})
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// Emit events only after the holds are committed, so a rolled-back screen
	// leaves no events behind.
	for _, c := range created {
		switch {
		case c.alert != nil:
			payload, err := json.Marshal(c.alert)
			if err != nil {
				return nil, err
			}
			if err := Emit(ctx, d, EvVelocityCrossed, &submissionID, payload); err != nil {
				return nil, err
			}
		case c.status == domain.HoldAutoDeclined:
			if err := Emit(ctx, d, EvHoldAutoDeclined, &c.id, nil); err != nil {
				return nil, err
			}
		default:
			if err := Emit(ctx, d, EvHoldCreated, &c.id, nil); err != nil {
				return nil, err
			}
		}
	}

	if err := d.Store.EnqueueJob(ctx, domain.JobProcess, submissionID, d.Clock.Now()); err != nil {
		return nil, err
	}
	return res, nil
}

// creditEligible reports whether an entry can be screened: a credit (tran
// 22/32) inside the effective-date lookback.
func creditEligible(e domain.BatchEntry, cutoff time.Time) bool {
	if e.TranCode != 22 && e.TranCode != 32 {
		return false
	}
	return e.EffectiveDate != nil && !e.EffectiveDate.Before(cutoff)
}

// velocityAlert describes a crossed same-day velocity group so operators can
// see how much already shipped before the hold tripped.
type velocityAlert struct {
	Account       string `json:"account"`
	Rdfi          string `json:"rdfi"`
	EffectiveDate string `json:"effective_date"`
	CustomerID    string `json:"customer_id"`
	Total         int64  `json:"total"`
	Prior         int64  `json:"prior"`
	Held          int64  `json:"held"`
}

// velocityAlertFor describes a crossed same-day velocity group so operators can
// see how much already shipped before the hold tripped. Prior is the portion of
// the group that actually shipped (was never held, so earlier holds are not
// double-counted as leaks); Held is this submission's portion (held now).
func velocityAlertFor(e domain.BatchEntry, total, held, prior int64) *velocityAlert {
	return &velocityAlert{
		Account:       e.ReceiverAccount,
		Rdfi:          e.Rdfi,
		EffectiveDate: e.EffectiveDate.Format("2006-01-02"),
		CustomerID:    e.CustomerID,
		Total:         total,
		Prior:         prior,
		Held:          held,
	}
}

func velocityKey(account, rdfi, customer string, eff *time.Time) string {
	day := ""
	if eff != nil {
		day = eff.Format("2006-01-02")
	}
	return fmt.Sprintf("%s|%s|%s|%s", account, rdfi, day, customer)
}

// cents renders an integer cent amount as a US dollar string.
func cents(c int64) string {
	return "$" + strconv.FormatFloat(float64(c)/100, 'f', 2, 64)
}
