package localfiles

import (
	"context"
	"testing"

	"github.com/27actions/ach/internal/checksum"
)

func TestFilesRejectsInvalidOrMismatchedChecksums(t *testing.T) {
	files := New(t.TempDir())
	data := []byte("artifact")
	valid := checksum.Bytes(data)

	if err := files.Put(context.Background(), "../escape", data); err == nil {
		t.Fatal("expected invalid checksum to be rejected")
	}
	if err := files.Put(context.Background(), valid, []byte("different")); err == nil {
		t.Fatal("expected mismatched data to be rejected")
	}
	if err := files.Put(context.Background(), valid, data); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := files.Get(context.Background(), valid)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("data = %q, want %q", got, data)
	}
}
