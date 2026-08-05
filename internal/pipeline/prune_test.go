package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/fakeports"
	"github.com/27actions/ach/internal/notifier"

	"github.com/google/uuid"
)

func pruneDeps(t *testing.T) (Deps, *fakeports.Store, *fakeports.Files, *fakeports.Clock) {
	t.Helper()
	st := fakeports.NewStore()
	files := fakeports.NewFiles()
	clk := fakeports.NewClock(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))
	st.Now = clk.Now
	d := Deps{
		Store: st, Files: files, Sender: fakeports.NewSender(), Notifier: notifier.Noop{},
		Clock:  clk,
		Policy: domain.Policy{},
	}
	return d, st, files, clk
}

func seedArtifact(t *testing.T, st *fakeports.Store, checksum string, kind domain.ArtifactKind, state domain.ArtifactState) uuid.UUID {
	t.Helper()
	sub, err := st.CreateSubmission(context.Background(), domain.Submission{
		Filename: "f.ach", Status: domain.SubmissionReady, ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	a, err := st.CreateArtifact(context.Background(), domain.Artifact{
		SubmissionID: sub.ID, Kind: kind, Checksum: checksum, State: state,
	})
	if err != nil {
		t.Fatalf("seed artifact: %v", err)
	}
	return a.ID
}

// TestPruneRespectsSharedChecksum: bytes are only deleted once every artifact
// referencing the checksum has been pruned, so a newer generation sharing the
// same bytes survives.
func TestPruneRespectsSharedChecksum(t *testing.T) {
	d, st, files, _ := pruneDeps(t)
	const sum = "aaaa"
	seedArtifact(t, st, sum, domain.ArtifactOriginal, domain.ArtifactArchived)
	// A newer generation still holds the same bytes.
	staged := seedArtifact(t, st, sum, domain.ArtifactOriginal, domain.ArtifactStaged)
	files.Put(context.Background(), sum, []byte("data"))
	st.BackdateArtifacts(48 * time.Hour)

	res, err := Prune(context.Background(), d, 0)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if res.Rows != 1 || res.Deleted != 0 {
		t.Fatalf("expected 1 row pruned and 0 bytes deleted, got %+v", res)
	}
	if files.Bytes(sum) == nil {
		t.Fatal("bytes were deleted while a staged artifact still references them")
	}

	// Retire the newer generation too; now the bytes go away.
	if err := st.SetArtifactState(context.Background(), staged, domain.ArtifactArchived); err != nil {
		t.Fatalf("archive staged: %v", err)
	}
	st.BackdateArtifacts(48 * time.Hour)
	res, err = Prune(context.Background(), d, 0)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if res.Rows != 1 || res.Deleted != 1 {
		t.Fatalf("expected 1 row pruned and the shared bytes deleted once, got %+v", res)
	}
	if files.Bytes(sum) != nil {
		t.Fatal("bytes should have been deleted")
	}
}

// TestPruneIdempotent: pruning again deletes nothing new.
func TestPruneIdempotent(t *testing.T) {
	d, st, files, _ := pruneDeps(t)
	const sum = "bbbb"
	seedArtifact(t, st, sum, domain.ArtifactOriginal, domain.ArtifactArchived)
	files.Put(context.Background(), sum, []byte("data"))
	st.BackdateArtifacts(48 * time.Hour)

	if _, err := Prune(context.Background(), d, 0); err != nil {
		t.Fatalf("prune: %v", err)
	}
	res, err := Prune(context.Background(), d, 0)
	if err != nil {
		t.Fatalf("prune again: %v", err)
	}
	if res.Rows != 0 || res.Deleted != 0 {
		t.Fatalf("expected no-op second prune, got %+v", res)
	}
}

// TestPruneSkipsRecentArtifacts: artifacts younger than the retention window
// are not candidates (guards the cutoff in the store query).
func TestPruneSkipsRecentArtifacts(t *testing.T) {
	d, st, files, _ := pruneDeps(t)
	const sum = "cccc"
	seedArtifact(t, st, sum, domain.ArtifactOriginal, domain.ArtifactArchived)
	files.Put(context.Background(), sum, []byte("data"))

	res, err := Prune(context.Background(), d, 30)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if res.Rows != 0 || res.Deleted != 0 {
		t.Fatalf("expected recent artifacts to survive 30-day retention, got %+v", res)
	}
	if files.Bytes(sum) == nil {
		t.Fatal("recent artifact bytes were deleted")
	}
}
