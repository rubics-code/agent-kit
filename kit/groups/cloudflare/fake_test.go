package cloudflare

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const testToken = "test-token"

// fakeDNS is the Cloudflare DNS API in memory. It records every write.
type fakeDNS struct {
	*httptest.Server
	mu     sync.Mutex
	zones  map[string][]Record // by zone name
	writes []map[string]any
	next   int
}

func newFakeDNS(t *testing.T, seed map[string][]Record) *fakeDNS {
	f := &fakeDNS{zones: map[string][]Record{}}
	for zone, recs := range seed {
		for _, r := range recs {
			f.next++
			r.ID = fmt.Sprintf("rec-%d", f.next)
			if r.TTL == 0 {
				r.TTL = 1
			}
			f.zones[zone] = append(f.zones[zone], r)
		}
	}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeDNS) client() *DNS {
	return &DNS{API: API{Token: testToken, BaseURL: f.URL, HTTP: f.Client()}}
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeDNS) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+testToken {
		reply(w, 403, map[string]any{"success": false, "errors": []any{map[string]any{"code": 10000, "message": "Authentication error"}}})
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/zones":
		name := r.URL.Query().Get("name")
		out := []Zone{}
		if _, ok := f.zones[name]; ok {
			out = append(out, Zone{ID: "zone-" + name, Name: name})
		}
		reply(w, 200, map[string]any{"success": true, "result": out})
	case len(parts) == 3 && parts[0] == "zones" && parts[2] == "dns_records":
		zone, ok := strings.CutPrefix(parts[1], "zone-")
		recs, found := f.zones[zone]
		if !ok || !found {
			reply(w, 404, map[string]any{"success": false, "errors": []any{map[string]any{"code": 7003, "message": "zone not found"}}})
			return
		}
		if r.Method == http.MethodGet {
			reply(w, 200, map[string]any{"success": true, "result": recs, "result_info": map[string]any{"page": 1, "total_pages": 1}})
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		body["zoneId"] = parts[1]
		f.writes = append(f.writes, body)
		var nr NewRecord
		b, _ := json.Marshal(body)
		_ = json.Unmarshal(b, &nr)
		for _, x := range recs {
			if x.Type == nr.Type && x.Name == nr.Name && x.Content == nr.Content {
				reply(w, 400, map[string]any{"success": false, "errors": []any{map[string]any{"code": 81058, "message": "An identical record already exists."}}})
				return
			}
		}
		f.next++
		rec := Record{ID: fmt.Sprintf("rec-%d", f.next), Type: nr.Type, Name: nr.Name, Content: nr.Content, Priority: nr.Priority, TTL: nr.TTL, Comment: nr.Comment}
		f.zones[zone] = append(recs, rec)
		reply(w, 200, map[string]any{"success": true, "result": rec})
	default:
		reply(w, 404, map[string]any{"success": false, "errors": []any{map[string]any{"code": 7000, "message": "No route"}}})
	}
}

func (f *fakeDNS) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writes)
}
