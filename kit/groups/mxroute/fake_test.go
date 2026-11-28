package mxroute

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeMX is the MXroute API in memory, for one account. It records every
// write, with no password.
type fakeMX struct {
	*httptest.Server
	mu        sync.Mutex
	domains   map[string]*domainData
	passwords map[string]string // address to password
	writes    []map[string]any
}

type domainData struct {
	accounts   []Account
	forwarders []Forwarder
}

func newFake(t *testing.T) *fakeMX {
	f := &fakeMX{domains: map[string]*domainData{
		"example.com": {accounts: []Account{
			{Username: "ada", Email: "ada@example.com", Quota: 1024, Limit: 9600},
			{Username: "lin", Email: "lin@example.com", Quota: 1024, Limit: 9600},
		}},
	}, passwords: map[string]string{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeMX) client() *Client {
	return &Client{Server: "mail.example.net", Username: "account", APIKey: "test-api-key", BaseURL: f.URL, HTTP: f.Client()}
}

func ok(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
}

func fail(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "error": map[string]any{"code": code, "message": msg}})
}

func (f *fakeMX) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("X-API-Key") != "test-api-key" || r.Header.Get("X-Server") == "" || r.Header.Get("X-Username") == "" {
		fail(w, 401, "UNAUTHORIZED", "bad credentials")
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.URL.Path == "/domains" && r.Method == http.MethodGet:
		out := []string{}
		for d := range f.domains {
			out = append(out, d)
		}
		ok(w, 200, out)
	case r.URL.Path == "/domains" && r.Method == http.MethodPost:
		d, _ := body["domain"].(string)
		f.writes = append(f.writes, map[string]any{"op": "addDomain", "domain": d})
		if f.domains[d] != nil {
			fail(w, 409, "CONFLICT", d+" already exists")
			return
		}
		f.domains[d] = &domainData{}
		ok(w, 201, map[string]any{"domain": d})
	case r.URL.Path == "/verification-key":
		key := "_da-verify-00000000000000000000000000000000"
		ok(w, 200, map[string]any{"key": key, "record": map[string]any{"type": "TXT", "name": key, "value": "domain-verified"}})
	case len(parts) >= 3 && parts[0] == "domains":
		dom := f.domains[parts[1]]
		if dom == nil {
			fail(w, 404, "NOT_FOUND", "domain "+parts[1]+" not found")
			return
		}
		f.domain(w, r, parts[1], dom, parts[2:], body)
	default:
		fail(w, 404, "NOT_FOUND", "no route")
	}
}

func (f *fakeMX) domain(w http.ResponseWriter, r *http.Request, name string, dom *domainData, rest []string, body map[string]any) {
	switch {
	case rest[0] == "dns":
		ok(w, 200, map[string]any{
			"mx_records":   []any{map[string]any{"priority": 10, "hostname": "mail.example.net"}, map[string]any{"priority": 20, "hostname": "relay.example.net"}},
			"spf":          map[string]any{"type": "TXT", "name": "@", "value": "v=spf1 include:mail.example.net -all"},
			"dkim":         map[string]any{"type": "TXT", "name": "x._domainkey", "value": "v=DKIM1; k=rsa; p=MIIBFAKEKEY"},
			"verification": nil,
		})
	case rest[0] == "email-accounts" && len(rest) == 1 && r.Method == http.MethodGet:
		ok(w, 200, dom.accounts)
	case rest[0] == "email-accounts" && len(rest) == 1 && r.Method == http.MethodPost:
		u, _ := body["username"].(string)
		q, _ := body["quota"].(float64)
		l, _ := body["limit"].(float64)
		f.writes = append(f.writes, map[string]any{"op": "createEmailAccount", "domain": name, "username": u, "quota": q, "limit": l})
		for _, a := range dom.accounts {
			if a.Username == u {
				fail(w, 409, "CONFLICT", u+"@"+name+" already exists")
				return
			}
		}
		dom.accounts = append(dom.accounts, Account{Username: u, Email: u + "@" + name, Quota: int(q), Limit: int(l)})
		f.passwords[u+"@"+name], _ = body["password"].(string)
		ok(w, 201, map[string]any{"username": u})
	case rest[0] == "email-accounts" && len(rest) == 2 && r.Method == http.MethodPatch:
		f.writes = append(f.writes, map[string]any{"op": "setEmailPassword", "domain": name, "username": rest[1]})
		found := false
		for _, a := range dom.accounts {
			found = found || a.Username == rest[1]
		}
		if !found {
			fail(w, 404, "NOT_FOUND", rest[1]+"@"+name+" not found")
			return
		}
		f.passwords[rest[1]+"@"+name], _ = body["password"].(string)
		w.WriteHeader(http.StatusNoContent)
	case rest[0] == "forwarders" && r.Method == http.MethodGet:
		ok(w, 200, dom.forwarders)
	case rest[0] == "forwarders" && r.Method == http.MethodPost:
		alias, _ := body["alias"].(string)
		var to []string
		for _, x := range body["destinations"].([]any) {
			to = append(to, x.(string))
		}
		f.writes = append(f.writes, map[string]any{"op": "createForwarder", "domain": name, "alias": alias, "destinations": to})
		dom.forwarders = append(dom.forwarders, Forwarder{Alias: alias, Email: alias + "@" + name, Destinations: to})
		ok(w, 201, map[string]any{"alias": alias})
	default:
		fail(w, 404, "NOT_FOUND", "no route")
	}
}

func (f *fakeMX) ops() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, w := range f.writes {
		out = append(out, w["op"].(string))
	}
	return out
}

func (f *fakeMX) password(addr string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.passwords[addr]
}
