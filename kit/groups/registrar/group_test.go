package registrar

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"google.golang.org/genai"

	"github.com/rubics-code/agent-kit/kit/group"
	"github.com/rubics-code/agent-kit/kit/groups/cloudflare"
	"github.com/rubics-code/agent-kit/kit/internal/kittest"
	"github.com/rubics-code/agent-kit/kit/policy"
	"github.com/rubics-code/agent-kit/kit/secrets"
)

var testPolicy = policy.Policy{Agent: "Example", Operator: "Sam", MaxKeyLimitUSD: 100}

func price(c string) *Pricing { return &Pricing{Currency: "USD", RegistrationCost: c, RenewalCost: c} }

// fakeRegistrar is the Registrar API in memory, for one account. It records
// every write.
type fakeRegistrar struct {
	*httptest.Server
	mu     sync.Mutex
	offers map[string]Offer
	// The states that a registration and then each status call give, in order.
	states []Status
	writes []map[string]any
}

func newFake(t *testing.T) *fakeRegistrar {
	f := &fakeRegistrar{
		offers: map[string]Offer{
			"exampledemo.com": {Name: "exampledemo.com", Registrable: true, Tier: "standard", Pricing: price("10.44")},
			"example.ai":      {Name: "example.ai", Registrable: false, Tier: "premium", Reason: "domain_premium"},
		},
		states: []Status{{State: "succeeded", Completed: true}},
	}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeRegistrar) client() *Client {
	return &Client{API: cloudflare.API{Token: "test-token", BaseURL: f.URL, HTTP: f.Client()}}
}

func (f *fakeRegistrar) next() Status {
	s := f.states[0]
	if len(f.states) > 1 {
		f.states = f.states[1:]
	}
	return s
}

func ok(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": v})
}

func (f *fakeRegistrar) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	const pre = "/accounts/acc1/registrar/"
	p := r.URL.Path
	switch {
	case p == "/accounts":
		ok(w, []any{map[string]any{"id": "acc1"}})
	case p == pre+"domain-search":
		out := []Offer{}
		for _, o := range f.offers {
			if strings.Contains(o.Name, strings.ToLower(r.URL.Query().Get("q"))) {
				out = append(out, o)
			}
		}
		ok(w, map[string]any{"domains": out})
	case p == pre+"domain-check":
		var body struct{ Domains []string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		out := []Offer{}
		for _, d := range body.Domains {
			o, found := f.offers[d]
			if !found {
				o = Offer{Name: d, Reason: "domain_unavailable"}
			}
			out = append(out, o)
		}
		ok(w, map[string]any{"domains": out})
	case p == pre+"registrations" && r.Method == http.MethodPost:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.writes = append(f.writes, body)
		w.WriteHeader(http.StatusAccepted)
		ok(w, f.next())
	case strings.HasPrefix(p, pre+"registrations/") && strings.HasSuffix(p, "/registration-status"):
		ok(w, f.next())
	default:
		w.WriteHeader(404)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []any{map[string]any{"code": 7000, "message": "No route"}}})
	}
}

func (f *fakeRegistrar) writeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writes)
}

func setup(t *testing.T, f *fakeRegistrar, script ...*genai.Content) *kittest.Harness {
	t.Helper()
	asm, err := group.Assemble([]group.Group{Group}, group.Services{"registrar": f.client()}, secrets.None, testPolicy,
		group.Enable{Has: func(string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	return kittest.New(t, kittest.Script(script...), asm.Tools, kittest.Config{})
}

func register(d string) *genai.Content {
	return kittest.Call(kittest.Fn("r1", "cloudflare_register_domain", map[string]any{"domain": d}))
}

func approveAndRun(t *testing.T, f *fakeRegistrar) string {
	t.Helper()
	h := setup(t, f, register("exampledemo.com"), kittest.Text("done"))
	cs := kittest.Confirmations(h.Say("buy it"))
	if len(cs) != 1 {
		t.Fatalf("%d confirmations", len(cs))
	}
	return kittest.AllText(h.Approve(cs[0]))
}

// Cloudflare Registrar tools

func TestSearchesWithNoApproval(t *testing.T) {
	h := setup(t, newFake(t), kittest.Call(kittest.Fn("s1", "cloudflare_search_domains", map[string]any{"query": "example"})), kittest.Text("found"))
	evs := h.Say("find")
	if len(kittest.Confirmations(evs)) != 0 || !strings.Contains(kittest.AllText(evs), "10.44") {
		t.Fatal(kittest.AllText(evs))
	}
}

func TestStopsBeforeARegistrationAndShowsTheLivePrice(t *testing.T) {
	f := newFake(t)
	h := setup(t, f, register("exampledemo.com"), kittest.Text("ok"))
	cs := kittest.Confirmations(h.Say("buy"))
	if len(cs) != 1 || !strings.Contains(kittest.Summary(t, cs[0]), "10.44 USD now, renewal 10.44 USD") || f.writeCount() != 0 {
		t.Fatalf("%v, %d writes", cs, f.writeCount())
	}
}

func TestRegistersOnceAfterApproval(t *testing.T) {
	f := newFake(t)
	out := approveAndRun(t, f)
	want := []map[string]any{{"domain_name": "exampledemo.com", "years": float64(1), "auto_renew": false, "privacy_mode": "redaction"}}
	if !reflect.DeepEqual(f.writes, want) || !strings.Contains(out, "Registered exampledemo.com") {
		t.Fatalf("%v\n%s", f.writes, out)
	}
}

func TestDoesNotRegisterWhenThePriceChangesAfterApproval(t *testing.T) {
	f := newFake(t)
	h := setup(t, f, register("exampledemo.com"), kittest.Text("ok"))
	cs := kittest.Confirmations(h.Say("buy"))
	f.mu.Lock()
	f.offers["exampledemo.com"] = Offer{Name: "exampledemo.com", Registrable: true, Tier: "standard", Pricing: price("99.00")}
	f.mu.Unlock()
	out := kittest.AllText(h.Approve(cs[0]))
	if f.writeCount() != 0 || !strings.Contains(out, "does not match these arguments") {
		t.Fatalf("%d writes\n%s", f.writeCount(), out)
	}
}

func TestDoesNotRegisterWhenTheOperatorRejects(t *testing.T) {
	f := newFake(t)
	h := setup(t, f, register("exampledemo.com"), kittest.Text("ok"))
	cs := kittest.Confirmations(h.Say("buy"))
	h.Reject(cs[0], "no")
	if f.writeCount() != 0 {
		t.Fatal(f.writes)
	}
}

func TestSendsPremiumAndDashboardOnlyDomainsToTheDashboardWithNoApproval(t *testing.T) {
	for _, d := range []string{"example.ai", "example.sh"} {
		f := newFake(t)
		h := setup(t, f, register(d), kittest.Text("ok"))
		evs := h.Say("buy")
		if len(kittest.Confirmations(evs)) != 0 || !strings.Contains(kittest.AllText(evs), "Cloudflare dashboard") || f.writeCount() != 0 {
			t.Fatalf("%s: %s", d, kittest.AllText(evs))
		}
	}
}

func TestPollsAPendingRegistrationUntilItFinishes(t *testing.T) {
	f := newFake(t)
	var done Status
	_ = json.Unmarshal([]byte(`{"state":"succeeded","completed":true,"context":{"registration":{"expires_at":"2027-11-27"}}}`), &done)
	f.states = []Status{{State: "pending"}, {State: "in_progress"}, done}
	if out := approveAndRun(t, f); !strings.Contains(out, "expires 2027-11-27") {
		t.Fatal(out)
	}
}

func TestReportsActionRequiredAndSaysNotToRegisterAgain(t *testing.T) {
	f := newFake(t)
	var s Status
	_ = json.Unmarshal([]byte(`{"state":"action_required","completed":false,"context":{"action":"registrant_email_confirmation_pending"}}`), &s)
	f.states = []Status{s}
	out := approveAndRun(t, f)
	if !strings.Contains(out, "registrant_email_confirmation_pending") || !strings.Contains(out, "Do not register again") {
		t.Fatal(out)
	}
}

func TestSaysWhenADomainIsNotAvailable(t *testing.T) {
	f := newFake(t)
	h := setup(t, f, register("taken.com"), kittest.Text("ok"))
	evs := h.Say("buy")
	if len(kittest.Confirmations(evs)) != 0 || !strings.Contains(kittest.AllText(evs), "taken.com is not available (domain_unavailable)") {
		t.Fatal(kittest.AllText(evs))
	}
}

// The client

func TestFindsTheOneAccountAndSendsAOneYearRegistrationWithNoAutoRenew(t *testing.T) {
	type call struct {
		Method, Path string
		Body         map[string]any
	}
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := call{Method: r.Method, Path: r.URL.Path}
		_ = json.NewDecoder(r.Body).Decode(&c.Body)
		calls = append(calls, c)
		if r.URL.Path == "/accounts" {
			ok(w, []any{map[string]any{"id": "acc1"}})
			return
		}
		w.WriteHeader(http.StatusAccepted)
		ok(w, map[string]any{"state": "pending", "completed": false})
	}))
	defer srv.Close()
	reg := &Client{API: cloudflare.API{Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}}
	if _, err := reg.Register(t.Context(), "exampledemo.com"); err != nil {
		t.Fatal(err)
	}
	last := calls[len(calls)-1]
	want := call{Method: "POST", Path: "/accounts/acc1/registrar/registrations",
		Body: map[string]any{"domain_name": "exampledemo.com", "years": float64(1), "auto_renew": false, "privacy_mode": "redaction"}}
	if !reflect.DeepEqual(last, want) {
		t.Fatalf("%+v", last)
	}
}

func TestRefusesATokenThatSeesMoreThanOneAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok(w, []any{map[string]any{"id": "a"}, map[string]any{"id": "b"}})
	}))
	defer srv.Close()
	reg := &Client{API: cloudflare.API{Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}}
	if _, err := reg.Search(t.Context(), "x", 20); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatal(err)
	}
}
