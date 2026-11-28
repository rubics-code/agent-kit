package briefing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/adk/v2/tool"

	"github.com/rubics-code/agent-kit/kit/group"
	"github.com/rubics-code/agent-kit/kit/policy"
)

var pol = policy.Policy{Agent: "Example", Operator: "Sam", MaxKeyLimitUSD: 25}

func groups() []group.Group {
	mk := func(id, name, key, guidance string) group.Group {
		return group.New(group.Def[string]{ID: id, Name: name, Keys: []string{key}, Setup: "Add " + key + ".", Guidance: guidance,
			Connect: func(c group.ConnectContext) (string, bool) { return "", c.Holds(key) },
			Tools:   func(string, group.Shared) ([]tool.Tool, error) { return nil, nil }})
	}
	return []group.Group{mk("mail", "Mail", "MAIL_KEY", "Ask {{operator}} before a mailbox."), mk("dns", "DNS", "DNS_KEY", "DNS rules.")}
}

func TestPutsTheSystemPromptTheGuidanceOfEachGroupThatIsOnAndTheNote(t *testing.T) {
	has := func(k string) bool { return k == "MAIL_KEY" }
	a, err := group.Assemble(groups(), group.Connect(groups(), group.ConnectContext{Holds: has}), nil, pol, group.Enable{Has: has})
	if err != nil {
		t.Fatal(err)
	}
	b := Assemble("You are {{agent}}. {{operator}} approves each change. Limit {{maxKeyLimitUsd}}.", a, pol)
	if !strings.HasPrefix(b, "You are Example. Sam approves each change. Limit 25.\n\nAsk Sam before a mailbox.\n\nYour tool groups now.") {
		t.Fatalf("%s", b)
	}
	if strings.Contains(b, "DNS rules.") || !strings.Contains(b, "- DNS: OFF. Missing: DNS_KEY. Add DNS_KEY.") {
		t.Fatalf("%s", b)
	}
}

func TestLoadsTheAgentsHome(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "prompts"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "prompts", "system.md"), []byte("You are {{agent}}.\n"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "policy.json"), []byte(`{"agent":"Example","operator":"Sam"}`), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "memory", "b.md"), []byte("second"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "memory", "a.md"), []byte("first"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "memory", "notes.txt"), []byte("not memory"), 0o600)
	h, err := LoadHome(dir)
	if err != nil {
		t.Fatal(err)
	}
	if h.System != "You are {{agent}}." || h.Policy.Operator != "Sam" || len(h.Memory) != 2 || h.Memory[0].Text != "first" {
		t.Fatalf("%+v", h)
	}
}

func TestAHomeNeedsASystemPromptAndAPolicy(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadHome(dir); err == nil || !strings.Contains(err.Error(), "prompts/system.md") {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(dir, "prompts"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "prompts", "system.md"), []byte("x"), 0o600)
	if _, err := LoadHome(dir); err == nil {
		t.Fatal("a home with no policy passed")
	}
}

func TestAREADMEInTheMemoryFolderIsNotMemory(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "README.md"), []byte("Memory lands here."), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "notes.md"), []byte("A fact."), 0o600)
	files, err := ReadMemory(dir)
	if err != nil || len(files) != 1 || files[0].Name != "notes.md" {
		t.Fatalf("%+v %v", files, err)
	}
}
