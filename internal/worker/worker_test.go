package worker

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/27actions/ach/internal/achp"
	"github.com/27actions/ach/internal/checksum"
	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/fakeports"
	"github.com/27actions/ach/internal/notifier"
	"github.com/27actions/ach/internal/pipeline"
	"github.com/27actions/ach/internal/testutil"
	"github.com/google/uuid"
)

func testDeps(t *testing.T) (pipeline.Deps, *fakeports.Store, *fakeports.Files, *fakeports.Sender) {
	t.Helper()
	st := fakeports.NewStore()
	files := fakeports.NewFiles()
	sender := fakeports.NewSender()
	d := pipeline.Deps{
		Store:    st,
		Files:    files,
		Sender:   sender,
		Notifier: notifier.Noop{},
		Clock:    fakeports.NewClock(time.Now()),
		Policy: domain.Policy{
			HoldDays:           30,
			HoldingRDFI:        "333333334",
			HoldingAccount:     "555555",
			HoldSingleAmount:   100000,
			HoldVelocityAmount: 100000,
			DedupWindowDays:    30,
		},
	}
	return d, st, files, sender
}

func creditFile(t *testing.T, amount int) []byte {
	t.Helper()
	data, err := testutil.CreditFile([]testutil.Entry{
		{Account: "123456789", Name: "Receiver A", Amount: amount, RDFI: "231380104"},
	}, time.Now().Format("060102"))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return data
}

func submissionID(t *testing.T, st *fakeports.Store, filename string) domain.Submission {
	t.Helper()
	for _, s := range st.Submissions() {
		if s.Filename == filename {
			return s
		}
	}
	t.Fatalf("submission %q not found", filename)
	return domain.Submission{}
}

func artifactState(t *testing.T, st *fakeports.Store, filename string, kind domain.ArtifactKind) domain.ArtifactState {
	t.Helper()
	sub := submissionID(t, st, filename)
	for _, a := range st.ArtifactsFor(sub.ID) {
		if a.Kind == kind {
			return a.State
		}
	}
	t.Fatalf("artifact %s/%s not found", filename, kind)
	return ""
}

func TestRunFullChainHeld(t *testing.T) {
	d, st, _, sender := testDeps(t)
	in := fakeports.NewInput()
	in.Add("held.ach", creditFile(t, 200000))

	rep, err := New(d).Run(context.Background(), in)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep.Ingested != 1 || rep.Completed != 5 || rep.Waiting != 1 || rep.Failed != 0 {
		t.Fatalf("unexpected report: %+v", rep)
	}

	// The intercept shipped, the release is waiting on the hold.
	sent := sender.SentKinds()
	if len(sent[domain.ArtifactCleaned]) != 1 {
		t.Fatalf("expected cleaned sent, got %v", sent)
	}
	if len(sent[domain.ArtifactRelease]) != 0 {
		t.Fatalf("release must not ship before approval")
	}
	if got := artifactState(t, st, "held.ach", domain.ArtifactCleaned); got != domain.ArtifactPublished {
		t.Fatalf("cleaned state = %s", got)
	}

	// Approve the hold; the release ships and the file archives.
	holds := st.HoldsFor(submissionID(t, st, "held.ach").ID)
	if len(holds) != 1 {
		t.Fatalf("expected 1 hold, got %d", len(holds))
	}
	if err := pipeline.ApproveHold(context.Background(), d, holds[0].ID, "tester", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}
	rep, err = New(d).Run(context.Background(), in)
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if rep.Completed != 2 || rep.Waiting != 0 || rep.Failed != 0 {
		t.Fatalf("unexpected re-run report: %+v", rep)
	}
	sent = sender.SentKinds()
	if len(sent[domain.ArtifactRelease]) != 1 {
		t.Fatalf("expected release sent after approval, got %v", sent)
	}
	for _, kind := range []domain.ArtifactKind{domain.ArtifactOriginal, domain.ArtifactFixed} {
		if got := artifactState(t, st, "held.ach", kind); got != domain.ArtifactArchived {
			t.Fatalf("%s state = %s, want archived", kind, got)
		}
	}

	// A third run is a clean no-op.
	rep, err = New(d).Run(context.Background(), in)
	if err != nil {
		t.Fatalf("third run: %v", err)
	}
	if rep.Completed != 0 || rep.Waiting != 0 || rep.Failed != 0 {
		t.Fatalf("expected no-op third run, got %+v", rep)
	}
}

func TestRunNoHoldArchives(t *testing.T) {
	d, st, _, sender := testDeps(t)
	in := fakeports.NewInput()
	in.Add("small.ach", creditFile(t, 2500))

	rep, err := New(d).Run(context.Background(), in)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep.Completed != 6 || rep.Waiting != 0 || rep.Failed != 0 {
		t.Fatalf("unexpected report: %+v", rep)
	}
	sent := sender.SentKinds()
	if len(sent[domain.ArtifactCleaned]) != 1 || len(sent[domain.ArtifactRelease]) != 0 {
		t.Fatalf("unexpected sends: %v", sent)
	}
	for _, kind := range []domain.ArtifactKind{domain.ArtifactOriginal, domain.ArtifactFixed} {
		if got := artifactState(t, st, "small.ach", kind); got != domain.ArtifactArchived {
			t.Fatalf("%s state = %s, want archived", kind, got)
		}
	}
}

// TestRunDeclineBlocksRelease: declining a hold after process blocks the
// release — the publish_release job completes cleanly (not a failure) and the
// release artifact never ships, so the blocked intercept stays visible.
func TestRunDeclineBlocksRelease(t *testing.T) {
	d, st, _, sender := testDeps(t)
	in := fakeports.NewInput()
	in.Add("held.ach", creditFile(t, 200000))

	if _, err := New(d).Run(context.Background(), in); err != nil {
		t.Fatalf("run: %v", err)
	}
	sub := submissionID(t, st, "held.ach")
	holds := st.HoldsFor(sub.ID)
	if len(holds) != 1 {
		t.Fatalf("expected 1 hold, got %d", len(holds))
	}
	if err := pipeline.DeclineHold(context.Background(), d, holds[0].ID, "tester", ""); err != nil {
		t.Fatalf("decline: %v", err)
	}

	rep, err := New(d).Run(context.Background(), in)
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if rep.Failed != 0 {
		t.Fatalf("a blocked release must not be a failure, got %+v", rep)
	}
	// The publish_release job ref is the release artifact id; it completes done.
	var release *domain.Artifact
	for _, a := range st.ArtifactsFor(sub.ID) {
		if a.Kind == domain.ArtifactRelease {
			release = &a
			break
		}
	}
	if release == nil {
		t.Fatal("expected a release artifact for the held entry")
	}
	if job := st.Job(domain.JobPublishRelease, release.ID); job == nil || job.State != domain.JobDone {
		t.Fatalf("publish_release job = %+v, want done", job)
	}
	if len(sender.SentKinds()[domain.ArtifactRelease]) != 0 {
		t.Fatal("a blocked release must not ship")
	}
	blocked := false
	for _, ev := range st.Events() {
		if ev.Type == "release_blocked" {
			blocked = true
		}
	}
	if !blocked {
		t.Fatal("expected a release_blocked event")
	}

	// The blocked release retires and the rest of the file archives, so the
	// release, original, and fixed artifacts are all retired.
	for _, kind := range []domain.ArtifactKind{
		domain.ArtifactRelease, domain.ArtifactOriginal, domain.ArtifactFixed,
	} {
		if got := artifactState(t, st, "held.ach", kind); got != domain.ArtifactArchived {
			t.Fatalf("%s state = %s, want archived", kind, got)
		}
	}
}

// TestRunPerGroupReleases: a velocity split and an unrelated hold produce two
// separate release artifacts. Approving the unrelated hold ships only its
// release; approving the whole velocity group ships theirs together, then the
// file archives.
func TestRunPerGroupReleases(t *testing.T) {
	d, st, files, sender := testDeps(t)
	in := fakeports.NewInput()
	data, err := testutil.CreditFile([]testutil.Entry{
		{Account: "VEL1", Name: "Velocity One", Amount: 60000, RDFI: "231380104"},
		{Account: "VEL1", Name: "Velocity Two", Amount: 60000, RDFI: "231380104"},
		{Account: "SOLO", Name: "Solo Hold", Amount: 150000, RDFI: "231380104"},
	}, time.Now().Format("060102"))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	in.Add("multi.ach", data)

	if _, err := New(d).Run(context.Background(), in); err != nil {
		t.Fatalf("run: %v", err)
	}

	sub := submissionID(t, st, "multi.ach")
	holds := st.HoldsFor(sub.ID)
	if len(holds) != 3 {
		t.Fatalf("expected 3 holds, got %d", len(holds))
	}
	var releases []domain.Artifact
	for _, a := range st.ArtifactsFor(sub.ID) {
		if a.Kind == domain.ArtifactRelease {
			releases = append(releases, a)
		}
	}
	if len(releases) != 2 {
		t.Fatalf("expected 2 release artifacts, got %d", len(releases))
	}

	legs := func(a domain.Artifact) int {
		raw, err := files.Get(context.Background(), a.Checksum)
		if err != nil {
			t.Fatalf("get release bytes: %v", err)
		}
		f, err := achp.Read(raw, true)
		if err != nil {
			t.Fatalf("read release: %v", err)
		}
		return achp.TotalEntries(f)
	}
	counts := []int{legs(releases[0]), legs(releases[1])}
	sort.Ints(counts)
	if counts[0] != 1 || counts[1] != 2 {
		t.Fatalf("expected release legs 1 and 2, got %v", counts)
	}

	var soloHold *domain.Hold
	var velIDs []uuid.UUID
	for i := range holds {
		switch holds[i].EntryReceiverAcct {
		case "SOLO":
			soloHold = &holds[i]
		case "VEL1":
			velIDs = append(velIDs, holds[i].ID)
		}
	}
	if soloHold == nil || len(velIDs) != 2 {
		t.Fatalf("holds not grouped as expected: solo=%v vel=%v", soloHold != nil, velIDs)
	}

	// Approve only the unrelated hold: its release ships, the velocity group waits.
	if err := pipeline.ApproveHold(context.Background(), d, soloHold.ID, "tester", ""); err != nil {
		t.Fatalf("approve solo: %v", err)
	}
	if _, err := New(d).Run(context.Background(), in); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if got := len(sender.SentKinds()[domain.ArtifactRelease]); got != 1 {
		t.Fatalf("expected 1 release sent after solo approval, got %d", got)
	}

	// Approving a SINGLE hold in the velocity group decides the whole group:
	// both become approved and their release ships together.
	if err := pipeline.ApproveHold(context.Background(), d, velIDs[0], "tester", ""); err != nil {
		t.Fatalf("approve one group member: %v", err)
	}
	for _, h := range st.HoldsFor(sub.ID) {
		if h.EntryReceiverAcct == "VEL1" && h.Status != domain.HoldApproved {
			t.Fatalf("group member %s status = %s, want approved (cascade)", h.ID, h.Status)
		}
	}
	if _, err := New(d).Run(context.Background(), in); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if got := len(sender.SentKinds()[domain.ArtifactRelease]); got != 2 {
		t.Fatalf("expected 2 releases sent, got %d", got)
	}
	for _, kind := range []domain.ArtifactKind{domain.ArtifactOriginal, domain.ArtifactFixed} {
		if got := artifactState(t, st, "multi.ach", kind); got != domain.ArtifactArchived {
			t.Fatalf("%s state = %s, want archived", kind, got)
		}
	}
}

// TestRunReconcilesOrphanedSubmission: a received submission that was
// registered but never got its fix job (a crash window) is re-driven by Run.
func TestRunReconcilesOrphanedSubmission(t *testing.T) {
	d, st, files, _ := testDeps(t)
	ctx := context.Background()

	// Simulate the crash state: bytes + submission + original artifact, but no
	// fix job, and an empty intake area.
	data := creditFile(t, 2500)
	sum := checksum.Bytes(data)
	sub, err := st.CreateSubmission(ctx, domain.Submission{
		Filename: "orphan.ach", SourceChecksum: sum, Status: domain.SubmissionReceived, ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := st.CreateArtifact(ctx, domain.Artifact{
		SubmissionID: sub.ID, Kind: domain.ArtifactOriginal, Checksum: sum, State: domain.ArtifactStaged,
	}); err != nil {
		t.Fatalf("seed artifact: %v", err)
	}
	if err := files.Put(ctx, sum, data); err != nil {
		t.Fatalf("put: %v", err)
	}

	in := fakeports.NewInput()
	rep, err := New(d).Run(ctx, in)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep.Recovered != 1 {
		t.Fatalf("expected 1 recovered submission, got %+v", rep)
	}
	if got := artifactState(t, st, "orphan.ach", domain.ArtifactOriginal); got != domain.ArtifactArchived {
		t.Fatalf("orphan was not finished: original state = %s", got)
	}
}

func TestRunUnfixableFailsSubmission(t *testing.T) {
	d, st, _, _ := testDeps(t)
	in := fakeports.NewInput()
	in.Add("broken.ach", []byte("this is not an ACH file\n"))

	rep, err := New(d).Run(context.Background(), in)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep.Ingested != 1 || rep.Failed != 1 || rep.Completed != 0 {
		t.Fatalf("unexpected report: %+v", rep)
	}
	sub := submissionID(t, st, "broken.ach")
	if sub.Status != domain.SubmissionFailed {
		t.Fatalf("submission status = %s, want failed", sub.Status)
	}
	if job := st.Job(domain.JobFixSubmission, sub.ID); job.State != domain.JobFailed {
		t.Fatalf("fix job state = %s, want failed", job.State)
	}
}

func TestRunDedupSkipsIdenticalResubmission(t *testing.T) {
	d, _, _, _ := testDeps(t)
	in := fakeports.NewInput()
	in.Add("small.ach", creditFile(t, 2500))

	if _, err := New(d).Run(context.Background(), in); err != nil {
		t.Fatalf("run: %v", err)
	}
	// Re-present the identical file (as if it were dropped again).
	in.Add("small.ach", creditFile(t, 2500))
	rep, err := New(d).Run(context.Background(), in)
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if rep.Ingested != 0 || rep.Skipped != 1 {
		t.Fatalf("expected duplicate skipped, got %+v", rep)
	}
}
