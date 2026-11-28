package group

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	"github.com/rubics-code/agent-kit/kit/policy"
	"github.com/rubics-code/agent-kit/kit/secrets"
)

type empty struct{}

type approved struct{ tool.Tool }

func (approved) RequiresApproval() bool { return true }

func fn(t *testing.T, name string) tool.Tool {
	t.Helper()
	tl, err := functiontool.New(functiontool.Config{Name: name, Description: name}, func(agent.Context, empty) (map[string]any, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

type mailClient struct{ user string }

// Sample groups. Real groups come in phase 3.
func groups(t *testing.T) []Group {
	mail := New(Def[*mailClient]{
		ID: "mail", Name: "Mail", Host: "mail.example.test", Keys: []string{"MAIL_USER", "MAIL_KEY"},
		Setup: "Add MAIL_USER and MAIL_KEY.", Guidance: "Mail guidance for {{operator}}.",
		Connect: func(c ConnectContext) (*mailClient, bool) {
			if !c.HoldsAll("MAIL_USER", "MAIL_KEY") {
				return nil, false
			}
			return &mailClient{user: c.Get("MAIL_USER")}, true
		},
		Tools: func(_ *mailClient, _ Shared) ([]tool.Tool, error) {
			return []tool.Tool{fn(t, "mail_list"), approved{fn(t, "mail_create")}}, nil
		},
	})
	dns := New(Def[string]{
		ID: "dns", Name: "DNS", Host: "dns.example.test", Keys: []string{"DNS_TOKEN"},
		Setup: "Add DNS_TOKEN.", Guidance: "DNS guidance.",
		Connect: func(c ConnectContext) (string, bool) { return "dns", c.Holds("DNS_TOKEN") },
		Tools: func(string, Shared) ([]tool.Tool, error) {
			return []tool.Tool{fn(t, "dns_list"), approved{fn(t, "dns_write")}}, nil
		},
	})
	return []Group{mail, dns}
}

func has(keys ...string) func(string) bool {
	return func(k string) bool {
		for _, x := range keys {
			if x == k {
				return true
			}
		}
		return false
	}
}

func ctxWith(keys ...string) ConnectContext {
	return ConnectContext{Holds: has(keys...), Get: func(k string) string { return "value-of-" + k }}
}

var testPolicy = policy.Policy{Agent: "Example", Operator: "Sam", MaxKeyLimitUSD: 10}

// tool groups

func TestKeepTheToolNamesAndTheApprovalFlags(t *testing.T) {
	cat, err := groups(t)[0].Catalog()
	if err != nil {
		t.Fatal(err)
	}
	want := []ToolMeta{{"mail_list", false}, {"mail_create", true}}
	if len(cat) != 2 || cat[0] != want[0] || cat[1] != want[1] {
		t.Fatalf("%+v", cat)
	}
}

func TestDeriveACatalogFromTheirOwnFactories(t *testing.T) {
	for _, g := range groups(t) {
		cat, err := g.Catalog()
		if err != nil || len(cat) == 0 {
			t.Fatalf("%s: %v %v", g.ID, cat, err)
		}
		for _, m := range cat {
			if !strings.HasPrefix(m.Name, g.ID+"_") {
				t.Fatalf("%s has the tool %s", g.ID, m.Name)
			}
		}
	}
}

func TestMarkEachToolThatAsksForApprovalStructurally(t *testing.T) {
	cat := Catalog([]tool.Tool{fn(t, "a"), approved{fn(t, "b")}})
	if cat[0].Approval || !cat[1].Approval {
		t.Fatalf("%+v", cat)
	}
}

func TestNameTheMissingKeysOfAGroupThatIsOff(t *testing.T) {
	st, err := ReviewGroup(groups(t)[0], EnableEnv{Has: has("MAIL_USER")}, "")
	if err != nil || st.Enabled || len(st.Missing) != 1 || st.Missing[0] != "MAIL_KEY" {
		t.Fatalf("%+v %v", st, err)
	}
	st, _ = ReviewGroup(groups(t)[0], EnableEnv{Has: has("MAIL_USER", "MAIL_KEY")}, "")
	if !st.Enabled || len(st.Missing) != 0 {
		t.Fatalf("%+v", st)
	}
}

func TestTellTheOperatorWhyAGroupIsOff(t *testing.T) {
	a, err := Assemble(groups(t), Connect(groups(t), ctxWith("DNS_TOKEN")), nil, testPolicy, Enable{Has: has("DNS_TOKEN")})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"If Sam asks why a tool is not there", "- Mail: OFF. Missing: MAIL_USER, MAIL_KEY. Add MAIL_USER and MAIL_KEY.", "- DNS: on."} {
		if !strings.Contains(a.Note, want) {
			t.Fatalf("note lacks %q:\n%s", want, a.Note)
		}
	}
}

// connect

func TestMakesAServiceOnlyWhenTheSessionHoldsEveryKey(t *testing.T) {
	s := Connect(groups(t), ctxWith("MAIL_USER"))
	if _, ok := s["mail"]; ok {
		t.Fatal("mail connected with one key")
	}
	s = Connect(groups(t), ctxWith("MAIL_USER", "MAIL_KEY"))
	if c, ok := s["mail"].(*mailClient); !ok || c.user != "value-of-MAIL_USER" {
		t.Fatalf("%+v", s)
	}
}

type memStore struct{}

func (memStore) ListFolders(context.Context, string) ([]string, error)     { return nil, nil }
func (memStore) ListSecretNames(context.Context, string) ([]string, error) { return nil, nil }
func (memStore) Check(context.Context, secrets.Place) (bool, bool, error)  { return false, false, nil }
func (memStore) Put(context.Context, secrets.Place, string) error          { return nil }

func TestGivesTheStoreGroupTheAgentsStore(t *testing.T) {
	var got secrets.Store
	store := New(Def[secrets.Store]{
		ID: StoreGroupID, Name: "Infisical", Host: "secrets.example.test",
		Review: func(e EnableEnv) Review {
			return Review{Enabled: e.HasStore, Missing: []string{"a secret store"}}
		},
		Connect: func(c ConnectContext) (secrets.Store, bool) { return c.Store, c.Store != nil },
		Tools:   func(secrets.Store, Shared) ([]tool.Tool, error) { return nil, nil },
	})
	probe := New(Def[string]{
		ID: "probe", Name: "Probe", Keys: []string{"PROBE"},
		Connect: func(ConnectContext) (string, bool) { return "p", true },
		Tools: func(_ string, s Shared) ([]tool.Tool, error) {
			got = s.Store
			return nil, nil
		},
	})
	list := []Group{store, probe}
	ctx := ctxWith("PROBE")
	ctx.Store = memStore{}
	if _, err := Assemble(list, Connect(list, ctx), nil, testPolicy, Enable{Has: has("PROBE")}); err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("the probe group did not get the store")
	}
}

func TestConnectsEachGroupInAListByID(t *testing.T) {
	s := Connect(groups(t), ctxWith("MAIL_USER", "MAIL_KEY", "DNS_TOKEN"))
	if len(s) != 2 || s["dns"] != "dns" {
		t.Fatalf("%+v", s)
	}
}

// a custom group

func custom(t *testing.T) Group {
	return New(Def[string]{
		ID: "weather", Name: "Weather", Host: "weather.example.test", Keys: []string{"WEATHER_KEY"},
		Setup: "Add WEATHER_KEY.", Guidance: "Use the weather tools for forecasts.",
		Connect: func(c ConnectContext) (string, bool) { return c.Get("WEATHER_KEY"), c.Holds("WEATHER_KEY") },
		Tools:   func(string, Shared) ([]tool.Tool, error) { return []tool.Tool{fn(t, "weather_today")}, nil },
	})
}

func TestMountsItsToolsAndJoinsTheBriefingWhenItsKeyIsThere(t *testing.T) {
	list := []Group{custom(t)}
	a, err := Assemble(list, Connect(list, ctxWith("WEATHER_KEY")), nil, testPolicy, Enable{Has: has("WEATHER_KEY")})
	if err != nil || len(a.Tools) != 1 || a.Tools[0].Name() != "weather_today" || len(a.Guidance) != 1 {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestStaysOffWithoutItsKey(t *testing.T) {
	list := []Group{custom(t)}
	a, err := Assemble(list, Connect(list, ctxWith()), nil, testPolicy, Enable{Has: has()})
	if err != nil || len(a.Tools) != 0 || len(a.Guidance) != 0 || a.Statuses[0].Enabled {
		t.Fatalf("%+v %v", a, err)
	}
}

// tool groups in Agent Vault mode

func TestTurnOnOnlyTheGroupsWhoseHostIsInTheSessionAndNameTheMissingHost(t *testing.T) {
	en := Enable{Has: has("MAIL_USER", "MAIL_KEY", "DNS_TOKEN"), VaultHosts: map[string]bool{"dns.example.test": true}}
	a, err := Assemble(groups(t), Connect(groups(t), ctxWith("MAIL_USER", "MAIL_KEY", "DNS_TOKEN")), nil, testPolicy, en)
	if err != nil {
		t.Fatal(err)
	}
	mail, dns := a.Statuses[0], a.Statuses[1]
	if mail.Enabled || mail.Missing[0] != "mail.example.test" || mail.Setup != vaultSetup || !dns.Enabled {
		t.Fatalf("%+v %+v", mail, dns)
	}
}

func TestBuildTheAgentWithoutTheToolsOfAMissingServiceAlthoughEveryPlaceholderExists(t *testing.T) {
	en := Enable{Has: has("MAIL_USER", "MAIL_KEY", "DNS_TOKEN"), VaultHosts: map[string]bool{"dns.example.test": true}}
	a, _ := Assemble(groups(t), Connect(groups(t), ctxWith("MAIL_USER", "MAIL_KEY", "DNS_TOKEN")), nil, testPolicy, en)
	var names []string
	for _, tl := range a.Tools {
		names = append(names, tl.Name())
	}
	if strings.Join(names, ",") != "dns_list,dns_write" {
		t.Fatalf("%v", names)
	}
}

func TestAHostOverrideDecidesInAgentVaultMode(t *testing.T) {
	en := Enable{VaultHosts: map[string]bool{"secrets.internal.test": true}, Hosts: map[string]string{"dns": "secrets.internal.test"}}
	a, _ := Assemble(groups(t), Services{}, nil, testPolicy, en)
	if !a.Statuses[1].Enabled {
		t.Fatalf("%+v", a.Statuses[1])
	}
}

func TestRefusesTwoToolsWithOneName(t *testing.T) {
	g := custom(t)
	twin := New(Def[string]{ID: "twin", Name: "Twin", Keys: []string{"WEATHER_KEY"},
		Connect: func(ConnectContext) (string, bool) { return "", true },
		Tools:   func(string, Shared) ([]tool.Tool, error) { return []tool.Tool{fn(t, "weather_today")}, nil }})
	list := []Group{g, twin}
	if _, err := Assemble(list, Connect(list, ctxWith("WEATHER_KEY")), nil, testPolicy, Enable{Has: has("WEATHER_KEY")}); err == nil {
		t.Fatal("two tools with one name passed")
	}
}

func TestKeys(t *testing.T) {
	if got := strings.Join(Keys(append(groups(t), custom(t))), ","); got != "MAIL_USER,MAIL_KEY,DNS_TOKEN,WEATHER_KEY" {
		t.Fatal(got)
	}
}
