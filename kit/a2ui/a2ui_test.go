package a2ui

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"google.golang.org/adk/v2/tool/toolconfirmation"

	"github.com/rubics-code/agent-kit/kit/approval"
)

var update = flag.Bool("update", false, "write the golden files")

func TestBasicApprovalCardIsA2UIv1(t *testing.T) {
	ap, _ := approval.NewApproval("write_note", "sum", map[string]any{"title": "t"})
	b, _ := json.Marshal(BasicApprovalCard("call-1", ap))
	for _, want := range []string{`"version":"1.0"`, `"createSurface"`, `"updateComponents"`, BasicCatalog, ActionApprove, ap.ArgsHash} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("card lacks %s: %s", want, b)
		}
	}
}

func TestApproveActionBecomesConfirmation(t *testing.T) {
	p := Part([]any{M{"version": "1.0", "action": M{
		"name": ActionApprove, "surfaceId": "approval-call-1", "sourceComponentId": "approve",
		"timestamp": "2026-11-28T00:00:00Z", "context": M{"confirmationId": "call-1", "argsHash": "sha256:x"},
	}}})
	g, err := ToGenAI(t.Context(), nil, p)
	if err != nil {
		t.Fatal(err)
	}
	fr := g.FunctionResponse
	if fr == nil || fr.Name != toolconfirmation.FunctionCallName || fr.ID != "call-1" || fr.Response["confirmed"] != true {
		t.Fatalf("got %+v", g)
	}
	other := Part([]any{M{"version": "1.0", "action": M{"name": "open", "context": M{}}}})
	if g, _ := ToGenAI(t.Context(), nil, other); !strings.Contains(g.Text, `"open"`) {
		t.Fatalf("other action: %+v", g)
	}
	if g, _ := ToGenAI(t.Context(), nil, a2a.NewTextPart("hi")); g.Text != "hi" {
		t.Fatalf("text part changed: %+v", g)
	}
}

func TestRenders(t *testing.T) {
	m := &a2a.Message{Metadata: map[string]any{"a2uiRendererCapabilities": map[string]any{"1.0": map[string]any{"supportedCatalogIds": []any{BasicCatalog}}}}}
	if !Renders(m, BasicCatalog) || Renders(m, "other") || Renders(&a2a.Message{}, BasicCatalog) || Renders(nil, BasicCatalog) {
		t.Fatal("Renders is wrong")
	}
}

func TestKitApprovalCard(t *testing.T) {
	ap, _ := approval.NewApproval("write_note", "Write it", map[string]any{"title": "t", "body": "b"})
	msgs := KitApprovalCard("call-2", ap)
	b, _ := json.Marshal(msgs)
	for _, want := range []string{KitCatalog, `"component":"Approval"`, `"summary":"Write it"`, `{"label":"body","value":"b","mono":true}`, ap.ArgsHash} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("card lacks %s: %s", want, b)
		}
	}
}

// The web client renders this file in web/src/a2ui/kit.test.tsx, so the Go
// card and the React catalog cannot drift apart. Run with -update to write it.
func TestKitApprovalCardGolden(t *testing.T) {
	ap, _ := approval.NewApproval("write_note", `Write the note "Groceries" (10 characters): "milk, eggs"`, map[string]any{"title": "Groceries", "body": "milk, eggs"})
	got, _ := json.MarshalIndent(KitApprovalCard("call-1", ap), "", "  ")
	path := filepath.Join("testdata", "kit_approval.json")
	if *update {
		if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(want)) != string(got) {
		t.Fatalf("the card changed; run go test ./kit/a2ui -update and check web/src/a2ui/kit.test.tsx\n%s", got)
	}
}
