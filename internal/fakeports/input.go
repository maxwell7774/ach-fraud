package fakeports

import (
	"context"
	"sync"

	"github.com/27actions/ach/internal/ports"
)

// Input is an in-memory intake area.
type Input struct {
	mu    sync.Mutex
	files []ports.InputFile
}

func NewInput() *Input {
	return &Input{}
}

// Add places a file in the intake area.
func (i *Input) Add(filename string, data []byte) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.files = append(i.files, ports.InputFile{Filename: filename, Data: data})
}

func (i *Input) Scan(_ context.Context) ([]ports.InputFile, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := make([]ports.InputFile, len(i.files))
	copy(out, i.files)
	return out, nil
}

func (i *Input) Remove(_ context.Context, filename string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	for j, f := range i.files {
		if f.Filename == filename {
			i.files = append(i.files[:j], i.files[j+1:]...)
			return nil
		}
	}
	return nil
}

// Remaining returns the files still in the intake area.
func (i *Input) Remaining() []string {
	i.mu.Lock()
	defer i.mu.Unlock()
	var out []string
	for _, f := range i.files {
		out = append(out, f.Filename)
	}
	return out
}

var _ ports.Input = (*Input)(nil)
