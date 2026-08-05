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
// write is atomic (temp file + rename) so a downstream reader never sees a
// partial file.
func (s *Sender) Send(_ context.Context, a domain.Artifact, data []byte) error {
	dir := filepath.Join(s.outgoingDir, string(a.Kind))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating outgoing dir %s: %w", dir, err)
	}
	final := filepath.Join(dir, a.Checksum+".ach")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing outgoing file: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		return fmt.Errorf("finalizing outgoing file: %w", err)
	}
	return nil
}
