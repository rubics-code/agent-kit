package fakes

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// OpenRouterKey is the management key that the OpenRouter fake accepts.
const OpenRouterKey = "test-openrouter-management-key"

// Key is an API key in the OpenRouter fake.
type Key struct {
	Hash           string   `json:"hash"`
	Name           string   `json:"name"`
	Label          string   `json:"label"`
	Disabled       bool     `json:"disabled"`
	Limit          *float64 `json:"limit"`
	LimitRemaining *float64 `json:"limit_remaining"`
	LimitReset     *string  `json:"limit_reset"`
	Usage          float64  `json:"usage"`
	UsageMonthly   float64  `json:"usage_monthly"`
	CreatedAt      string   `json:"created_at"`
	LastUsedAt     *string  `json:"last_used_at"`
	ExpiresAt      *string  `json:"expires_at"`
}

// SeedKey is a key that a test starts with.
type SeedKey struct {
	Name         string
	Limit        *float64
	UsageMonthly float64
	Disabled     bool
}

// OpenRouter is the OpenRouter key management API.
type OpenRouter struct {
	*httptest.Server
	log
	keys   map[string]*Key
	order  []string
	values map[string]string
	next   int
}

// USD gives a pointer to an amount, for a seed.
func USD(n float64) *float64 { return &n }

// NewOpenRouter starts a fake with these keys. The hash of the first key is
// 63 zeros and a 1.
func NewOpenRouter(t testing.TB, seed ...SeedKey) *OpenRouter {
	f := &OpenRouter{keys: map[string]*Key{}, values: map[string]string{}}
	for _, s := range seed {
		k := f.addKey(s.Name, s.Limit, "monthly", s.UsageMonthly, "")
		k.Disabled = s.Disabled
	}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

// Values gives the values of the keys that the fake made, by hash.
func (f *OpenRouter) Values() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for k, v := range f.values {
		out[k] = v
	}
	return out
}

func (f *OpenRouter) addKey(name string, limit *float64, reset string, usage float64, expires string) *Key {
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

func orFail(w http.ResponseWriter, status int, msg string) {
	send(w, status, map[string]any{"error": map[string]any{"code": status, "message": msg}})
}

func (f *OpenRouter) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+OpenRouterKey {
		orFail(w, 401, "No auth credentials found")
		return
	}
	body := decode(r)
	hash, isKey := strings.CutPrefix(r.URL.Path, "/keys/")
	switch {
	case r.URL.Path == "/credits":
		send(w, 200, map[string]any{"data": map[string]any{"total_credits": 100, "total_usage": 12.5}})
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
		f.add("createKey", body)
		name, _ := body["name"].(string)
		reset, _ := body["limit_reset"].(string)
		expires, _ := body["expires_at"].(string)
		var limit *float64
		if v, ok := body["limit"].(float64); ok {
			limit = &v
		}
		k := f.addKey(name, limit, reset, 0, expires)
		f.values[k.Hash] = "sk-or-v1-" + k.Hash
		send(w, 201, map[string]any{"data": k, "key": f.values[k.Hash]})
	case isKey && f.keys[hash] == nil:
		orFail(w, 404, "Resource not found")
	case isKey && r.Method == http.MethodGet:
		send(w, 200, map[string]any{"data": f.keys[hash]})
	case isKey && r.Method == http.MethodPatch:
		args := map[string]any{"hash": hash}
		for k, v := range body {
			args[k] = v
		}
		f.add("updateKey", args)
		k := f.keys[hash]
		if v, ok := body["limit"].(float64); ok {
			k.Limit = &v
		}
		if v, ok := body["disabled"].(bool); ok {
			k.Disabled = v
		}
		if v, ok := body["limit_reset"].(string); ok {
			k.LimitReset = &v
		}
		send(w, 200, map[string]any{"data": k})
	default:
		orFail(w, 404, "Not found")
	}
}
