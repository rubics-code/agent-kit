package mxroute

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"

	"github.com/rubics-code/agent-kit/kit/fakes"
	"github.com/rubics-code/agent-kit/kit/group"
	"github.com/rubics-code/agent-kit/kit/groups/infisical"
	"github.com/rubics-code/agent-kit/kit/internal/kittest"
	"github.com/rubics-code/agent-kit/kit/policy"
	"github.com/rubics-code/agent-kit/kit/secrets"
)

func testPolicy(t *testing.T) policy.Policy {
	t.Helper()
	p, err := policy.Parse([]byte(`{"agent":"Example","operator":"Sam","maxKeyLimitUsd":100,"infisical":{
		"readable":["/example/**","/agents/**"],"writable":["/example/**","/agents/*/**"],
		"envs":[{"path":"/agents/**","env":"staging"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type sink struct {
	mu        sync.Mutex
	delivered []string
}

func (s *sink) Where() string { return "Sam's terminal" }

func (s *sink) Deliver(_ context.Context, _, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delivered = append(s.delivered, value)
	return nil
}

func newFake(t *testing.T) *fakes.MXroute {
	return fakes.NewMXroute(t, map[string][]string{"example.com": {"ada", "lin"}})
}

func client(f *fakes.MXroute) *Client {
	return &Client{Server: "mail.example.net", Username: "account", APIKey: fakes.MXrouteKey, BaseURL: f.URL, HTTP: f.Client()}
}

func setup(t *testing.T, f *fakes.MXroute, s secrets.Sink, inf *fakes.Infisical, script ...*genai.Content) *kittest.Harness {
	t.Helper()
	pol := testPolicy(t)
	services := group.Services{"mxroute": client(f)}
	if inf != nil {
		services[group.StoreGroupID] = inf.Client(pol.Infisical.EnvLabel)
	}
	asm, err := group.Assemble([]group.Group{Group, infisical.Group}, services, s, pol, group.Enable{Has: func(string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	return kittest.New(t, kittest.Script(script...), asm.Tools, kittest.Config{})
}

var createHello = kittest.Fn("c1", "mxroute_create_mailbox", map[string]any{"domain": "example.com", "username": "hello"})

// The approval gate, with the MXroute tools

func TestRunsAReadToolWithNoApproval(t *testing.T) {
	h := setup(t, newFake(t), &sink{}, nil, kittest.Call(kittest.Fn("r1", "mxroute_list_mailboxes", map[string]any{"domain": "example.com"})), kittest.Text("ada and lin"))
	evs := h.Say("who has a mailbox?")
	texts := kittest.Texts(evs)
	if len(kittest.Confirmations(evs)) != 0 || texts[len(texts)-1] != "ada and lin" || !strings.Contains(kittest.AllText(evs), "lin@example.com") {
		t.Fatal(kittest.AllText(evs))
	}
}

func TestStopsBeforeAWriteAndShowsASummaryMadeByCode(t *testing.T) {
	f := newFake(t)
	h := setup(t, f, &sink{}, nil, kittest.Call(createHello), kittest.Text("done"))
	cs := kittest.Confirmations(h.Say("make hello@"))
	p := kittest.Payload(t, cs[0])
	if p["tool"] != "mxroute_create_mailbox" || p["summary"] != "Create the mailbox hello@example.com (quota 1024 MB, send limit 9600 per day)" || len(f.Ops()) != 0 {
		t.Fatalf("%v %v", p, f.Ops())
	}
}

func TestWritesOnceAfterApprovalAndThePasswordNeverEntersTheConversation(t *testing.T) {
	f, s := newFake(t), &sink{}
	h := setup(t, f, s, nil, kittest.Call(createHello), kittest.Text("done"))
	cs := kittest.Confirmations(h.Say("make hello@"))
	all := kittest.AllText(h.Approve(cs[0]))
	want := []fakes.Write{{Op: "createEmailAccount", Args: map[string]any{"domain": "example.com", "username": "hello", "quota": float64(1024), "limit": float64(9600)}}}
	if !reflect.DeepEqual(f.Writes(), want) {
		t.Fatalf("%v", f.Writes())
	}
	pw := f.Password("hello@example.com")
	if len(s.delivered) != 1 || s.delivered[0] != pw || len(pw) != 24 || strings.Contains(all, pw) {
		t.Fatalf("%d delivered", len(s.delivered))
	}
	if !strings.Contains(all, "Created hello@example.com. It went directly to Sam's terminal. You do not have it.") {
		t.Fatal(all)
	}
}

func TestDoesNotWriteWhenTheOperatorRejects(t *testing.T) {
	f, s := newFake(t), &sink{}
	h := setup(t, f, s, nil, kittest.Call(createHello), kittest.Text("ok"))
	cs := kittest.Confirmations(h.Say("make hello@"))
	all := kittest.AllText(h.Reject(cs[0], "not now"))
	if len(f.Ops()) != 0 || len(s.delivered) != 0 || !strings.Contains(all, "Not done: Sam did not approve (not now). Do not try again unless Sam asks.") {
		t.Fatal(all)
	}
}

func TestListsTheDomainsWithNoArguments(t *testing.T) {
	h := setup(t, newFake(t), &sink{}, nil, kittest.Call(kittest.Fn("l1", "mxroute_list_domains", map[string]any{})), kittest.Text("ok"))
	r := kittest.Responses(h.Say("domains?"))["mxroute_list_domains"][0]
	if ds, _ := r["domains"].([]any); r["error"] != nil || len(ds) != 1 || ds[0] != "example.com" {
		t.Fatalf("%v", r)
	}
}

func TestDoesNotWriteWhenTheApprovalIsForOtherArguments(t *testing.T) {
	f := newFake(t)
	h := setup(t, f, &sink{}, nil, kittest.Call(createHello), kittest.Text("ok"))
	cs := kittest.Confirmations(h.Say("make hello@"))
	h.Send(kittest.Answer(true, map[string]map[string]any{cs[0].ID: {"argsHash": "sha256:" + strings.Repeat("0", 64)}}))
	if len(f.Ops()) != 0 {
		t.Fatal(f.Ops())
	}
}

func TestAsksForEachWriteWhenTheModelCallsTwoAtOnce(t *testing.T) {
	f := newFake(t)
	fwd := kittest.Fn("c2", "mxroute_create_forwarder", map[string]any{"domain": "example.com", "alias": "sales", "destinations": []any{"sam@example.org"}})
	h := setup(t, f, &sink{}, nil, kittest.Call(createHello, fwd), kittest.Text("done"))
	cs := kittest.Confirmations(h.Say("both"))
	if len(cs) != 2 {
		t.Fatalf("%d confirmations", len(cs))
	}
	answer := &genai.Content{Role: genai.RoleUser}
	for _, c := range cs {
		p := kittest.Payload(t, c)
		answer.Parts = append(answer.Parts, &genai.Part{FunctionResponse: &genai.FunctionResponse{ID: c.ID, Name: toolconfirmation.FunctionCallName,
			Response: map[string]any{"confirmed": p["tool"] == "mxroute_create_forwarder", "payload": map[string]any{"argsHash": p["argsHash"]}}}})
	}
	h.Send(answer)
	if got := f.Ops(); !reflect.DeepEqual(got, []string{"createForwarder"}) {
		t.Fatal(got)
	}
}

func TestRefusesArgumentsOutsideTheSchemaBeforeAnyApproval(t *testing.T) {
	f := newFake(t)
	for _, args := range []map[string]any{
		{"domain": "Not A Domain", "username": "x"},
		{"domain": "example.com", "username": "Hello"},
		{"domain": "example.com", "username": "x", "dailySendLimit": 10000},
	} {
		h := setup(t, f, &sink{}, nil, kittest.Call(kittest.Fn("bad", "mxroute_create_mailbox", args)), kittest.Text("sorry"))
		evs := h.Say("x")
		if len(kittest.Confirmations(evs)) != 0 || len(f.Ops()) != 0 {
			t.Fatalf("%v: %s", args, kittest.AllText(evs))
		}
	}
}

// The other write tools

func TestGivesTheVerificationRecordForTheDomain(t *testing.T) {
	h := setup(t, newFake(t), &sink{}, nil, kittest.Call(kittest.Fn("v", "mxroute_get_verification_record", map[string]any{"domain": "example.org"})), kittest.Text("ok"))
	r := kittest.Responses(h.Say("verify"))["mxroute_get_verification_record"][0]
	if r["name"] != "_da-verify-00000000000000000000000000000000.example.org" || r["value"] != "domain-verified" || r["type"] != "TXT" {
		t.Fatalf("%v", r)
	}
}

func TestAddsADomainAfterApproval(t *testing.T) {
	f := newFake(t)
	h := setup(t, f, &sink{}, nil, kittest.Call(kittest.Fn("a", "mxroute_add_domain", map[string]any{"domain": "example.org"})), kittest.Text("ok"))
	cs := kittest.Confirmations(h.Say("add it"))
	if s := kittest.Summary(t, cs[0]); s != "Add the domain example.org to MXroute" {
		t.Fatal(s)
	}
	all := kittest.AllText(h.Approve(cs[0]))
	if !reflect.DeepEqual(f.Ops(), []string{"addDomain"}) || !strings.Contains(all, "Next, get its DNS records") {
		t.Fatal(all)
	}
}

func TestResetsAPasswordAfterApprovalAndStoresItInInfisical(t *testing.T) {
	f, s := newFake(t), &sink{}
	inf := fakes.NewInfisical(t, map[string]map[string]string{"/agents/example": {}})
	h := setup(t, f, s, inf, kittest.Call(kittest.Fn("p", "mxroute_reset_mailbox_password", map[string]any{"domain": "example.com", "username": "ada",
		"storeAt": map[string]any{"path": "/agents/example", "name": "MAIL_PASSWORD"}})), kittest.Text("ok"))
	cs := kittest.Confirmations(h.Say("reset ada"))
	want := "Set a new password for ada@example.com. The old password stops working and store the password in Infisical staging /agents/example/MAIL_PASSWORD (new)."
	if got := kittest.Summary(t, cs[0]); got != want {
		t.Fatalf("%q", got)
	}
	all := kittest.AllText(h.Approve(cs[0]))
	stored, _ := inf.Value("/agents/example", "MAIL_PASSWORD")
	if stored == "" || stored != f.Password("ada@example.com") || len(s.delivered) != 0 || strings.Contains(all, stored) {
		t.Fatalf("%q", stored)
	}
}

func TestRefusesStoreAtWhenInfisicalIsNotSetUpBeforeAnyApproval(t *testing.T) {
	f := newFake(t)
	h := setup(t, f, &sink{}, nil, kittest.Call(kittest.Fn("p", "mxroute_reset_mailbox_password", map[string]any{"domain": "example.com", "username": "ada",
		"storeAt": map[string]any{"path": "/agents/example", "name": "MAIL_PASSWORD"}})), kittest.Text("ok"))
	evs := h.Say("reset ada")
	if len(kittest.Confirmations(evs)) != 0 || len(f.Ops()) != 0 || !strings.Contains(kittest.AllText(evs), "Infisical is not set up") {
		t.Fatal(kittest.AllText(evs))
	}
}

func TestRefusesAMailboxWithNoPlaceForThePasswordBeforeAnyApproval(t *testing.T) {
	f := newFake(t)
	h := setup(t, f, secrets.None, nil, kittest.Call(createHello), kittest.Text("ok"))
	evs := h.Say("make hello@")
	if len(kittest.Confirmations(evs)) != 0 || len(f.Ops()) != 0 || !strings.Contains(kittest.AllText(evs), "Call again with storeAt") {
		t.Fatal(kittest.AllText(evs))
	}
}

func TestRefusesAForwarderToAValueThatIsNotAnAddress(t *testing.T) {
	f := newFake(t)
	h := setup(t, f, &sink{}, nil, kittest.Call(kittest.Fn("c", "mxroute_create_forwarder", map[string]any{"domain": "example.com", "alias": "sales", "destinations": []any{"Sam <sam@example.org>"}})), kittest.Text("ok"))
	evs := h.Say("x")
	if len(kittest.Confirmations(evs)) != 0 || len(f.Ops()) != 0 {
		t.Fatal(kittest.AllText(evs))
	}
}

// The client

func TestSendsTheThreeKeysAsHeadersAndShowsAnErrorWithItsField(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"RATE_LIMITED","message":"Too many requests","field":"username"}}`))
	}))
	defer srv.Close()
	c := &Client{Server: "s1", Username: "u1", APIKey: "k1", BaseURL: srv.URL, HTTP: srv.Client()}
	err := c.CreateAccount(t.Context(), "example.com", NewAccount{Username: "x", Password: "pw-should-not-leak"})
	if got.Get("X-Server") != "s1" || got.Get("X-Username") != "u1" || got.Get("X-API-Key") != "k1" {
		t.Fatalf("%v", got)
	}
	if err == nil || err.Error() != "MXroute RATE_LIMITED (429): Too many requests (retry after 30s) [field: username]" || strings.Contains(err.Error(), "pw-should-not-leak") {
		t.Fatal(err)
	}
}
