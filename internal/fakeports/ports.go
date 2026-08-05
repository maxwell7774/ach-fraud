package fakeports

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/ports"
)

// Files is an in-memory content-addressed store.
type Files struct {
	mu   sync.Mutex
	data map[string][]byte
}

func NewFiles() *Files {
	return &Files{data: map[string][]byte{}}
}

func (f *Files) Get(_ context.Context, checksum string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.data[checksum]
	if !ok {
		return nil, fmt.Errorf("artifact %s not found", checksum)
	}
	return d, nil
}

func (f *Files) Put(_ context.Context, checksum string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[checksum] = data
	return nil
}

func (f *Files) Delete(_ context.Context, checksum string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.data, checksum)
	return nil
}

// Bytes returns the stored bytes for a checksum (nil when absent).
func (f *Files) Bytes(checksum string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.data[checksum]
}

var _ ports.Files = (*Files)(nil)

// Sender records transmitted artifacts in memory.
type Sender struct {
	mu   sync.Mutex
	sent []domain.Artifact
}

func NewSender() *Sender {
	return &Sender{}
}

func (s *Sender) Send(_ context.Context, a domain.Artifact, _ []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, a)
	return nil
}

// Sent returns the artifacts transmitted so far.
func (s *Sender) Sent() []domain.Artifact {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Artifact, len(s.sent))
	copy(out, s.sent)
	return out
}

func (s *Sender) SentKinds() map[domain.ArtifactKind][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[domain.ArtifactKind][]string{}
	for _, a := range s.sent {
		out[a.Kind] = append(out[a.Kind], a.Checksum)
	}
	return out
}

var _ ports.Sender = (*Sender)(nil)

// Clock is a settable ports.Clock.
type Clock struct {
	t time.Time
}

func NewClock(t time.Time) *Clock {
	return &Clock{t: t}
}

func (c *Clock) Now() time.Time { return c.t }

// Set advances the clock.
func (c *Clock) Set(t time.Time) { c.t = t }

var _ ports.Clock = (*Clock)(nil)
