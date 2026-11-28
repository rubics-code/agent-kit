package openrouter

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const testKey = "test-management-key"

// fakeOpenRouter is the key management API in memory. It records every write.
type fakeOpenRouter struct {
	*httptest.Server
	mu     sync.Mutex
	keys   map[string]*Key
	order  []string
	values map[string]string // hash to key value
	writes []map[string]any
	next   int
}

func newFake(t *testing.T) *fakeOpenRouter {
	f := &fakeOpenRouter{keys: map[string]*Key{}, values: map[string]string{}}
	lim := 20.0
	f.add("Example", &lim, "monthly", 3, "")
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeOpenRouter) client() *Client {
	return &Client{ManagementKey: testKey, BaseURL: f.URL, HTTP: f.Client()}
}

func (f *fakeOpenRouter) add(name string, limit *float64, reset string, usage float64, expires string) *Key {
	f.next++
	hash := fmt.Sprintf("%064x", f.next)
	k := &Key{Hash: hash, Name: name, Label: "sk-or-v1-" + hash[61:] + "..." + hash[60:], Limit: limit, LimitReset: &reset,
		Usage: usage, UsageMonthly: usage, CreatedAt: "2026-11-01T00:00:00Z"}
	if limit != nil {
		r := math.Max(0, *limit-usage)
		k.LimitRemaining = &r
	}
	if expires != "" {
		k.ExpiresAt = &expires
	}
	f.keys[hash] = k
	f.order = append(f.order, hash)
	return k
}

func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeOpenRouter) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+testKey {
		send(w, 401, map[string]any{"error": map[string]any{"code": 401, "message": "No auth credentials found"}})
		return
	}
	hash, isKey := strings.CutPrefix(r.URL.Path, "/keys/")
	switch {
	case r.URL.Path == "/credits":
		send(w, 200, map[string]any{"data": Credits{TotalCredits: 100, TotalUsage: 12.5}})
	case r.URL.Path == "/keys" && r.Method == http.MethodGet:
		out := []*Key{}
		if r.URL.Query().Get("offset") == "0" {
			for _, h := range f.order {
				if k := f.keys[h]; !k.Disabled || r.URL.Query().Get("include_disabled") == "true" {
					out = append(out, k)
				}
			}
		}
		send(w, 200, map[string]any{"data": out})
	case r.URL.Path == "/keys" && r.Method == http.MethodPost:
		var in NewKey
		_ = json.NewDecoder(r.Body).Decode(&in)
		b, _ := json.Marshal(in)
		var rec map[string]any
		_ = json.Unmarshal(b, &rec)
		f.writes = append(f.writes, map[string]any{"op": "createKey", "args": rec})
		lim := in.Limit
		k := f.add(in.Name, &lim, in.LimitReset, 0, in.ExpiresAt)
		f.values[k.Hash] = "sk-or-v1-" + k.Hash
		send(w, 201, map[string]any{"data": k, "key": f.values[k.Hash]})
	case isKey && f.keys[hash] == nil:
		send(w, 404, map[string]any{"error": map[string]any{"code": 404, "message": "Resource not found"}})
	case isKey && r.Method == http.MethodGet:
		send(w, 200, map[string]any{"data": f.keys[hash]})
	case isKey && r.Method == http.MethodPatch:
		var ch map[string]any
		_ = json.NewDecoder(r.Body).Decode(&ch)
		args := map[string]any{"hash": hash}
		for k, v := range ch {
			args[k] = v
		}
		f.writes = append(f.writes, map[string]any{"op": "updateKey", "args": args})
		k := f.keys[hash]
		if v, ok := ch["limit"].(float64); ok {
			k.Limit = &v
		}
		if v, ok := ch["disabled"].(bool); ok {
			k.Disabled = v
		}
		if v, ok := ch["limit_reset"].(string); ok {
			k.LimitReset = &v
		}
		send(w, 200, map[string]any{"data": k})
	default:
		send(w, 404, map[string]any{"error": map[string]any{"code": 404, "message": "Not found"}})
	}
}

func (f *fakeOpenRouter) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writes)
}
