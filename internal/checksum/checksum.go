// Package checksum computes the sha256 hex checksums used in artifact filenames
// and to dedupe processed files.
package checksum

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// Valid reports whether s is a canonical SHA-256 hex checksum.
func Valid(s string) bool {
	if len(s) != sha256.Size*2 || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func File(path string) (string, error) {
	fd, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening %s: %w", path, err)
	}
	defer fd.Close()

	h := sha256.New()
	if _, err := io.Copy(h, fd); err != nil {
		return "", fmt.Errorf("hashing %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func Bytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
