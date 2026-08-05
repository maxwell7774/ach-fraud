// Package fakeports provides in-memory implementations of the ports interfaces
// for tests. They are intentionally faithful to the production semantics the
// pipeline relies on (uniqueness, joins, upsert jobs), so pipeline tests behave
// like they would against Postgres.
package fakeports

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
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
	jobs    map[string]*domain.Job
	order   []uuid.UUID
	updated map[uuid.UUID]time.Time
	artUpd  map[uuid.UUID]time.Time
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
		jobs:    map[string]*domain.Job{},
		updated: map[uuid.UUID]time.Time{},
		artUpd:  map[uuid.UUID]time.Time{},
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
	jobs    map[string]*domain.Job
	order   []uuid.UUID
	updated map[uuid.UUID]time.Time
	artUpd  map[uuid.UUID]time.Time
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
		jobs:    cloneJobs(s.jobs),
		order:   append([]uuid.UUID(nil), s.order...),
		updated: maps.Clone(s.updated),
		artUpd:  maps.Clone(s.artUpd),
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
	s.jobs = snap.jobs
	s.order = snap.order
	s.updated = snap.updated
	s.artUpd = snap.artUpd
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

func (s *Store) CreateSubmission(ctx context.Context, in domain.Submission) (domain.Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	s.subs[in.ID] = in
	return in, nil
}

func (s *Store) GetSubmission(ctx context.Context, id uuid.UUID) (domain.Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[id]
	if !ok {
		return domain.Submission{}, domain.ErrNotFound
	}
	return sub, nil
}

func (s *Store) FindSubmissionsBySourceChecksum(ctx context.Context, checksum string) ([]domain.Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Submission
	for _, v := range s.subs {
		if v.SourceChecksum == checksum {
			out = append(out, v)
		}
	}
	return out, nil
}

func (s *Store) SetSubmissionStatus(ctx context.Context, id uuid.UUID, st domain.SubmissionStatus, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[id]
	if !ok {
		return domain.ErrNotFound
	}
	sub.Status = st
	sub.FailedReason = reason
	s.subs[id] = sub
	return nil
}

func (s *Store) ListReceivedWithoutFixJob(ctx context.Context) ([]domain.Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Submission
	for _, sub := range s.subs {
		if sub.Status != domain.SubmissionReceived {
			continue
		}
		if _, ok := s.jobs[jobKey(domain.JobFixSubmission, sub.ID)]; ok {
			continue
		}
		out = append(out, sub)
	}
	return out, nil
}

func (s *Store) CreateArtifact(ctx context.Context, in domain.Artifact) (domain.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.arts {
		if a.SubmissionID == in.SubmissionID && a.Kind == in.Kind && in.Kind != domain.ArtifactRelease {
			return domain.Artifact{}, fmt.Errorf("duplicate artifact (submission %s, kind %s)", in.SubmissionID, in.Kind)
		}
	}
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	s.arts[in.ID] = in
	s.artUpd[in.ID] = s.Now()
	return in, nil
}

func (s *Store) GetArtifactBySubmissionKind(ctx context.Context, submissionID uuid.UUID, kind domain.ArtifactKind) (domain.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.arts {
		if a.SubmissionID == submissionID && a.Kind == kind {
			return a, nil
		}
	}
	return domain.Artifact{}, domain.ErrNotFound
}

func (s *Store) GetArtifactByID(ctx context.Context, id uuid.UUID) (domain.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.arts[id]
	if !ok {
		return domain.Artifact{}, domain.ErrNotFound
	}
	return a, nil
}

func (s *Store) ListArtifactsBySubmission(ctx context.Context, submissionID uuid.UUID) ([]domain.Artifact, error) {
	return s.ArtifactsFor(submissionID), nil
}

func (s *Store) SetArtifactState(ctx context.Context, id uuid.UUID, st domain.ArtifactState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.arts[id]
	if !ok {
		return domain.ErrNotFound
	}
	a.State = st
	s.arts[id] = a
	s.artUpd[id] = s.Now()
	return nil
}

func (s *Store) ListArtifactsByStateOlderThan(ctx context.Context, state domain.ArtifactState, cutoff time.Time) ([]domain.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Artifact
	for _, a := range s.arts {
		if a.State != state {
			continue
		}
		updated, ok := s.artUpd[a.ID]
		if !ok || !updated.Before(cutoff) {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// BackdateArtifacts moves every artifact's recorded updated_at back by d, so
// prune tests can make seeded artifacts older than the retention cutoff.
func (s *Store) BackdateArtifacts(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, t := range s.artUpd {
		s.artUpd[id] = t.Add(-d)
	}
}

func (s *Store) ListArtifactsByChecksum(ctx context.Context, checksum string) ([]domain.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Artifact
	for _, a := range s.arts {
		if a.Checksum == checksum {
			out = append(out, a)
		}
	}
	return out, nil
}

func (s *Store) CreateBatchHeader(ctx context.Context, in domain.BatchHeader) (domain.BatchHeader, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	s.headers[in.ID] = in
	return in, nil
}

func (s *Store) CreateBatchEntry(ctx context.Context, in domain.BatchEntry) (domain.BatchEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	s.entries[in.ID] = in
	return in, nil
}

func (s *Store) ListEntriesBySubmission(ctx context.Context, submissionID uuid.UUID) ([]domain.BatchEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.BatchEntry
	for _, e := range s.entries {
		hdr := s.headers[e.HeaderID]
		if hdr.SubmissionID != submissionID {
			continue
		}
		e.EffectiveDate = hdr.EffectiveDate
		e.CustomerID = hdr.CustomerID
		out = append(out, e)
	}
	return out, nil
}

func (s *Store) HasEntryByRdfiAccount(ctx context.Context, rdfi, account string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.Rdfi == rdfi && e.ReceiverAccount == account {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) SumVelocity(ctx context.Context, cutoff time.Time, submissionID uuid.UUID) ([]domain.VelocitySum, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Restrict to the velocity groups present in the submission being screened.
	targets := map[string]bool{}
	for _, e := range s.entries {
		hdr := s.headers[e.HeaderID]
		if hdr.SubmissionID != submissionID {
			continue
		}
		if e.TranCode != 22 && e.TranCode != 32 {
			continue
		}
		if hdr.EffectiveDate == nil || hdr.EffectiveDate.Before(cutoff) {
			continue
		}
		targets[velocityKeyFor(e.ReceiverAccount, e.Rdfi, hdr.CustomerID, hdr.EffectiveDate)] = true
	}

	sums := map[string]*domain.VelocitySum{}
	order := []string{}
	for _, e := range s.entries {
		hdr := s.headers[e.HeaderID]
		sub := s.subs[hdr.SubmissionID]
		if sub.Status != domain.SubmissionReady {
			continue
		}
		if j, ok := s.jobs[jobKey(domain.JobProcess, sub.ID)]; ok && j.State == domain.JobFailed {
			continue
		}
		if e.TranCode != 22 && e.TranCode != 32 {
			continue
		}
		if hdr.EffectiveDate == nil || hdr.EffectiveDate.Before(cutoff) {
			continue
		}
		key := velocityKeyFor(e.ReceiverAccount, e.Rdfi, hdr.CustomerID, hdr.EffectiveDate)
		if !targets[key] {
			continue
		}
		vs, ok := sums[key]
		if !ok {
			vs = &domain.VelocitySum{
				ReceiverAccount: e.ReceiverAccount,
				Rdfi:            e.Rdfi,
				EffectiveDate:   hdr.EffectiveDate,
				CustomerID:      hdr.CustomerID,
			}
			sums[key] = vs
			order = append(order, key)
		}
		vs.Total += e.Amount
	}
	out := make([]domain.VelocitySum, 0, len(order))
	for _, k := range order {
		out = append(out, *sums[k])
	}
	return out, nil
}

func velocityKeyFor(account, rdfi, customer string, eff *time.Time) string {
	day := ""
	if eff != nil {
		day = eff.Format("2006-01-02")
	}
	return account + "|" + rdfi + "|" + day + "|" + customer
}

func (s *Store) populateHold(h domain.Hold) domain.Hold {
	en, ok := s.entries[h.EntryID]
	if ok {
		h.EntryTrace = en.Trace
		h.EntryRdfi = en.Rdfi
		h.EntryReceiverName = en.ReceiverName
		h.EntryReceiverAcct = en.ReceiverAccount
		h.EntryAmount = en.Amount
		h.EntryTranCode = en.TranCode
		hdr := s.headers[en.HeaderID]
		h.EffectiveDate = hdr.EffectiveDate
		h.SubmissionID = hdr.SubmissionID
		h.CustomerID = hdr.CustomerID
		if sub, ok := s.subs[hdr.SubmissionID]; ok {
			h.Filename = sub.Filename
		}
	}
	return h
}

func (s *Store) CreateHold(ctx context.Context, entryID uuid.UUID, status domain.HoldStatus, reason string) (domain.Hold, error) {
	if s.HoldError != nil {
		if err := s.HoldError(entryID); err != nil {
			return domain.Hold{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range s.holds {
		if h.EntryID == entryID {
			return domain.Hold{}, fmt.Errorf("hold for entry %s already exists", entryID)
		}
	}
	h := domain.Hold{ID: uuid.New(), EntryID: entryID, Status: status, Reason: reason}
	s.holds[h.ID] = h
	return h, nil
}

func (s *Store) ListHoldsBySubmission(ctx context.Context, submissionID uuid.UUID) ([]domain.Hold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Hold
	for _, h := range s.holds {
		p := s.populateHold(h)
		if p.SubmissionID == submissionID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *Store) ListHoldsByReleaseArtifact(ctx context.Context, artifactID uuid.UUID) ([]domain.Hold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Hold
	for _, h := range s.holds {
		if h.ReleaseArtifactID != nil && *h.ReleaseArtifactID == artifactID {
			out = append(out, s.populateHold(h))
		}
	}
	return out, nil
}

func (s *Store) ListAllHolds(ctx context.Context) ([]domain.Hold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Hold
	for _, h := range s.holds {
		out = append(out, s.populateHold(h))
	}
	return out, nil
}

func (s *Store) GetHold(ctx context.Context, id uuid.UUID) (domain.Hold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.holds[id]
	if !ok {
		return domain.Hold{}, domain.ErrNotFound
	}
	return s.populateHold(h), nil
}

func (s *Store) ListHoldsByStatus(ctx context.Context, status string, limit int) ([]domain.Hold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Hold
	for _, h := range s.holds {
		if status != "" && string(h.Status) != status {
			continue
		}
		out = append(out, s.populateHold(h))
	}
	return out, nil
}

func (s *Store) ListHoldsFiltered(ctx context.Context, status, search string, start, end *time.Time, limit int) ([]domain.Hold, error) {
	rows, err := s.ListHoldsByStatus(ctx, status, limit)
	if err != nil {
		return nil, err
	}
	search = strings.ToLower(search)
	var out []domain.Hold
	for _, h := range rows {
		if !matchesHold(h, search) {
			continue
		}
		if start != nil && (h.EffectiveDate == nil || h.EffectiveDate.Before(*start)) {
			continue
		}
		if end != nil && (h.EffectiveDate != nil && h.EffectiveDate.After(*end)) {
			continue
		}
		out = append(out, h)
	}
	return out, nil
}

func matchesHold(h domain.Hold, search string) bool {
	if search == "" {
		return true
	}
	for _, v := range []string{h.Filename, h.EntryReceiverName, h.EntryReceiverAcct, h.EntryTrace} {
		if strings.Contains(strings.ToLower(v), search) {
			return true
		}
	}
	return false
}

func (s *Store) CountHoldsFiltered(ctx context.Context, status, search string, start, end *time.Time) (int64, error) {
	rows, err := s.ListHoldsFiltered(ctx, status, search, start, end, 100000)
	if err != nil {
		return 0, err
	}
	return int64(len(rows)), nil
}

func (s *Store) ListEntriesFiltered(ctx context.Context, search string, start, end *time.Time, limit int) ([]domain.BatchEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	search = strings.ToLower(search)
	var out []domain.BatchEntry
	for _, e := range s.entries {
		hdr := s.headers[e.HeaderID]
		sub := s.subs[hdr.SubmissionID]
		filename := sub.Filename
		e.EffectiveDate = hdr.EffectiveDate
		e.CustomerID = hdr.CustomerID
		e.Filename = filename
		if search != "" {
			fields := []string{e.Trace, e.Rdfi, e.ReceiverName, e.ReceiverAccount, filename}
			found := false
			for _, v := range fields {
				if strings.Contains(strings.ToLower(v), search) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if start != nil && (hdr.EffectiveDate == nil || hdr.EffectiveDate.Before(*start)) {
			continue
		}
		if end != nil && (hdr.EffectiveDate != nil && hdr.EffectiveDate.After(*end)) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *Store) CountEntriesFiltered(ctx context.Context, search string, start, end *time.Time) (int64, error) {
	rows, err := s.ListEntriesFiltered(ctx, search, start, end, 100000)
	if err != nil {
		return 0, err
	}
	return int64(len(rows)), nil
}

func (s *Store) ListHeadersFiltered(ctx context.Context, search string, start, end *time.Time, limit int) ([]domain.BatchHeader, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	search = strings.ToLower(search)
	var out []domain.BatchHeader
	for _, h := range s.headers {
		sub := s.subs[h.SubmissionID]
		h.Filename = sub.Filename
		if search != "" {
			fields := []string{h.CompanyName, h.CustomerID, h.CompanyDescription, sub.Filename}
			found := false
			for _, v := range fields {
				if strings.Contains(strings.ToLower(v), search) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if start != nil && (h.EffectiveDate == nil || h.EffectiveDate.Before(*start)) {
			continue
		}
		if end != nil && (h.EffectiveDate != nil && h.EffectiveDate.After(*end)) {
			continue
		}
		out = append(out, h)
	}
	return out, nil
}

func (s *Store) CountHeadersFiltered(ctx context.Context, search string, start, end *time.Time) (int64, error) {
	rows, err := s.ListHeadersFiltered(ctx, search, start, end, 100000)
	if err != nil {
		return 0, err
	}
	return int64(len(rows)), nil
}

func (s *Store) CountHoldsByStatus(ctx context.Context) (map[string]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int64{}
	for _, h := range s.holds {
		out[string(h.Status)]++
	}
	return out, nil
}

func (s *Store) ListSubmissions(ctx context.Context, status string, limit int) ([]domain.Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Submission
	for _, sub := range s.subs {
		if status != "" && string(sub.Status) != status {
			continue
		}
		out = append(out, sub)
	}
	return out, nil
}

func (s *Store) ListReviewsByHold(ctx context.Context, holdID uuid.UUID) ([]domain.Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Review
	for _, r := range s.reviews {
		if r.HoldID == holdID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (s *Store) ListEvents(ctx context.Context, limit, offset int) ([]domain.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Event
	for i := len(s.events) - 1 - offset; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.events[i])
	}
	return out, nil
}

func (s *Store) CountEvents(ctx context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return int64(len(s.events)), nil
}

func (s *Store) ListCombosWithHolds(ctx context.Context) ([]domain.HoldCombo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]domain.HoldCombo{}
	for _, h := range s.holds {
		en := s.entries[h.EntryID]
		key := en.Rdfi + "|" + en.ReceiverAccount
		seen[key] = domain.HoldCombo{Rdfi: en.Rdfi, ReceiverAccount: en.ReceiverAccount}
	}
	out := make([]domain.HoldCombo, 0, len(seen))
	for _, c := range seen {
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) ListCombosWithDeclinedHolds(ctx context.Context) ([]domain.HoldCombo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]domain.HoldCombo{}
	for _, h := range s.holds {
		if h.Status != domain.HoldDeclined {
			continue
		}
		en := s.entries[h.EntryID]
		key := en.Rdfi + "|" + en.ReceiverAccount
		seen[key] = domain.HoldCombo{Rdfi: en.Rdfi, ReceiverAccount: en.ReceiverAccount}
	}
	out := make([]domain.HoldCombo, 0, len(seen))
	for _, c := range seen {
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) ListCombosBySubmission(ctx context.Context, submissionID uuid.UUID, cutoff time.Time) ([]domain.HoldCombo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Distinct eligible combos present in the submission being screened.
	combos := map[string]*domain.HoldCombo{}
	order := []string{}
	for _, e := range s.entries {
		hdr := s.headers[e.HeaderID]
		if hdr.SubmissionID != submissionID {
			continue
		}
		if e.TranCode != 22 && e.TranCode != 32 {
			continue
		}
		if hdr.EffectiveDate == nil || hdr.EffectiveDate.Before(cutoff) {
			continue
		}
		key := e.Rdfi + "|" + e.ReceiverAccount
		if _, ok := combos[key]; !ok {
			combos[key] = &domain.HoldCombo{Rdfi: e.Rdfi, ReceiverAccount: e.ReceiverAccount}
			order = append(order, key)
		}
	}

	// Full hold history (no expiry) consulted only for those pairs.
	hasHold := map[string]bool{}
	hasDeclined := map[string]bool{}
	for _, h := range s.holds {
		en := s.entries[h.EntryID]
		key := en.Rdfi + "|" + en.ReceiverAccount
		hasHold[key] = true
		if h.Status == domain.HoldDeclined {
			hasDeclined[key] = true
		}
	}

	out := make([]domain.HoldCombo, 0, len(order))
	for _, k := range order {
		c := combos[k]
		c.HasHold = hasHold[k]
		c.HasDeclined = hasDeclined[k]
		out = append(out, *c)
	}
	return out, nil
}

func (s *Store) SetHoldStatus(ctx context.Context, id uuid.UUID, status domain.HoldStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.holds[id]
	if !ok {
		return domain.ErrNotFound
	}
	h.Status = status
	s.holds[id] = h
	return nil
}

func (s *Store) SetHoldReleaseArtifact(ctx context.Context, holdID, artifactID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.holds[holdID]
	if !ok {
		return domain.ErrNotFound
	}
	id := artifactID
	h.ReleaseArtifactID = &id
	s.holds[holdID] = h
	return nil
}

func (s *Store) CreateReview(ctx context.Context, r domain.Review) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ID = uuid.New()
	r.CreatedAt = time.Now()
	s.reviews = append(s.reviews, r)
	return nil
}

func (s *Store) UpsertUser(ctx context.Context, in domain.User) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.Subject == in.Subject {
			u.UPN = in.UPN
			u.Email = in.Email
			u.Name = in.Name
			if in.Role != "" {
				u.Role = in.Role
			}
			u.LastLogin = time.Now()
			s.users[u.ID] = u
			return u, nil
		}
	}
	in.ID = uuid.New()
	in.LastLogin = time.Now()
	in.CreatedAt = time.Now()
	if in.Role == "" {
		in.Role = domain.RoleWatcher
	}
	s.users[in.ID] = in
	return in, nil
}

func (s *Store) GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
}

func (s *Store) CreateSession(ctx context.Context, in domain.Session) (domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in.ID = uuid.New()
	in.CreatedAt = time.Now()
	s.sess[in.ID] = in
	return in, nil
}

func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sess {
		if sess.TokenHash == tokenHash {
			return sess, nil
		}
	}
	return domain.Session{}, domain.ErrNotFound
}

func (s *Store) DeleteSession(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sess[id]; !ok {
		return domain.ErrNotFound
	}
	delete(s.sess, id)
	return nil
}

func (s *Store) EnqueueJob(ctx context.Context, kind domain.JobKind, ref uuid.UUID, runAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := jobKey(kind, ref)
	if j, ok := s.jobs[key]; ok {
		j.RunAt = runAt
		j.State = domain.JobQueued
		j.LastError = ""
		s.updated[j.ID] = s.Now()
		return nil
	}
	j := &domain.Job{
		ID:    uuid.New(),
		Kind:  kind,
		Ref:   ref,
		State: domain.JobQueued,
		RunAt: runAt,
	}
	s.jobs[key] = j
	s.order = append(s.order, j.ID)
	s.updated[j.ID] = s.Now()
	return nil
}

func (s *Store) ClaimDueJob(ctx context.Context) (*domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	for _, id := range s.order {
		var j *domain.Job
		for _, candidate := range s.jobs {
			if candidate.ID == id {
				j = candidate
				break
			}
		}
		if j == nil || j.State != domain.JobQueued || j.RunAt.After(now) {
			continue
		}
		j.State = domain.JobInProgress
		s.updated[j.ID] = s.Now()
		cp := *j
		return &cp, nil
	}
	return nil, nil
}

func (s *Store) CompleteJob(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.ID == id {
			j.State = domain.JobDone
			s.updated[j.ID] = s.Now()
			return nil
		}
	}
	return domain.ErrNotFound
}

func (s *Store) FailJob(ctx context.Context, id uuid.UUID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.ID == id {
			j.State = domain.JobFailed
			j.LastError = reason
			j.Failures++
			s.updated[j.ID] = s.Now()
			return nil
		}
	}
	return domain.ErrNotFound
}

func (s *Store) RequeueJob(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.ID == id {
			j.State = domain.JobQueued
			j.LastError = ""
			j.RunAt = s.Now()
			s.updated[j.ID] = s.Now()
			return nil
		}
	}
	return domain.ErrNotFound
}

func (s *Store) RequeueStaleJobs(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	for _, j := range s.jobs {
		if j.State == domain.JobInProgress && now.Sub(s.updated[j.ID]) > 5*time.Minute {
			j.State = domain.JobQueued
		}
	}
	return nil
}

func (s *Store) ListJobsByState(ctx context.Context, st domain.JobState, limit int) ([]domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Job
	for _, j := range s.jobs {
		if j.State == st {
			out = append(out, *j)
		}
	}
	return out, nil
}

func (s *Store) ListJobsByRef(ctx context.Context, ref uuid.UUID) ([]domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Job
	for _, j := range s.jobs {
		if j.Ref == ref {
			out = append(out, *j)
		}
	}
	return out, nil
}

func (s *Store) AppendEvent(ctx context.Context, typ string, ref *uuid.UUID, payload json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, domain.Event{ID: uuid.New(), Type: typ, Ref: ref, Payload: payload, CreatedAt: time.Now()})
	return nil
}

var _ ports.Store = (*Store)(nil)
