// Package fakes holds in-memory fakes of the services that the tool groups
// call, as httptest servers. Agents use them in evals, so that a run with a
// real model can never change a real account. Each fake records its writes.
package fakes

import (
	"encoding/json"
	"net/http"
	"slices"
	"sync"
)

// Write is one write that a fake got.
type Write struct {
	Op   string
	Args map[string]any
}

type log struct {
	mu     sync.Mutex
	writes []Write
}

func (l *log) add(op string, args map[string]any) {
	l.writes = append(l.writes, Write{Op: op, Args: args})
}

// Writes gives the writes so far.
func (l *log) Writes() []Write {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.writes)
}

// Ops gives the operation names of the writes so far.
func (l *log) Ops() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []string{}
	for _, w := range l.writes {
		out = append(out, w.Op)
	}
	return out
}

func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decode(r *http.Request) map[string]any {
	var m map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&m)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m
}
