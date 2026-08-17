// Package localfiles implements ports.Files as a content-addressed store on
// disk: each artifact's bytes live at <root>/<checksum>, keyed purely by their
// sha256. Lifecycle (staged/published/archived/pruned) is tracked on artifact
// rows in the database, not by moving files between directories.
package localfiles

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/27actions/ach/internal/checksum"
)

// Files stores artifact bytes under root, one file per checksum.
type Files struct {
	root string
}

func New(root string) *Files {
	return &Files{root: root}
}

func (f *Files) path(checksum string) string {
	// Two-level sharding keeps a directory from growing unbounded. Guard the
	// slice so a malformed checksum can never panic; the root holds short keys.
	if len(checksum) < 2 {
		return filepath.Join(f.root, checksum)
	}
	return filepath.Join(f.root, checksum[:2], checksum)
}

func validateChecksum(sum string) error {
	if !checksum.Valid(sum) {
		return fmt.Errorf("invalid artifact checksum %q", sum)
	}
	return nil
}

func (f *Files) Get(_ context.Context, checksum string) ([]byte, error) {
	if err := validateChecksum(checksum); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(f.path(checksum))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("artifact %s not found", checksum)
		}
		return nil, err
	}
	if got := checksumBytes(data); got != checksum {
		return nil, fmt.Errorf("artifact %s checksum mismatch: got %s", checksum, got)
	}
	return data, nil
}

func (f *Files) Put(_ context.Context, checksum string, data []byte) error {
	if err := validateChecksum(checksum); err != nil {
		return err
	}
	if got := checksumBytes(data); got != checksum {
		return fmt.Errorf("artifact checksum mismatch: key %s, data %s", checksum, got)
	}
	final := f.path(checksum)
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(final), ".ach-tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, final)
}

func (f *Files) Delete(_ context.Context, checksum string) error {
	if err := validateChecksum(checksum); err != nil {
		return err
	}
	path := f.path(checksum)
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	// Remove the shard directory if it is now empty.
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
	return nil
}

func checksumBytes(data []byte) string {
	return checksum.Bytes(data)
}
