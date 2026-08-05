package pipeline

import (
	"context"

	"github.com/27actions/ach/internal/achp"
	"github.com/27actions/ach/internal/ports"

	"github.com/moov-io/ach"
)

// readFile fetches artifact bytes from the content store and parses them
// leniently.
func readFile(ctx context.Context, f ports.Files, checksum string) (*ach.File, error) {
	data, err := f.Get(ctx, checksum)
	if err != nil {
		return nil, err
	}
	return achp.Read(data, true)
}
