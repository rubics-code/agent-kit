package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

func TestArgsHashIsStableAndBindsToArgs(t *testing.T) {
	a := writeArgs{Title: "t", Body: "b"}
	h1, _ := ArgsHash(writeToolName, a)
	h2, _ := ArgsHash(writeToolName, map[string]any{"body": "b", "title": "t"})
	if h1 != h2 {
		t.Fatalf("struct and map give other hashes: %s %s", h1, h2)
	}
	if err := CheckBinding(writeToolName, a, map[string]any{"argsHash": h1}); err != nil {
		t.Fatal(err)
	}
	if err := CheckBinding(writeToolName, writeArgs{Title: "t", Body: "B"}, map[string]any{"argsHash": h1}); err == nil {
		t.Fatal("changed args passed the check")
	}
	if err := CheckBinding(writeToolName, a, map[string]any{}); err == nil {
		t.Fatal("missing hash passed the check")
	}
}

func TestSummaryComesFromCode(t *testing.T) {
	s := writeSummary(writeArgs{Title: "Groceries", Body: strings.Repeat("x", 100)})
	if !strings.Contains(s, `"Groceries"`) || !strings.Contains(s, "100 characters") {
		t.Fatal(s)
	}
}

func TestApprovalCardIsA2UIv1(t *testing.T) {
	ap, _ := NewApproval(writeToolName, "sum", writeArgs{Title: "t", Body: "b"})
	msgs := ApprovalCard("call-1", ap)
	b, _ := json.Marshal(msgs)
	for _, want := range []string{`"version":"1.0"`, `"createSurface"`, `"updateComponents"`, a2uiBasicCatalog, actionApprove, ap.ArgsHash} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("card lacks %s: %s", want, b)
		}
	}
}

func TestApproveActionBecomesConfirmation(t *testing.T) {
	p := a2uiPart([]any{map[string]any{"version": "1.0", "action": map[string]any{
		"name": actionApprove, "surfaceId": "approval-call-1", "sourceComponentId": "approve",
		"timestamp": "2026-11-28T00:00:00Z", "context": map[string]any{"confirmationId": "call-1", "argsHash": "sha256:x"},
	}}})
	g, err := a2uiToGenAI(t.Context(), nil, p)
	if err != nil {
		t.Fatal(err)
	}
	fr := g.FunctionResponse
	if fr == nil || fr.Name != toolconfirmation.FunctionCallName || fr.ID != "call-1" || fr.Response["confirmed"] != true {
		t.Fatalf("got %+v", g)
	}
	if got, _ := a2uiToGenAI(t.Context(), nil, a2a.NewTextPart("hi")); got.Text != "hi" {
		t.Fatalf("text part changed: %+v", got)
	}
}
