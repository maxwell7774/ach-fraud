// Package fakeports provides in-memory implementations of the ports interfaces
// for tests. They are intentionally faithful to the production semantics the
// pipeline relies on (uniqueness, joins, upsert jobs), so pipeline tests behave
// like they would against Postgres.
package fakeports

import (
	"context"
	"maps"
	"sync"
	"time"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/ports"

	"github.com/google/uuid"
)

// Store is an in-memory ports.Store.
type Store struct {
	mu sync.Mutex

	subs    map[uuid.UUID]domain.Submission
	arts    map[uuid.UUID]domain.Artifact
	headers map[uuid.UUID]domain.BatchHeader
	entries map[uuid.UUID]domain.BatchEntry
	holds   map[uuid.UUID]domain.Hold
	reviews []domain.Review
	users   map[uuid.UUID]domain.User
	sess    map[uuid.UUID]domain.Session
	verifs  map[uuid.UUID]domain.Verification
	recips  map[uuid.UUID]domain.Recipient
	jobs    map[string]*domain.Job
	order   []uuid.UUID
	updated map[uuid.UUID]time.Time
	artUpd  map[uuid.UUID]time.Time
	holdUpd map[uuid.UUID]time.Time
	events  []domain.Event

	// Now supplies the time for ClaimDueJob, RequeueStaleJobs, and artifact
	// timestamps. Defaults to time.Now; tests can pin it to the fake clock.
	Now func() time.Time

	// HoldError, when set, is invoked by CreateHold; its error aborts (and
	// rolls back) the enclosing transaction.
	HoldError func(entryID uuid.UUID) error
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{
		subs:    map[uuid.UUID]domain.Submission{},
		arts:    map[uuid.UUID]domain.Artifact{},
		headers: map[uuid.UUID]domain.BatchHeader{},
		entries: map[uuid.UUID]domain.BatchEntry{},
		holds:   map[uuid.UUID]domain.Hold{},
		users:   map[uuid.UUID]domain.User{},
		sess:    map[uuid.UUID]domain.Session{},
		verifs:  map[uuid.UUID]domain.Verification{},
		recips:  map[uuid.UUID]domain.Recipient{},
		jobs:    map[string]*domain.Job{},
		updated: map[uuid.UUID]time.Time{},
		artUpd:  map[uuid.UUID]time.Time{},
		holdUpd: map[uuid.UUID]time.Time{},
		Now:     time.Now,
	}
}

func jobKey(kind domain.JobKind, ref uuid.UUID) string {
	return string(kind) + "|" + ref.String()
}

// Submissions returns all submissions.
func (s *Store) Submissions() []domain.Submission {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Submission, 0, len(s.subs))
	for _, v := range s.subs {
		out = append(out, v)
	}
	return out
}

// ArtifactsFor returns the artifacts for a submission.
func (s *Store) ArtifactsFor(submissionID uuid.UUID) []domain.Artifact {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Artifact
	for _, a := range s.arts {
		if a.SubmissionID == submissionID {
			out = append(out, a)
		}
	}
	return out
}

// HoldsFor returns the holds for a submission.
func (s *Store) HoldsFor(submissionID uuid.UUID) []domain.Hold {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Hold
	for _, h := range s.holds {
		p := s.populateHold(h)
		if p.SubmissionID == submissionID {
			out = append(out, p)
		}
	}
	return out
}

// Reviews returns all recorded reviews.
func (s *Store) Reviews() []domain.Review {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Review, len(s.reviews))
	copy(out, s.reviews)
	return out
}

// Events returns all recorded events.
func (s *Store) Events() []domain.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Event, len(s.events))
	copy(out, s.events)
	return out
}

// Users returns all users.
func (s *Store) Users() []domain.User {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}
	return out
}

// Job returns the job for a kind/ref pair, or nil.
func (s *Store) Job(kind domain.JobKind, ref uuid.UUID) *domain.Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[jobKey(kind, ref)]
	if !ok {
		return nil
	}
	cp := *j
	return &cp
}

// storeState is a snapshot of every collection, taken so a transaction can be
// rolled back by restoring it.
type storeState struct {
	subs    map[uuid.UUID]domain.Submission
	arts    map[uuid.UUID]domain.Artifact
	headers map[uuid.UUID]domain.BatchHeader
	entries map[uuid.UUID]domain.BatchEntry
	holds   map[uuid.UUID]domain.Hold
	reviews []domain.Review
	users   map[uuid.UUID]domain.User
	sess    map[uuid.UUID]domain.Session
	verifs  map[uuid.UUID]domain.Verification
	recips  map[uuid.UUID]domain.Recipient
	jobs    map[string]*domain.Job
	order   []uuid.UUID
	updated map[uuid.UUID]time.Time
	artUpd  map[uuid.UUID]time.Time
	holdUpd map[uuid.UUID]time.Time
	events  []domain.Event
}

func (s *Store) snapshot() storeState {
	cloneJobs := func(m map[string]*domain.Job) map[string]*domain.Job {
		out := make(map[string]*domain.Job, len(m))
		for k, v := range m {
			cp := *v
			out[k] = &cp
		}
		return out
	}
	return storeState{
		subs:    maps.Clone(s.subs),
		arts:    maps.Clone(s.arts),
		headers: maps.Clone(s.headers),
		entries: maps.Clone(s.entries),
		holds:   maps.Clone(s.holds),
		reviews: append([]domain.Review(nil), s.reviews...),
		users:   maps.Clone(s.users),
		sess:    maps.Clone(s.sess),
		verifs:  maps.Clone(s.verifs),
		recips:  maps.Clone(s.recips),
		jobs:    cloneJobs(s.jobs),
		order:   append([]uuid.UUID(nil), s.order...),
		updated: maps.Clone(s.updated),
		artUpd:  maps.Clone(s.artUpd),
		holdUpd: maps.Clone(s.holdUpd),
		events:  append([]domain.Event(nil), s.events...),
	}
}

func (s *Store) restore(snap storeState) {
	s.subs = snap.subs
	s.arts = snap.arts
	s.headers = snap.headers
	s.entries = snap.entries
	s.holds = snap.holds
	s.reviews = snap.reviews
	s.users = snap.users
	s.sess = snap.sess
	s.verifs = snap.verifs
	s.recips = snap.recips
	s.jobs = snap.jobs
	s.order = snap.order
	s.updated = snap.updated
	s.artUpd = snap.artUpd
	s.holdUpd = snap.holdUpd
	s.events = snap.events
}

// runTx runs fn, restoring the pre-transaction state if fn fails, so the fake
// models the atomicity of the real store.
func (s *Store) runTx(fn func(tx ports.Store) error) error {
	s.mu.Lock()
	snap := s.snapshot()
	s.mu.Unlock()
	if err := fn(s); err != nil {
		s.mu.Lock()
		s.restore(snap)
		s.mu.Unlock()
		return err
	}
	return nil
}

func (s *Store) WithinTx(ctx context.Context, fn func(tx ports.Store) error) error {
	return s.runTx(fn)
}

// WithIngestLock serializes the ingest pass across processes in Postgres; in
// memory it is simply a transactional run with no concurrency to exclude.
func (s *Store) WithIngestLock(ctx context.Context, fn func(tx ports.Store) error) error {
	return s.runTx(fn)
}

var _ ports.Store = (*Store)(nil)
