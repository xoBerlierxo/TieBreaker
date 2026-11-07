package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Fake is an in-memory Client for tests (SPEC §13: "Provide llm.Fake"). It
// returns scripted responses in order and records every request.
type Fake struct {
	mu        sync.Mutex
	Responses []string // returned in order; the last one repeats
	Err       error    // if set, every call fails with it
	Texts     []TextRequest
	JSONs     []JSONRequest
}

func (f *Fake) next() (string, error) {
	if f.Err != nil {
		return "", f.Err
	}
	if len(f.Responses) == 0 {
		return "", ErrEmptyOutput
	}
	r := f.Responses[0]
	if len(f.Responses) > 1 {
		f.Responses = f.Responses[1:]
	}
	return r, nil
}

// GenerateText implements Client.
func (f *Fake) GenerateText(_ context.Context, req TextRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Texts = append(f.Texts, req)
	return f.next()
}

// GenerateJSON implements Client.
func (f *Fake) GenerateJSON(_ context.Context, req JSONRequest, out any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.JSONs = append(f.JSONs, req)
	r, err := f.next()
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(r), out); err != nil {
		return fmt.Errorf("fake JSON response: %w", err)
	}
	return nil
}

// Calls returns how many requests were made.
func (f *Fake) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Texts) + len(f.JSONs)
}
