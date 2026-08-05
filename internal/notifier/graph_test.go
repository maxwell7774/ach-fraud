package notifier

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/fakeports"
	"github.com/27actions/ach/internal/graphmail"

	"github.com/google/uuid"
)

type fakeMailer struct {
	sent []graphmail.Message
}

func (f *fakeMailer) Send(_ context.Context, from string, m graphmail.Message) error {
	f.sent = append(f.sent, m)
	return nil
}

// seedHold creates a submission with one held entry and returns the hold id.
func seedHold(t *testing.T, st *fakeports.Store) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	sub, err := st.CreateSubmission(ctx, domain.Submission{Filename: "a.ach", Status: domain.SubmissionReady, ReceivedAt: now})
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	hdr, err := st.CreateBatchHeader(ctx, domain.BatchHeader{
		SubmissionID: sub.ID, CustomerID: "1001", EffectiveDate: &now,
	})
	if err != nil {
		t.Fatalf("hdr: %v", err)
	}
	entry, err := st.CreateBatchEntry(ctx, domain.BatchEntry{
		HeaderID: hdr.ID, Rdfi: "231380104", ReceiverName: "Alice",
		ReceiverAccount: "111000001", Amount: 150000, TranCode: 22,
	})
	if err != nil {
		t.Fatalf("entry: %v", err)
	}
	hold, err := st.CreateHold(ctx, entry.ID, domain.HoldPending, "amount: $1500.00 exceeds the single-entry threshold")
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	return sub.ID, hold.ID
}

func newGraph(st *fakeports.Store, m *fakeMailer) *Graph {
	return NewGraph(st, m, "shared@corp.com", []string{"ops@corp.com"}, "https://app.example")
}

func TestGraphNotifyHoldCreated(t *testing.T) {
	st := fakeports.NewStore()
	_, holdID := seedHold(t, st)
	m := &fakeMailer{}
	g := newGraph(st, m)

	if err := g.Notify(context.Background(), domain.Event{Type: evHoldCreated, Ref: &holdID}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(m.sent) != 1 {
		t.Fatalf("sent %d emails", len(m.sent))
	}
	msg := m.sent[0]
	if !strings.Contains(msg.Subject, "111000001") || !strings.Contains(msg.Subject, "$1,500.00") {
		t.Fatalf("subject = %q", msg.Subject)
	}
	for _, want := range []string{"Alice", "a.ach", "$1,500.00", "/holds/"} {
		if !strings.Contains(msg.Body, want) {
			t.Fatalf("body missing %q: %q", want, msg.Body)
		}
	}
}

func TestGraphNotifyReleaseBlocked(t *testing.T) {
	st := fakeports.NewStore()
	subID, _ := seedHold(t, st)
	ctx := context.Background()
	art, err := st.CreateArtifact(ctx, domain.Artifact{
		SubmissionID: subID, Kind: domain.ArtifactRelease, Checksum: "abc", State: domain.ArtifactStaged,
	})
	if err != nil {
		t.Fatalf("artifact: %v", err)
	}
	m := &fakeMailer{}
	g := newGraph(st, m)

	if err := g.Notify(ctx, domain.Event{Type: evReleaseBlocked, Ref: &art.ID}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(m.sent) != 1 || !strings.Contains(m.sent[0].Subject, "a.ach") {
		t.Fatalf("sent = %+v", m.sent)
	}
}

func TestGraphNotifyVelocityCrossed(t *testing.T) {
	st := fakeports.NewStore()
	subID, _ := seedHold(t, st)
	m := &fakeMailer{}
	g := newGraph(st, m)
	payload := []byte(`{"account":"111000001","rdfi":"231380104","effective_date":"2026-01-15","total":120000,"prior":60000,"held":60000}`)

	if err := g.Notify(context.Background(), domain.Event{Type: evVelocityCrossed, Ref: &subID, Payload: payload}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(m.sent) != 1 {
		t.Fatalf("sent %d emails", len(m.sent))
	}
	for _, want := range []string{"$1,200.00", "$600.00"} {
		if !strings.Contains(m.sent[0].Body, want) {
			t.Fatalf("body missing %q: %q", want, m.sent[0].Body)
		}
	}
}

func TestGraphNotifyIgnoresOtherEvents(t *testing.T) {
	st := fakeports.NewStore()
	m := &fakeMailer{}
	g := newGraph(st, m)
	id := uuid.New()
	if err := g.Notify(context.Background(), domain.Event{Type: "submission_created", Ref: &id}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(m.sent) != 0 {
		t.Fatalf("sent %d emails for an ignored event", len(m.sent))
	}
}

func TestGraphNotifyNoRecipients(t *testing.T) {
	st := fakeports.NewStore()
	_, holdID := seedHold(t, st)
	m := &fakeMailer{}
	g := NewGraph(st, m, "shared@corp.com", nil, "")
	if err := g.Notify(context.Background(), domain.Event{Type: evHoldCreated, Ref: &holdID}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(m.sent) != 0 {
		t.Fatalf("sent %d emails with no recipients", len(m.sent))
	}
}
