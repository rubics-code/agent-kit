package a2ui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"google.golang.org/adk/v2/tool/toolconfirmation"

	"github.com/rubics-code/agent-kit/kit/approval"
)

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
