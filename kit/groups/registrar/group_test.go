package registrar

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/rubics-code/agent-kit/kit/fakes"
	"github.com/rubics-code/agent-kit/kit/group"
	"github.com/rubics-code/agent-kit/kit/groups/cloudflare"
	"github.com/rubics-code/agent-kit/kit/internal/kittest"
	"github.com/rubics-code/agent-kit/kit/policy"
	"github.com/rubics-code/agent-kit/kit/secrets"
)

var testPolicy = policy.Policy{Agent: "Example", Operator: "Sam", MaxKeyLimitUSD: 100}

func newFake(t *testing.T) *fakes.Cloudflare {
	f := fakes.NewCloudflare(t, nil)
	f.SetOffer(fakes.Price("exampledemo.com", "10.44"))
	f.SetOffer(fakes.Offer{Name: "example.ai", Tier: "premium", Reason: "domain_premium"})
	return f
}

func client(f *fakes.Cloudflare) *Client {
	return &Client{API: cloudflare.API{Token: fakes.CloudflareToken, BaseURL: f.URL, HTTP: f.Client()}}
}

func ok(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": v})
}

func setup(t *testing.T, f *fakes.Cloudflare, script ...*genai.Content) *kittest.Harness {
	t.Helper()
	asm, err := group.Assemble([]group.Group{Group}, group.Services{"registrar": client(f)}, secrets.None, testPolicy,
		group.Enable{Has: func(string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	return kittest.New(t, kittest.Script(script...), asm.Tools, kittest.Config{})
}

func register(d string) *genai.Content {
	return kittest.Call(kittest.Fn("r1", "cloudflare_register_domain", map[string]any{"domain": d}))
}

func approveAndRun(t *testing.T, f *fakes.Cloudflare) string {
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
	if len(cs) != 1 || !strings.Contains(kittest.Summary(t, cs[0]), "10.44 USD now, renewal 10.44 USD") || len(f.Writes()) != 0 {
		t.Fatalf("%v, %d writes", cs, len(f.Writes()))
	}
}

func TestRegistersOnceAfterApproval(t *testing.T) {
	f := newFake(t)
	out := approveAndRun(t, f)
	want := []fakes.Write{{Op: "register", Args: map[string]any{"domain_name": "exampledemo.com", "years": float64(1), "auto_renew": false, "privacy_mode": "redaction"}}}
	if !reflect.DeepEqual(f.Writes(), want) || !strings.Contains(out, "Registered exampledemo.com") {
		t.Fatalf("%v\n%s", f.Writes(), out)
	}
}

func TestDoesNotRegisterWhenThePriceChangesAfterApproval(t *testing.T) {
	f := newFake(t)
	h := setup(t, f, register("exampledemo.com"), kittest.Text("ok"))
	cs := kittest.Confirmations(h.Say("buy"))
	f.SetOffer(fakes.Price("exampledemo.com", "99.00"))
	out := kittest.AllText(h.Approve(cs[0]))
	if len(f.Writes()) != 0 || !strings.Contains(out, "does not match these arguments") {
		t.Fatalf("%d writes\n%s", len(f.Writes()), out)
	}
}

func TestDoesNotRegisterWhenTheOperatorRejects(t *testing.T) {
	f := newFake(t)
	h := setup(t, f, register("exampledemo.com"), kittest.Text("ok"))
	cs := kittest.Confirmations(h.Say("buy"))
	h.Reject(cs[0], "no")
	if len(f.Writes()) != 0 {
		t.Fatal(f.Writes())
	}
}

func TestSendsPremiumAndDashboardOnlyDomainsToTheDashboardWithNoApproval(t *testing.T) {
	for _, d := range []string{"example.ai", "example.sh"} {
		f := newFake(t)
		h := setup(t, f, register(d), kittest.Text("ok"))
		evs := h.Say("buy")
		if len(kittest.Confirmations(evs)) != 0 || !strings.Contains(kittest.AllText(evs), "Cloudflare dashboard") || len(f.Writes()) != 0 {
			t.Fatalf("%s: %s", d, kittest.AllText(evs))
		}
	}
}

func TestPollsAPendingRegistrationUntilItFinishes(t *testing.T) {
	f := newFake(t)
	f.SetStates(map[string]any{"state": "pending"}, map[string]any{"state": "in_progress"},
		map[string]any{"state": "succeeded", "completed": true, "context": map[string]any{"registration": map[string]any{"expires_at": "2027-11-27"}}})
	if out := approveAndRun(t, f); !strings.Contains(out, "expires 2027-11-27") {
		t.Fatal(out)
	}
}

func TestReportsActionRequiredAndSaysNotToRegisterAgain(t *testing.T) {
	f := newFake(t)
	f.SetStates(map[string]any{"state": "action_required", "context": map[string]any{"action": "registrant_email_confirmation_pending"}})
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
