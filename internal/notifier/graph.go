// Package notifier implements ports.Notifier. The no-op adapter is the default;
// Graph-backed alerts send a single run digest email from a shared mailbox when
// configured. The digest aggregates the run's alert-worthy events — pending
// holds, velocity leaks, blocked releases, and failures — instead of emailing
// per event, and goes to the enabled recipients subscribed to a category that
// fired.
package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/graphmail"
	"github.com/27actions/ach/internal/ports"

	"github.com/google/uuid"
)

// Event types that feed the digest. Kept as literals so this adapter stays a
// leaf (no dependency back on the pipeline package).
const (
	evHoldCreated      = "hold_created"
	evVelocityCrossed  = "velocity_crossed"
	evReleaseBlocked   = "release_blocked"
	evSubmissionFailed = "submission_failed"
	evJobFailed        = "job_failed"
)

// mailer is the Graph send boundary, satisfied by *graphmail.Client.
type mailer interface {
	Send(ctx context.Context, from string, msg graphmail.Message) error
}

// Graph sends a run digest via Graph application permissions. Sends are
// best-effort: a mail outage must never fail pipeline processing, so errors are
// logged and dropped. Recipients come from the store, so the web UI controls
// who is alerted on what.
type Graph struct {
	Store   ports.Store
	Mail    mailer
	From    string
	BaseURL string
	Timeout time.Duration

	mu  sync.Mutex
	buf []domain.Event
}

// NewGraph builds a Graph notifier. From is the shared mailbox; baseURL, when
// set, is prepended to review links.
func NewGraph(store ports.Store, m mailer, from string, baseURL string) *Graph {
	return &Graph{
		Store:   store,
		Mail:    m,
		From:    from,
		BaseURL: baseURL,
		Timeout: 15 * time.Second,
	}
}

// Notify buffers events that feed the run digest; nothing is emailed until
// Flush.
func (g *Graph) Notify(_ context.Context, e domain.Event) error {
	if g == nil || !isDigestEvent(e.Type) {
		return nil
	}
	g.mu.Lock()
	g.buf = append(g.buf, e)
	g.mu.Unlock()
	return nil
}

// Flush sends one digest email for everything buffered since the last flush,
// but only to enabled recipients subscribed to a category that fired, and only
// when something fired — a run that does nothing sends nothing.
func (g *Graph) Flush(ctx context.Context) error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	events := g.buf
	g.buf = nil
	g.mu.Unlock()
	if len(events) == 0 {
		return nil
	}

	dig := g.buildDigest(ctx, events)
	if len(dig.fired) == 0 {
		return nil
	}

	recs, err := g.Store.ListRecipients(ctx)
	if err != nil {
		log.Printf("notifier: listing recipients: %v", err)
		return nil
	}
	var to []string
	for _, r := range recs {
		if !r.Enabled {
			continue
		}
		for _, t := range r.AlertTypes {
			if dig.fired[t] {
				to = append(to, r.Email)
				break
			}
		}
	}
	if len(to) == 0 {
		return nil
	}

	subject, body := dig.render(g.BaseURL)
	sendCtx, cancel := context.WithTimeout(ctx, g.Timeout)
	defer cancel()
	if err := g.Mail.Send(sendCtx, g.From, graphmail.Message{Subject: subject, To: to, Body: body}); err != nil {
		// Best-effort: never fail the pipeline because mail is down.
		log.Printf("notifier: send failed for run digest: %v", err)
	}
	return nil
}

func isDigestEvent(typ string) bool {
	switch typ {
	case evHoldCreated, evVelocityCrossed, evReleaseBlocked, evSubmissionFailed, evJobFailed:
		return true
	}
	return false
}

// velocityAlert mirrors the pipeline's velocity_crossed payload.
type velocityAlert struct {
	Account       string `json:"account"`
	Rdfi          string `json:"rdfi"`
	EffectiveDate string `json:"effective_date"`
	CustomerID    string `json:"customer_id"`
	Total         int64  `json:"total"`
	Prior         int64  `json:"prior"`
	Held          int64  `json:"held"`
	File          string `json:"-"`
}

type jobFailure struct {
	Kind  string `json:"kind"`
	Error string `json:"error"`
}

// digest accumulates one run's alert-worthy events into sections.
type digest struct {
	holds    []domain.Hold
	leaks    []velocityAlert
	blocked  []string
	failures []string
	fired    map[string]bool
}

func (d *digest) mark(typ string) {
	if d.fired == nil {
		d.fired = map[string]bool{}
	}
	d.fired[typ] = true
}

func (g *Graph) buildDigest(ctx context.Context, events []domain.Event) *digest {
	dig := &digest{}
	for _, e := range events {
		switch e.Type {
		case evHoldCreated:
			if e.Ref == nil {
				continue
			}
			h, err := g.Store.GetHold(ctx, *e.Ref)
			if err != nil {
				continue
			}
			// Only holds still pending review belong in the digest; a hold
			// decided within the same run is not a call to action.
			if h.Status != domain.HoldPending {
				continue
			}
			dig.holds = append(dig.holds, h)
			dig.mark(domain.AlertPendingHolds)
		case evVelocityCrossed:
			var a velocityAlert
			if err := json.Unmarshal(e.Payload, &a); err != nil {
				continue
			}
			if e.Ref != nil {
				a.File = g.filenameFor(ctx, *e.Ref)
			}
			dig.leaks = append(dig.leaks, a)
			dig.mark(domain.AlertVelocityLeaks)
		case evReleaseBlocked:
			if e.Ref == nil {
				continue
			}
			dig.blocked = append(dig.blocked, g.filenameFor(ctx, *e.Ref))
			dig.mark(domain.AlertReleaseBlocked)
		case evSubmissionFailed:
			if e.Ref == nil {
				continue
			}
			dig.failures = append(dig.failures, g.filenameFor(ctx, *e.Ref))
			dig.mark(domain.AlertFailed)
		case evJobFailed:
			var f jobFailure
			_ = json.Unmarshal(e.Payload, &f)
			file := ""
			if e.Ref != nil {
				file = g.filenameFor(ctx, *e.Ref)
			}
			dig.failures = append(dig.failures, fmt.Sprintf("%s — %s", file, f.Error))
			dig.mark(domain.AlertFailed)
		}
	}
	return dig
}

// filenameFor resolves a ref that may be a submission id or a release artifact
// id to its submission's filename.
func (g *Graph) filenameFor(ctx context.Context, ref uuid.UUID) string {
	if sub, err := g.Store.GetSubmission(ctx, ref); err == nil {
		return sub.Filename
	}
	if a, err := g.Store.GetArtifactByID(ctx, ref); err == nil {
		if sub, err := g.Store.GetSubmission(ctx, a.SubmissionID); err == nil {
			return sub.Filename
		}
	}
	return ""
}

func (d *digest) render(baseURL string) (subject, body string) {
	if len(d.holds) > 0 {
		subject = fmt.Sprintf("ACH review: %d new hold(s) awaiting review", len(d.holds))
	} else {
		subject = "ACH pipeline alert"
	}
	var b strings.Builder
	if len(d.holds) > 0 {
		fmt.Fprintf(&b, "%d new hold(s) are pending review.\n\n", len(d.holds))
		fmt.Fprintf(&b, "PENDING HOLDS\n")
		for _, h := range d.holds {
			fmt.Fprintf(&b, "  • %s to account %s (%s) — file %s\n", dollars(h.EntryAmount), h.EntryReceiverAcct, h.EntryReceiverName, h.Filename)
			if h.Reason != "" {
				fmt.Fprintf(&b, "    Reason: %s\n", h.Reason)
			}
			if baseURL != "" {
				fmt.Fprintf(&b, "    Review: %s/holds/%s\n", strings.TrimSuffix(baseURL, "/"), h.ID)
			}
		}
	}
	if len(d.leaks) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "VELOCITY LEAKS\n")
		for _, a := range d.leaks {
			file := ""
			if a.File != "" {
				file = " (file " + a.File + ")"
			}
			fmt.Fprintf(&b, "  • Account %s — same-day %s, %s already shipped, %s held%s\n",
				a.Account, dollars(a.Total), dollars(a.Prior), dollars(a.Held), file)
		}
	}
	if len(d.blocked) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "BLOCKED RELEASES\n")
		for _, f := range d.blocked {
			fmt.Fprintf(&b, "  • %s — release blocked because a hold was declined\n", f)
		}
	}
	if len(d.failures) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "FAILURES\n")
		for _, f := range d.failures {
			fmt.Fprintf(&b, "  • %s\n", f)
		}
	}
	return subject, b.String()
}

func dollars(cents int64) string {
	neg := ""
	if cents < 0 {
		neg = "-"
		cents = -cents
	}
	intPart := cents / 100
	fracPart := cents % 100
	digits := strconv.FormatInt(intPart, 10)
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return fmt.Sprintf("%s$%s.%02d", neg, b.String(), fracPart)
}
