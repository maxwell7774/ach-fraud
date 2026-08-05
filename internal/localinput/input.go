// Package localinput implements ports.Input over a directory on disk. It
// lists *.ach files in the intake directory and reads them into memory.
package localinput

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/27actions/ach/internal/ports"
)

// Input reads ACH files from a directory.
type Input struct {
	dir string
}

func New(dir string) *Input {
	return &Input{dir: dir}
}

func (i *Input) Scan(_ context.Context) ([]ports.InputFile, error) {
	entries, err := os.ReadDir(i.dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".ach") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var out []ports.InputFile
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(i.dir, name))
		if err != nil {
			return nil, err
		}
		out = append(out, ports.InputFile{Filename: name, Data: data})
	}
	return out, nil
}

func (i *Input) Remove(_ context.Context, filename string) error {
	return os.Remove(filepath.Join(i.dir, filename))
}
