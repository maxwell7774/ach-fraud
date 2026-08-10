// Package notifier implements ports.Notifier. The no-op adapter is the default;
// Graph-backed alerts send email from a shared mailbox when configured.
package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/graphmail"
	"github.com/27actions/ach/internal/ports"

	"github.com/google/uuid"
)

// Event types we email about. Kept as literals so this adapter stays a leaf
// (no dependency back on the pipeline package).
const (
	evHoldCreated     = "hold_created"
	evReleaseBlocked  = "release_blocked"
	evVelocityCrossed = "velocity_crossed"
)

// mailer is the Graph send boundary, satisfied by *graphmail.Client.
type mailer interface {
	Send(ctx context.Context, from string, msg graphmail.Message) error
}

// Graph emails operational alerts from a shared mailbox via Graph application
// permissions. Sends are best-effort: a mail outage must never fail pipeline
// processing, so errors are logged and dropped.
type Graph struct {
	Store   ports.Store
	Mail    mailer
	From    string
	To      []string
	BaseURL string
	Timeout time.Duration
}

// NewGraph builds a Graph notifier. From is the shared mailbox; To is the
// recipient list; baseURL, when set, is prepended to review links.
func NewGraph(store ports.Store, m mailer, from string, to []string, baseURL string) *Graph {
	return &Graph{
		Store:   store,
		Mail:    m,
		From:    from,
		To:      to,
		BaseURL: baseURL,
		Timeout: 15 * time.Second,
	}
}

func (g *Graph) Notify(ctx context.Context, e domain.Event) error {
	if g == nil || len(g.To) == 0 {
		return nil
	}
	subject, body, ok := g.render(e)
	if !ok {
		return nil
	}
	sendCtx, cancel := context.WithTimeout(ctx, g.Timeout)
	defer cancel()
	if err := g.Mail.Send(sendCtx, g.From, graphmail.Message{Subject: subject, To: g.To, Body: body}); err != nil {
		// Best-effort: never fail the pipeline because mail is down.
		log.Printf("notifier: send failed for %s: %v", e.Type, err)
	}
	return nil
}

// render builds the email for an event; ok=false means the event is not one we
// email about.
func (g *Graph) render(e domain.Event) (subject, body string, ok bool) {
	if e.Ref == nil {
		return "", "", false
	}
	switch e.Type {
	case evHoldCreated:
		h, err := g.Store.GetHold(context.Background(), *e.Ref)
		if err != nil {
			return "", "", false
		}
		return g.holdCreated(h)
	case evReleaseBlocked:
		return g.releaseBlocked(context.Background(), *e.Ref)
	case evVelocityCrossed:
		return g.velocityCrossed(*e.Ref, e.Payload)
	}
	return "", "", false
}

func (g *Graph) holdCreated(h domain.Hold) (string, string, bool) {
	subject := fmt.Sprintf("New hold for review: account %s — %s", h.EntryReceiverAcct, dollars(h.EntryAmount))
	var b strings.Builder
	fmt.Fprintf(&b, "A new hold was created for review.\n\n")
	fmt.Fprintf(&b, "  Receiver: %s\n", h.EntryReceiverName)
	fmt.Fprintf(&b, "  Account:  %s\n", h.EntryReceiverAcct)
	fmt.Fprintf(&b, "  Amount:   %s\n", dollars(h.EntryAmount))
	fmt.Fprintf(&b, "  File:     %s\n", h.Filename)
	if h.Reason != "" {
		fmt.Fprintf(&b, "  Reason:   %s\n", h.Reason)
	}
	if g.BaseURL != "" {
		fmt.Fprintf(&b, "\nReview: %s/holds/%s\n", strings.TrimSuffix(g.BaseURL, "/"), h.ID)
	}
	return subject, b.String(), true
}

func (g *Graph) releaseBlocked(ctx context.Context, artifactID uuid.UUID) (string, string, bool) {
	a, err := g.Store.GetArtifactByID(ctx, artifactID)
	if err != nil {
		return "", "", false
	}
	sub, err := g.Store.GetSubmission(ctx, a.SubmissionID)
	if err != nil {
		return "", "", false
	}
	subject := fmt.Sprintf("Release blocked: %s", sub.Filename)
	var b strings.Builder
	fmt.Fprintf(&b, "The release for %s was blocked because a hold was declined.\n\n"+
		"The intercept stays at the holding account pending manual handling.\n", sub.Filename)
	if g.BaseURL != "" {
		fmt.Fprintf(&b, "\nFile: %s/submissions/%s\n", strings.TrimSuffix(g.BaseURL, "/"), sub.ID)
	}
	return subject, b.String(), true
}

// velocityAlert mirrors the pipeline's velocity_crossed payload.
type velocityAlert struct {
	Account       string `json:"account"`
	Rdfi          string `json:"rdfi"`
	EffectiveDate string `json:"effective_date"`
	Total         int64  `json:"total"`
	Prior         int64  `json:"prior"`
	Held          int64  `json:"held"`
}

func (g *Graph) velocityCrossed(submissionID uuid.UUID, payload json.RawMessage) (string, string, bool) {
	var a velocityAlert
	if err := json.Unmarshal(payload, &a); err != nil {
		return "", "", false
	}
	sub, err := g.Store.GetSubmission(context.Background(), submissionID)
	if err != nil {
		return "", "", false
	}
	subject := fmt.Sprintf("Velocity alert: account %s — same-day %s", a.Account, dollars(a.Total))
	var b strings.Builder
	fmt.Fprintf(&b, "Same-day velocity crossed the threshold for account %s.\n\n", a.Account)
	fmt.Fprintf(&b, "  Same-day total: %s\n", dollars(a.Total))
	fmt.Fprintf(&b, "  Already shipped: %s\n", dollars(a.Prior))
	fmt.Fprintf(&b, "  Held for review: %s\n", dollars(a.Held))
	fmt.Fprintf(&b, "  File: %s\n", sub.Filename)
	return subject, b.String(), true
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
