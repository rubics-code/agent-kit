package openrouter

import (
	"slices"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/rubics-code/agent-kit/kit/fakes"
	"github.com/rubics-code/agent-kit/kit/group"
	"github.com/rubics-code/agent-kit/kit/groups/infisical"
	"github.com/rubics-code/agent-kit/kit/internal/kittest"
	"github.com/rubics-code/agent-kit/kit/policy"
)

// OpenRouter keys with storeAt, and the Infisical group on.

func storePolicy(t *testing.T) policy.Policy {
	t.Helper()
	p, err := policy.Parse([]byte(`{"agent":"Example","operator":"Sam","maxKeyLimitUsd":100,"infisical":{
		"readable":["/example/**","/agents/**"],"writable":["/example/**","/agents/*/**"],
		"envs":[{"path":"/agents/**","env":"staging"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func withStore(t *testing.T, or *fakes.OpenRouter, inf *fakes.Infisical, s *sink, script ...*genai.Content) *kittest.Harness {
	t.Helper()
	pol := storePolicy(t)
	services := group.Services{"openrouter": client(or)}
	if inf != nil {
		services[group.StoreGroupID] = inf.Client(pol.Infisical.EnvLabel)
	}
	asm, err := group.Assemble([]group.Group{Group, infisical.Group}, services, s, pol,
		group.Enable{Has: func(string) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	return kittest.New(t, kittest.Script(script...), asm.Tools, kittest.Config{})
}

func createAt(name, path string) *genai.Content {
	return kittest.Call(kittest.Fn("k", "openrouter_create_key", map[string]any{"name": name, "limitUsd": 10,
		"storeAt": map[string]any{"path": path, "name": "OPENROUTER_API_KEY"}}))
}

func seededStore(t *testing.T) *fakes.Infisical {
	return fakes.NewInfisical(t, map[string]map[string]string{"/agents/example": {"OPENROUTER_API_KEY": "sk-example-secret"}})
}

func TestStoresANewOpenRouterKeyInInfisicalAndSaysWhenItReplacesAValue(t *testing.T) {
	or, inf, s := newFake(t), seededStore(t), &sink{}
	h := withStore(t, or, inf, s, createAt("Example", "/agents/example"), kittest.Text("done"))
	cs := kittest.Confirmations(h.Say("new key for the example agent"))
	want := `Create the OpenRouter API key "Example" (limit $10.00 monthly with no expiry) and store it in Infisical staging /agents/example/OPENROUTER_API_KEY (REPLACES the current value)`
	if got := kittest.Summary(t, cs[0]); got != want {
		t.Fatalf("%q", got)
	}
	all := kittest.AllText(h.Approve(cs[0]))
	stored, _ := inf.Value("/agents/example", "OPENROUTER_API_KEY")
	values := []string{}
	for _, v := range or.Values() {
		values = append(values, v)
	}
	if stored == "sk-example-secret" || !slices.Contains(values, stored) || len(s.delivered) != 0 {
		t.Fatalf("stored %q, delivered %d", stored, len(s.delivered))
	}
	if strings.Contains(all, stored) || !strings.Contains(all, "stored in Infisical staging /agents/example/OPENROUTER_API_KEY") {
		t.Fatal(all)
	}
}

func TestSendsTheSecretToTheTerminalWhenInfisicalFailsAfterTheKeyExists(t *testing.T) {
	or, inf, s := newFake(t), seededStore(t), &sink{}
	inf.FailPut = "no permission"
	h := withStore(t, or, inf, s, createAt("New", "/agents/new-agent"), kittest.Text("done"))
	cs := kittest.Confirmations(h.Say("x"))
	all := kittest.AllText(h.Approve(cs[0]))
	if len(s.delivered) != 1 || !strings.Contains(all, "went to Sam's terminal instead") || strings.Contains(all, s.delivered[0]) {
		t.Fatal(all)
	}
}

func TestRefusesStoreAtWhenInfisicalIsNotSetUpBeforeAnyApproval(t *testing.T) {
	or := newFake(t)
	h := withStore(t, or, nil, &sink{}, createAt("Example", "/agents/example"), kittest.Text("ok"))
	evs := h.Say("x")
	if len(kittest.Confirmations(evs)) != 0 || len(or.Writes()) != 0 || !strings.Contains(kittest.AllText(evs), "Infisical is not set up") {
		t.Fatal(kittest.AllText(evs))
	}
}

func TestRefusesStoreAtOutsideTheWritablePathsBeforeAnyApproval(t *testing.T) {
	or := newFake(t)
	h := withStore(t, or, seededStore(t), &sink{}, createAt("Example", "/ops"), kittest.Text("ok"))
	evs := h.Say("x")
	if len(kittest.Confirmations(evs)) != 0 || len(or.Writes()) != 0 || !strings.Contains(kittest.AllText(evs), "is not a writable folder") {
		t.Fatal(kittest.AllText(evs))
	}
}
