package sender

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/27actions/ach/internal/checksum"
	"github.com/27actions/ach/internal/domain"
)

func TestSendRejectsInvalidArtifactData(t *testing.T) {
	s := New(t.TempDir())
	if err := s.Send(context.Background(), domain.Artifact{Kind: domain.ArtifactCleaned, Checksum: "bad"}, []byte("data")); err == nil {
		t.Fatal("expected invalid checksum to be rejected")
	}
	data := []byte("data")
	if err := s.Send(context.Background(), domain.Artifact{Kind: domain.ArtifactCleaned, Checksum: checksum.Bytes([]byte("other"))}, data); err == nil {
		t.Fatal("expected checksum mismatch to be rejected")
	}
}

func TestSendIsIdempotentAndDoesNotReplaceExistingOutput(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	data := []byte("artifact")
	a := domain.Artifact{Kind: domain.ArtifactCleaned, Checksum: checksum.Bytes(data)}
	if err := s.Send(context.Background(), a, data); err != nil {
		t.Fatalf("first send: %v", err)
	}
	if err := s.Send(context.Background(), a, data); err != nil {
		t.Fatalf("retry send: %v", err)
	}
	path := filepath.Join(dir, string(a.Kind), a.Checksum+".ach")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("output = %q, want %q", got, data)
	}
}

func TestSendRejectsConflictingExistingOutput(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	data := []byte("artifact")
	a := domain.Artifact{Kind: domain.ArtifactCleaned, Checksum: checksum.Bytes(data)}
	path := filepath.Join(dir, string(a.Kind))
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, a.Checksum+".ach"), []byte("wrong"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.Send(context.Background(), a, data); err == nil {
		t.Fatal("expected conflicting output to be rejected")
	}
}
