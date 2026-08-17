// Package sender implements ports.Sender for the local outgoing directory.
// Artifacts are written under outgoing/<kind>/<checksum>.ach so a downstream
// process can pick them up. A later implementation could transmit over SFTP or
// push to object storage without changing the pipeline.
package sender

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/27actions/ach/internal/checksum"
	"github.com/27actions/ach/internal/domain"
)

// Sender writes published artifacts into the outgoing directory.
type Sender struct {
	outgoingDir string
}

func New(outgoingDir string) *Sender {
	return &Sender{outgoingDir: outgoingDir}
}

// Send writes the artifact bytes to outgoing/<kind>/<checksum>.ach. The
// write is atomic (temp file + hard link) so a downstream reader never sees a
// partial file and retries never replace an existing output.
func (s *Sender) Send(_ context.Context, a domain.Artifact, data []byte) error {
	if !checksum.Valid(a.Checksum) {
		return fmt.Errorf("invalid artifact checksum %q", a.Checksum)
	}
	if got := checksum.Bytes(data); got != a.Checksum {
		return fmt.Errorf("artifact checksum mismatch: key %s, data %s", a.Checksum, got)
	}
	dir := filepath.Join(s.outgoingDir, string(a.Kind))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating outgoing dir %s: %w", dir, err)
	}
	final := filepath.Join(dir, a.Checksum+".ach")
	tmpFile, err := os.CreateTemp(dir, ".ach-send-*.tmp")
	if err != nil {
		return fmt.Errorf("creating outgoing temp file: %w", err)
	}
	tmp := tmpFile.Name()
	defer os.Remove(tmp)
	if err := tmpFile.Chmod(0o644); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("setting outgoing file permissions: %w", err)
	}
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("writing outgoing file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing outgoing file: %w", err)
	}
	// Link, rather than overwrite, makes a retry after the database update a
	// no-op and avoids replacing a file that a downstream consumer may already
	// be reading. A pre-existing file is valid only if its content matches the
	// checksum-derived destination.
	if err := os.Link(tmp, final); err == nil {
		return nil
	} else if !os.IsExist(err) {
		return fmt.Errorf("finalizing outgoing file: %w", err)
	}
	existing, err := os.ReadFile(final)
	if err != nil {
		return fmt.Errorf("checking existing outgoing file: %w", err)
	}
	if got := checksum.Bytes(existing); got != a.Checksum {
		return fmt.Errorf("existing outgoing file %s checksum mismatch: got %s", final, got)
	}
	return nil
}
