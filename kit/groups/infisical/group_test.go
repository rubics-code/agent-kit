package infisical

import (
	"regexp"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/rubics-code/agent-kit/kit/group"
	"github.com/rubics-code/agent-kit/kit/internal/fakeinfisical"
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

var secretValues = []string{"example-api-secret", "sk-example-secret", "ops-admin-secret"}

func seeded(t *testing.T) *fakeinfisical.Server {
	return fakeinfisical.New(t, map[string]map[string]string{
		"/example":        {"OWN_KEY": "own-secret"},
		"/agents/example": {"API_SERVER_KEY": "example-api-secret", "OPENROUTER_API_KEY": "sk-example-secret"},
		"/ops":            {"ADMIN_PASSWORD": "ops-admin-secret"},
	})
}

func setup(t *testing.T, f *fakeinfisical.Server, script ...*genai.Content) *kittest.Harness {
	t.Helper()
	pol := testPolicy(t)
	store := f.Client(pol.Infisical.EnvLabel)
	list := []group.Group{Group}
	asm, err := group.Assemble(list, group.Connect(list, group.ConnectContext{Holds: func(string) bool { return false }, Store: store}),
		secrets.None, pol, group.Enable{Has: func(string) bool { return false }})
	if err != nil {
		t.Fatal(err)
	}
	if len(asm.Tools) != 3 {
		t.Fatalf("%d tools", len(asm.Tools))
	}
	return kittest.New(t, kittest.Script(script...), asm.Tools, kittest.Config{})
}

func random(path, name string) *genai.Content {
	return kittest.Call(kittest.Fn("r", "infisical_create_random_secret", map[string]any{"path": path, "name": name}))
}

// Infisical tools

func TestListsFoldersAndNamesWithNoApprovalAndNoValues(t *testing.T) {
	h := setup(t, seeded(t), kittest.Call(
		kittest.Fn("f", "infisical_list_folders", map[string]any{"path": "/agents"}),
		kittest.Fn("n", "infisical_list_secret_names", map[string]any{"path": "/agents/example"}),
	), kittest.Text("ok"))
	evs := h.Say("what is there?")
	all := kittest.AllText(evs)
	if len(kittest.Confirmations(evs)) != 0 || !strings.Contains(all, `"example"`) || !strings.Contains(all, "API_SERVER_KEY") {
		t.Fatal(all)
	}
	for _, v := range secretValues {
		if strings.Contains(all, v) {
			t.Fatalf("the value %s is in the conversation", v)
		}
	}
}

func TestRefusesAFolderOutsideThePolicyBeforeAnyCall(t *testing.T) {
	f := seeded(t)
	h := setup(t, f, kittest.Call(kittest.Fn("n", "infisical_list_secret_names", map[string]any{"path": "/ops"})), kittest.Text("no"))
	all := kittest.AllText(h.Say("ops?"))
	if strings.Contains(all, "ADMIN") || f.Asked("/ops") || !strings.Contains(all, "/ops is not a readable folder") {
		t.Fatal(all)
	}
}

func TestMakesARandomSecretAfterApprovalAndTheValueNeverEntersTheConversation(t *testing.T) {
	f := seeded(t)
	h := setup(t, f, random("/agents/new-agent", "API_SERVER_KEY"), kittest.Text("done"))
	cs := kittest.Confirmations(h.Say("key for the new agent"))
	want := "Make a random secret (32 bytes, hex) and store it in Infisical staging /agents/new-agent/API_SERVER_KEY (new, makes the folder /agents/new-agent)"
	if got := kittest.Summary(t, cs[0]); got != want || len(f.Writes()) != 0 {
		t.Fatalf("%q %v", got, f.Writes())
	}
	all := kittest.AllText(h.Approve(cs[0]))
	value, ok := f.Value("/agents/new-agent", "API_SERVER_KEY")
	if !ok || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(value) || strings.Contains(all, value) {
		t.Fatalf("%q %v", value, ok)
	}
	if ws := f.Writes(); len(ws) != 2 || ws[0] != (fakeinfisical.Write{Method: "POST", Path: "/agents/new-agent", Env: "staging"}) {
		t.Fatalf("%v", ws)
	}
}

func TestMakesABase64urlSecret(t *testing.T) {
	f := seeded(t)
	h := setup(t, f, kittest.Call(kittest.Fn("r", "infisical_create_random_secret", map[string]any{"path": "/example", "name": "SESSION_KEY", "bytes": 16, "format": "base64url"})), kittest.Text("done"))
	cs := kittest.Confirmations(h.Say("x"))
	if s := kittest.Summary(t, cs[0]); s != "Make a random secret (16 bytes, base64url) and store it in Infisical prod /example/SESSION_KEY (new)" {
		t.Fatal(s)
	}
	h.Approve(cs[0])
	if v, _ := f.Value("/example", "SESSION_KEY"); !regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`).MatchString(v) {
		t.Fatalf("%q", v)
	}
}

func TestDoesNotReplaceASecretWithARandomOne(t *testing.T) {
	f := seeded(t)
	h := setup(t, f, random("/agents/example", "API_SERVER_KEY"), kittest.Text("ok"))
	evs := h.Say("x")
	v, _ := f.Value("/agents/example", "API_SERVER_KEY")
	if len(kittest.Confirmations(evs)) != 0 || len(f.Writes()) != 0 || v != "example-api-secret" || !strings.Contains(kittest.AllText(evs), "already exists") {
		t.Fatal(kittest.AllText(evs))
	}
}

func TestRefusesToWriteInAgentsItself(t *testing.T) {
	f := seeded(t)
	h := setup(t, f, random("/agents", "X"), kittest.Text("no"))
	evs := h.Say("x")
	if len(kittest.Confirmations(evs)) != 0 || len(f.Writes()) != 0 || !strings.Contains(kittest.AllText(evs), "is not a writable folder") {
		t.Fatal(kittest.AllText(evs))
	}
}

// enablement

func TestIsOnWithAStoreAndNamesTheIdentityKeysWithout(t *testing.T) {
	st, _ := group.ReviewGroup(Group, group.EnableEnv{HasStore: true}, "")
	off, _ := group.ReviewGroup(Group, group.EnableEnv{}, "")
	if !st.Enabled || off.Enabled || strings.Join(off.Missing, ",") != "INFISICAL_CLIENT_ID,INFISICAL_CLIENT_SECRET" {
		t.Fatalf("%+v %+v", st, off)
	}
}

func TestIsOnInAgentVaultModeOnlyForTheAgentsHost(t *testing.T) {
	en := group.EnableEnv{HasStore: true, VaultHosts: map[string]bool{"secrets.example.test": true}}
	on, _ := group.ReviewGroup(Group, en, "secrets.example.test")
	off, _ := group.ReviewGroup(Group, en, "other.example.test")
	if !on.Enabled || off.Enabled || off.Missing[0] != "other.example.test" {
		t.Fatalf("%+v %+v", on, off)
	}
}
