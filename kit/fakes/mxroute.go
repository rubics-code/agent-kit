package fakes

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// MXrouteKey is the API key that the MXroute fake accepts.
const MXrouteKey = "test-mxroute-api-key"

// Mailbox is a mailbox in the MXroute fake.
type Mailbox struct {
	Username  string `json:"username"`
	Email     string `json:"email"`
	Quota     int    `json:"quota"`
	Usage     int    `json:"usage"`
	Limit     int    `json:"limit"`
	Sent      int    `json:"sent"`
	Suspended bool   `json:"suspended"`
}

// Forwarder is a forwarder in the MXroute fake.
type Forwarder struct {
	Alias        string   `json:"alias"`
	Email        string   `json:"email"`
	Destinations []string `json:"destinations"`
}

type mxDomain struct {
	mailboxes  []Mailbox
	forwarders []Forwarder
}

// MXroute is the MXroute REST API, for one account.
type MXroute struct {
	*httptest.Server
	log
	domains   map[string]*mxDomain
	passwords map[string]string
	// DNS gives the records of a domain. The default gives neutral sample
	// records. An eval can replace it, for example to put text in a record.
	DNS func(domain string) map[string]any
	// VerificationKey is the account's verification key.
	VerificationKey string
}

// SampleDNS is the default DNS answer.
func SampleDNS(string) map[string]any {
	return map[string]any{
		"mx_records":   []any{map[string]any{"priority": 10, "hostname": "mail.example.net"}, map[string]any{"priority": 20, "hostname": "relay.example.net"}},
		"spf":          map[string]any{"type": "TXT", "name": "@", "value": "v=spf1 include:mail.example.net -all"},
		"dkim":         map[string]any{"type": "TXT", "name": "x._domainkey", "value": "v=DKIM1; k=rsa; p=MIIBFAKEKEY"},
		"verification": nil,
	}
}

// NewMXroute starts a fake with these mailboxes, by domain.
func NewMXroute(t testing.TB, mailboxes map[string][]string) *MXroute {
	f := &MXroute{domains: map[string]*mxDomain{}, passwords: map[string]string{}, DNS: SampleDNS,
		VerificationKey: "_da-verify-00000000000000000000000000000000"}
	for d, users := range mailboxes {
		f.domains[d] = &mxDomain{}
		for _, u := range users {
			f.domains[d].mailboxes = append(f.domains[d].mailboxes, Mailbox{Username: u, Email: u + "@" + d, Quota: 1024, Limit: 9600})
		}
	}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

// Password gives the password of a mailbox, for a test that checks where it went.
func (f *MXroute) Password(addr string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.passwords[addr]
}

func mxOK(w http.ResponseWriter, status int, data any) {
	send(w, status, map[string]any{"success": true, "data": data})
}

func mxFail(w http.ResponseWriter, status int, code, msg string) {
	send(w, status, map[string]any{"success": false, "error": map[string]any{"code": code, "message": msg}})
}

func (f *MXroute) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("X-API-Key") != MXrouteKey || r.Header.Get("X-Server") == "" || r.Header.Get("X-Username") == "" {
		mxFail(w, 401, "UNAUTHORIZED", "bad credentials")
		return
	}
	body := decode(r)
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case r.URL.Path == "/domains" && r.Method == http.MethodGet:
		out := []string{}
		for d := range f.domains {
			out = append(out, d)
		}
		slices.Sort(out)
		mxOK(w, 200, out)
	case r.URL.Path == "/domains" && r.Method == http.MethodPost:
		d, _ := body["domain"].(string)
		f.add("addDomain", map[string]any{"domain": d})
		if f.domains[d] != nil {
			mxFail(w, 409, "CONFLICT", d+" already exists")
			return
		}
		f.domains[d] = &mxDomain{}
		mxOK(w, 201, map[string]any{"domain": d})
	case r.URL.Path == "/verification-key":
		mxOK(w, 200, map[string]any{"key": f.VerificationKey, "record": map[string]any{"type": "TXT", "name": f.VerificationKey, "value": "domain-verified"}})
	case len(parts) >= 3 && parts[0] == "domains":
		dom := f.domains[parts[1]]
		if dom == nil {
			mxFail(w, 404, "NOT_FOUND", "domain "+parts[1]+" not found")
			return
		}
		f.domain(w, r, parts[1], dom, parts[2:], body)
	default:
		mxFail(w, 404, "NOT_FOUND", "no route")
	}
}

func (f *MXroute) domain(w http.ResponseWriter, r *http.Request, name string, dom *mxDomain, rest []string, body map[string]any) {
	switch {
	case rest[0] == "dns":
		mxOK(w, 200, f.DNS(name))
	case rest[0] == "email-accounts" && len(rest) == 1 && r.Method == http.MethodGet:
		mxOK(w, 200, dom.mailboxes)
	case rest[0] == "email-accounts" && len(rest) == 1 && r.Method == http.MethodPost:
		u, _ := body["username"].(string)
		q, _ := body["quota"].(float64)
		l, _ := body["limit"].(float64)
		f.add("createEmailAccount", map[string]any{"domain": name, "username": u, "quota": q, "limit": l})
		for _, a := range dom.mailboxes {
			if a.Username == u {
				mxFail(w, 409, "CONFLICT", u+"@"+name+" already exists")
				return
			}
		}
		dom.mailboxes = append(dom.mailboxes, Mailbox{Username: u, Email: u + "@" + name, Quota: int(q), Limit: int(l)})
		f.passwords[u+"@"+name], _ = body["password"].(string)
		mxOK(w, 201, map[string]any{"username": u})
	case rest[0] == "email-accounts" && len(rest) == 2 && r.Method == http.MethodPatch:
		f.add("setEmailPassword", map[string]any{"domain": name, "username": rest[1]})
		if !slices.ContainsFunc(dom.mailboxes, func(a Mailbox) bool { return a.Username == rest[1] }) {
			mxFail(w, 404, "NOT_FOUND", rest[1]+"@"+name+" not found")
			return
		}
		f.passwords[rest[1]+"@"+name], _ = body["password"].(string)
		w.WriteHeader(http.StatusNoContent)
	case rest[0] == "forwarders" && r.Method == http.MethodGet:
		mxOK(w, 200, dom.forwarders)
	case rest[0] == "forwarders" && r.Method == http.MethodPost:
		alias, _ := body["alias"].(string)
		to := []string{}
		ds, _ := body["destinations"].([]any)
		for _, x := range ds {
			s, _ := x.(string)
			to = append(to, s)
		}
		f.add("createForwarder", map[string]any{"domain": name, "alias": alias, "destinations": to})
		dom.forwarders = append(dom.forwarders, Forwarder{Alias: alias, Email: alias + "@" + name, Destinations: to})
		mxOK(w, 201, map[string]any{"alias": alias})
	default:
		mxFail(w, 404, "NOT_FOUND", "no route")
	}
}
