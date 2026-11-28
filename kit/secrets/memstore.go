package secrets

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
)

// MemoryStore is a Store in memory, for tests and for a dev agent.
type MemoryStore struct {
	mu      sync.Mutex
	folders map[string]bool
	values  map[string]string
	// FailPut, when set, makes Put fail.
	FailPut error
}

func NewMemoryStore(folders ...string) *MemoryStore {
	m := &MemoryStore{folders: map[string]bool{"/": true}, values: map[string]string{}}
	for _, f := range folders {
		m.folders[f] = true
	}
	return m
}

func key(p Place) string { return p.Path + "/" + p.Name }

func (m *MemoryStore) ListFolders(_ context.Context, path string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	prefix := strings.TrimSuffix(path, "/") + "/"
	for f := range m.folders {
		if rest, ok := strings.CutPrefix(f, prefix); ok && rest != "" && !strings.Contains(rest, "/") {
			out = append(out, rest)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (m *MemoryStore) ListSecretNames(_ context.Context, path string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for k := range m.values {
		if rest, ok := strings.CutPrefix(k, path+"/"); ok && !strings.Contains(rest, "/") {
			out = append(out, rest)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (m *MemoryStore) Check(_ context.Context, p Place) (bool, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, exists := m.values[key(p)]
	return m.folders[p.Path], exists, nil
}

func (m *MemoryStore) Put(_ context.Context, p Place, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FailPut != nil {
		return m.FailPut
	}
	if p.Path == "" || p.Name == "" {
		return errors.New("a place needs a path and a name")
	}
	for f := p.Path; f != "" && f != "/"; f = f[:strings.LastIndex(f, "/")] {
		m.folders[f] = true
	}
	m.values[key(p)] = value
	return nil
}

// Value is for tests only: a Store never gives a value back.
func (m *MemoryStore) Value(p Place) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[key(p)]
	return v, ok
}
