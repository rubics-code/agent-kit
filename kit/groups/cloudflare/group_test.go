package cloudflare

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/rubics-code/agent-kit/kit/fakes"
	"github.com/rubics-code/agent-kit/kit/group"
	"github.com/rubics-code/agent-kit/kit/internal/kittest"
	"github.com/rubics-code/agent-kit/kit/policy"
	"github.com/rubics-code/agent-kit/kit/secrets"
)

var testPolicy = policy.Policy{Agent: "Example", Operator: "Sam", MaxKeyLimitUSD: 100}

func prio(n int) *int { return &n }

func client(f *fakes.Cloudflare) *DNS {
	return &DNS{API: API{Token: fakes.CloudflareToken, BaseURL: f.URL, HTTP: f.Client()}}
}

func seeded(t *testing.T) *fakes.Cloudflare {
	return fakes.NewCloudflare(t, map[string][]fakes.Record{
		"example.org": {
			{Type: "A", Name: "example.org", Content: "192.0.2.1"},
			{Type: "CNAME", Name: "www.example.org", Content: "example.org"},
		},
		"example.com": {{Type: "TXT", Name: "example.com", Content: `"v=spf1 include:_spf.example.net -all"`}},
	})
}

func setup(t *testing.T, f *fakes.Cloudflare, script ...*genai.Content) *kittest.Harness {
	t.Helper()
	asm, err := group.Assemble([]group.Group{Group}, group.Services{"cloudflare": client(f)}, secrets.None, testPolicy,
		group.Enable{Has: func(string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	return kittest.New(t, kittest.Script(script...), asm.Tools, kittest.Config{})
}

func create(args map[string]any) *genai.Content {
	return kittest.Call(kittest.Fn("c1", "cloudflare_create_dns_record", args))
}

var verification = map[string]any{"domain": "example.org", "type": "TXT", "name": "_da-verify-abc", "content": "domain-verified"}

func with(base map[string]any, kv ...any) map[string]any {
	out := map[string]any{}
	for k, v := range base {
		out[k] = v
	}
	for i := 0; i < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

// Cloudflare DNS tools

func TestListsRecordsWithNoApproval(t *testing.T) {
	f := seeded(t)
	h := setup(t, f, kittest.Call(kittest.Fn("l1", "cloudflare_list_dns_records", map[string]any{"domain": "example.org", "type": "CNAME"})), kittest.Text("one CNAME"))
	evs := h.Say("records?")
	all := kittest.AllText(evs)
	if len(kittest.Confirmations(evs)) != 0 || !strings.Contains(all, "www.example.org") || strings.Contains(all, "192.0.2.1") {
		t.Fatal(all)
	}
}

func TestStopsBeforeAWriteAndShowsTheRecordAndWhatIsAtThatNameNow(t *testing.T) {
	f := seeded(t)
	h := setup(t, f, create(verification), kittest.Text("done"))
	cs := kittest.Confirmations(h.Say("add it"))
	if len(cs) != 1 {
		t.Fatalf("%d confirmations", len(cs))
	}
	want := "Create in Cloudflare (zone example.org): TXT _da-verify-abc.example.org domain-verified. Records at that name now: none"
	if got := kittest.Summary(t, cs[0]); got != want || len(f.Writes()) != 0 {
		t.Fatalf("%q, %d writes", got, len(f.Writes()))
	}
}

func TestWritesOnceAfterApprovalWithTheAgentsComment(t *testing.T) {
	f := seeded(t)
	mx := map[string]any{"domain": "example.org", "type": "MX", "name": "@", "content": "mail.example.net", "priority": 10}
	h := setup(t, f, create(mx), kittest.Text("done"))
	cs := kittest.Confirmations(h.Say("add MX"))
	if s := kittest.Summary(t, cs[0]); !strings.Contains(s, "Records at that name now: A example.org 192.0.2.1") {
		t.Fatal(s)
	}
	evs := h.Approve(cs[0])
	want := []fakes.Write{{Op: "createRecord", Args: map[string]any{"zoneId": "zone-example.org", "type": "MX", "name": "example.org", "content": "mail.example.net",
		"priority": float64(10), "comment": "Created by Example, approved by Sam", "ttl": float64(1)}}}
	if !reflect.DeepEqual(f.Writes(), want) {
		t.Fatalf("%v", f.Writes())
	}
	if r := kittest.Responses(evs)["cloudflare_create_dns_record"]; r[len(r)-1]["result"] != "Created MX example.org 10 mail.example.net." {
		t.Fatalf("%v", r)
	}
}

func TestDoesNotWriteWhenTheOperatorRejects(t *testing.T) {
	f := seeded(t)
	h := setup(t, f, create(verification), kittest.Text("ok"))
	cs := kittest.Confirmations(h.Say("add it"))
	evs := h.Reject(cs[0], "no")
	if len(f.Writes()) != 0 || !strings.Contains(kittest.AllText(evs), "Not done: Sam did not approve (no)") {
		t.Fatalf("%d writes", len(f.Writes()))
	}
}

func noApproval(t *testing.T, f *fakes.Cloudflare, args map[string]any, want string) {
	t.Helper()
	h := setup(t, f, create(args), kittest.Text("ok"))
	evs := h.Say("add it")
	all := kittest.AllText(evs)
	if len(kittest.Confirmations(evs)) != 0 || len(f.Writes()) != 0 || !strings.Contains(all, want) {
		t.Fatalf("want %q in %s", want, all)
	}
}

func TestAsksNothingWhenTheSameRecordIsAlreadyThere(t *testing.T) {
	noApproval(t, seeded(t), map[string]any{"domain": "example.com", "type": "TXT", "name": "@", "content": "v=spf1 include:_spf.example.net -all"}, "Already in Cloudflare")
}

func TestRefusesASecondSPFRecordAtAName(t *testing.T) {
	noApproval(t, seeded(t), map[string]any{"domain": "example.com", "type": "TXT", "name": "@", "content": "v=spf1 include:mail.example.net -all"}, "already has an SPF record")
}

func TestRefusesARecordAtACNAME(t *testing.T) {
	noApproval(t, seeded(t), map[string]any{"domain": "example.org", "type": "TXT", "name": "www", "content": "hello"}, "is a CNAME")
}

func TestRefusesARecordTypeThatTerraformOwnsBeforeAnyApproval(t *testing.T) {
	noApproval(t, seeded(t), map[string]any{"domain": "example.org", "type": "A", "name": "mail", "content": "192.0.2.9"}, `"error"`)
}

func TestRefusesAnMXRecordWithNoPriorityBeforeAnyApproval(t *testing.T) {
	noApproval(t, seeded(t), map[string]any{"domain": "example.org", "type": "MX", "name": "@", "content": "mail.example.net"}, "give a priority for an MX record")
}

func TestSaysSoWhenNoZoneHoldsTheDomain(t *testing.T) {
	noApproval(t, seeded(t), with(verification, "domain", "elsewhere.net"), "Cloudflare has no zone for elsewhere.net")
}

func TestRefusesADomainThatIsNotLowercase(t *testing.T) {
	noApproval(t, seeded(t), with(verification, "domain", "Example.org"), `"error"`)
}

func TestUsesTheKitsHTTPClient(t *testing.T) {
	f := seeded(t)
	var seen []string
	hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		seen = append(seen, r.URL.Path)
		return f.Client().Transport.RoundTrip(r)
	})}
	s := group.Connect([]group.Group{Group}, group.ConnectContext{
		Holds: func(k string) bool { return k == TokenKey }, Get: func(string) string { return fakes.CloudflareToken }, HTTPClient: hc,
	})
	dns := s["cloudflare"].(*DNS)
	dns.API.BaseURL = f.URL
	if z, err := dns.FindZone(t.Context(), "example.org"); err != nil || z == nil || len(seen) != 1 {
		t.Fatalf("%v %v %v", z, err, seen)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The client

type call struct {
	Method, URL, Auth string
	Body              map[string]any
}

func recorder(t *testing.T, handle func(r *http.Request) (int, any)) (*DNS, *[]call) {
	var calls []call
	srv := newRawServer(t, func(w http.ResponseWriter, r *http.Request) {
		c := call{Method: r.Method, URL: r.URL.RequestURI(), Auth: r.Header.Get("Authorization")}
		_ = json.NewDecoder(r.Body).Decode(&c.Body)
		calls = append(calls, c)
		status, body := handle(r)
		reply(w, status, body)
	})
	return &DNS{API: API{Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}}, &calls
}

func TestFindsTheZoneOfASubdomainByItsParent(t *testing.T) {
	dns, calls := recorder(t, func(r *http.Request) (int, any) {
		if r.URL.Query().Get("name") == "example.org" {
			return 200, map[string]any{"success": true, "result": []any{map[string]any{"id": "z1", "name": "example.org"}}}
		}
		return 200, map[string]any{"success": true, "result": []any{}}
	})
	z, err := dns.FindZone(t.Context(), "mail.example.org")
	if err != nil || *z != (Zone{ID: "z1", Name: "example.org"}) {
		t.Fatalf("%v %v", z, err)
	}
	c := *calls
	if len(c) != 2 || c[0].URL != "/zones?name=mail.example.org" || c[1].URL != "/zones?name=example.org" || c[0].Auth != "Bearer t" {
		t.Fatalf("%+v", c)
	}
}

func TestReadsEveryPageOfRecords(t *testing.T) {
	dns, _ := recorder(t, func(r *http.Request) (int, any) {
		page := r.URL.Query().Get("page")
		return 200, map[string]any{"success": true, "result": []any{map[string]any{"id": "r" + page, "type": "TXT", "name": "a.example.org", "content": "x", "ttl": 1}},
			"result_info": map[string]any{"page": len(page), "total_pages": 2}}
	})
	recs, err := dns.ListRecords(t.Context(), "z1")
	if err != nil || len(recs) != 2 || recs[0].ID != "r1" || recs[1].ID != "r2" {
		t.Fatalf("%+v %v", recs, err)
	}
}

func TestCreatesARecordWithAnAutomaticTTL(t *testing.T) {
	dns, calls := recorder(t, func(*http.Request) (int, any) {
		return 200, map[string]any{"success": true, "result": map[string]any{"id": "r9", "type": "TXT", "name": "a.example.org", "content": "x", "ttl": 1}}
	})
	if _, err := dns.CreateRecord(t.Context(), "z1", NewRecord{Type: "TXT", Name: "a.example.org", Content: "x", Comment: "c"}); err != nil {
		t.Fatal(err)
	}
	c := (*calls)[0]
	want := map[string]any{"type": "TXT", "name": "a.example.org", "content": "x", "comment": "c", "ttl": float64(1)}
	if c.Method != http.MethodPost || !reflect.DeepEqual(c.Body, want) {
		t.Fatalf("%+v", c)
	}
}

func TestTurnsACloudflareErrorIntoAnError(t *testing.T) {
	dns, _ := recorder(t, func(*http.Request) (int, any) {
		return 403, map[string]any{"success": false, "errors": []any{map[string]any{"code": 10000, "message": "Authentication error"}}}
	})
	_, err := dns.ListRecords(t.Context(), "z1")
	var ce *Error
	if !asError(err, &ce) || err.Error() != "Cloudflare 403 [10000]: Authentication error" {
		t.Fatalf("%v", err)
	}
}

func TestPutsNoTokenInAnError(t *testing.T) {
	dns, _ := recorder(t, func(*http.Request) (int, any) { return 500, "secret-echo" })
	dns.API.Token = "tok-should-not-leak"
	_, err := dns.ListRecords(t.Context(), "z1")
	if err == nil || strings.Contains(err.Error(), "tok-should-not-leak") {
		t.Fatalf("%v", err)
	}
}
