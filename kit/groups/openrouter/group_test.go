package openrouter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"google.golang.org/genai"

	"github.com/rubics-code/agent-kit/kit/group"
	"github.com/rubics-code/agent-kit/kit/internal/kittest"
	"github.com/rubics-code/agent-kit/kit/policy"
	"github.com/rubics-code/agent-kit/kit/secrets"
)

var testPolicy = policy.Policy{Agent: "Example", Operator: "Sam", MaxKeyLimitUSD: 100}

// sink keeps the secrets that it gets, so a test can look for them.
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

func setup(t *testing.T, f *fakeOpenRouter, s secrets.Sink, script ...*genai.Content) *kittest.Harness {
	t.Helper()
	asm, err := group.Assemble([]group.Group{Group}, group.Services{"openrouter": f.client()}, s, testPolicy,
		group.Enable{Has: func(string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	return kittest.New(t, kittest.Script(script...), asm.Tools, kittest.Config{})
}

var createExample = kittest.Call(kittest.Fn("c1", "openrouter_create_key", map[string]any{"name": "Example chatbot", "limitUsd": 10, "expiresOn": "2027-06-30"}))

var keyHash = strings.Repeat("0", 63) + "1"

// OpenRouter key tools

func TestListsKeysWithNoApproval(t *testing.T) {
	h := setup(t, newFake(t), &sink{}, kittest.Call(kittest.Fn("l1", "openrouter_list_keys", map[string]any{})), kittest.Text("one key"))
	evs := h.Say("keys?")
	keys, _ := kittest.Responses(evs)["openrouter_list_keys"][0]["keys"].([]any)
	if len(kittest.Confirmations(evs)) != 0 || len(keys) != 1 {
		t.Fatal(kittest.AllText(evs))
	}
	k := keys[0].(map[string]any)
	if k["name"] != "Example" || k["limit_usd"] != float64(20) || k["limit_remaining_usd"] != float64(17) {
		t.Fatalf("%v", k)
	}
}

func TestCreatesAKeyOnlyAfterApprovalAndTheKeyNeverEntersTheConversation(t *testing.T) {
	f, s := newFake(t), &sink{}
	h := setup(t, f, s, createExample, kittest.Text("done"))
	cs := kittest.Confirmations(h.Say("key for the example site"))
	if got := kittest.Summary(t, cs[0]); got != `Create the OpenRouter API key "Example chatbot" (limit $10.00 monthly until 2027-06-30)` || f.writeCount() != 0 {
		t.Fatalf("%q", got)
	}
	evs := h.Approve(cs[0])
	want := []map[string]any{{"op": "createKey", "args": map[string]any{"name": "Example chatbot", "limit": float64(10), "limit_reset": "monthly", "expires_at": "2027-06-30T23:59:59Z"}}}
	if !reflect.DeepEqual(f.writes, want) {
		t.Fatalf("%v", f.writes)
	}
	if len(s.delivered) != 1 || !strings.HasPrefix(s.delivered[0], "sk-or-v1-") {
		t.Fatalf("%v", s.delivered)
	}
	all := kittest.AllText(evs)
	if strings.Contains(all, s.delivered[0]) || !strings.Contains(all, "It went directly to Sam's terminal. You do not have it.") {
		t.Fatal(all)
	}
}

func TestDoesNotCreateAKeyWhenTheOperatorRejects(t *testing.T) {
	f, s := newFake(t), &sink{}
	h := setup(t, f, s, createExample, kittest.Text("ok"))
	cs := kittest.Confirmations(h.Say("key"))
	h.Reject(cs[0], "no")
	if f.writeCount() != 0 || len(s.delivered) != 0 {
		t.Fatal(f.writes, s.delivered)
	}
}

func TestRefusesAKeyWithNoLimitOrALimitAboveTheCap(t *testing.T) {
	f := newFake(t)
	h := setup(t, f, &sink{}, kittest.Call(
		kittest.Fn("b1", "openrouter_create_key", map[string]any{"name": "big"}),
		kittest.Fn("b2", "openrouter_create_key", map[string]any{"name": "huge", "limitUsd": 5000}),
	), kittest.Text("sorry"))
	evs := h.Say("x")
	rs := kittest.Responses(evs)["openrouter_create_key"]
	if len(kittest.Confirmations(evs)) != 0 || f.writeCount() != 0 || len(rs) != 2 || rs[0]["error"] == nil || rs[1]["error"] == nil {
		t.Fatal(kittest.AllText(evs))
	}
}

func TestShowsTheCurrentAndNewLimitReadFromOpenRouterBeforeAnUpdate(t *testing.T) {
	f := newFake(t)
	h := setup(t, f, &sink{}, kittest.Call(kittest.Fn("u1", "openrouter_update_key", map[string]any{"hash": keyHash, "limitUsd": 40, "disabled": true})), kittest.Text("done"))
	cs := kittest.Confirmations(h.Say("raise it"))
	if got := kittest.Summary(t, cs[0]); got != `Change the OpenRouter API key "Example" (sk-or-v1-001...0001): limit $20.00 to $40.00, disable it` || f.writeCount() != 0 {
		t.Fatalf("%q", got)
	}
	evs := h.Approve(cs[0])
	want := []map[string]any{{"op": "updateKey", "args": map[string]any{"hash": keyHash, "limit": float64(40), "disabled": true}}}
	if !reflect.DeepEqual(f.writes, want) {
		t.Fatalf("%v", f.writes)
	}
	r := kittest.Responses(evs)["openrouter_update_key"]
	last := r[len(r)-1]
	if last["hash"] != keyHash || last["limit_usd"] != float64(40) || last["disabled"] != true {
		t.Fatalf("%v", last)
	}
}

func TestRefusesAnUpdateWithNoChange(t *testing.T) {
	f := newFake(t)
	h := setup(t, f, &sink{}, kittest.Call(kittest.Fn("u2", "openrouter_update_key", map[string]any{"hash": keyHash})), kittest.Text("sorry"))
	evs := h.Say("x")
	if len(kittest.Confirmations(evs)) != 0 || f.writeCount() != 0 || !strings.Contains(kittest.AllText(evs), "give at least one change") {
		t.Fatal(kittest.AllText(evs))
	}
}

func TestRefusesANewKeyWithNoPlaceForItBeforeAnyApproval(t *testing.T) {
	f := newFake(t)
	h := setup(t, f, secrets.None, createExample, kittest.Text("sorry"))
	evs := h.Say("key")
	if len(kittest.Confirmations(evs)) != 0 || f.writeCount() != 0 || !strings.Contains(kittest.AllText(evs), "Call again with storeAt") {
		t.Fatal(kittest.AllText(evs))
	}
}

// The client

func TestSendsTheManagementKeyAsABearerTokenAndReturnsTheOneTimeKey(t *testing.T) {
	var got struct {
		URL, Method, Auth string
		Body              map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.URL, got.Method, got.Auth = r.URL.Path, r.Method, r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&got.Body)
		send(w, 201, map[string]any{"data": map[string]any{"hash": "abc", "name": "x"}, "key": "sk-or-v1-secret"})
	}))
	defer srv.Close()
	c := &Client{ManagementKey: "mgmt-key", BaseURL: srv.URL + "/api/v1", HTTP: srv.Client()}
	_, value, err := c.CreateKey(t.Context(), NewKey{Name: "x", Limit: 5, LimitReset: "monthly"})
	if err != nil || value != "sk-or-v1-secret" {
		t.Fatal(value, err)
	}
	want := map[string]any{"name": "x", "limit": float64(5), "limit_reset": "monthly"}
	if got.URL != "/api/v1/keys" || got.Method != "POST" || got.Auth != "Bearer mgmt-key" || !reflect.DeepEqual(got.Body, want) {
		t.Fatalf("%+v", got)
	}
}

func TestTurnsAnErrorBodyIntoAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		send(w, 403, map[string]any{"error": map[string]any{"code": 403, "message": "Management key does not have permission for this request"}})
	}))
	defer srv.Close()
	_, err := (&Client{ManagementKey: "k", BaseURL: srv.URL, HTTP: srv.Client()}).Credits(t.Context())
	var oe *Error
	if !errorsAs(err, &oe) || !strings.Contains(err.Error(), "(403): Management key does not have permission") {
		t.Fatal(err)
	}
}

func TestPutsNoNewKeyInAnErrorForABadResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"key":"sk-or-v1-leak", "data": [`))
	}))
	defer srv.Close()
	_, _, err := (&Client{ManagementKey: "k", BaseURL: srv.URL, HTTP: srv.Client()}).CreateKey(t.Context(), NewKey{Name: "x", Limit: 1, LimitReset: "monthly"})
	if err == nil || strings.Contains(err.Error(), "leak") {
		t.Fatal(err)
	}
}
