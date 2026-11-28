package fakemodel

import (
	"testing"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/model"
)

func req(parts ...*genai.Part) *model.LLMRequest {
	return &model.LLMRequest{Contents: []*genai.Content{{Role: "user", Parts: parts}}}
}

func TestReplyRules(t *testing.T) {
	c := Reply(req(genai.NewPartFromText("Write a note called Groceries with the text: milk, eggs.")))
	fc := c.Parts[0].FunctionCall
	if fc == nil || fc.Name != "write_note" || fc.Args["title"] != "Groceries" || fc.Args["body"] != "milk, eggs" {
		t.Fatalf("write: %+v", c.Parts[0])
	}
	if fc := Reply(req(genai.NewPartFromText("What notes do I have?"))).Parts[0].FunctionCall; fc == nil || fc.Name != "list_notes" {
		t.Fatalf("list: %+v", fc)
	}
	rej := Reply(req(&genai.Part{FunctionResponse: &genai.FunctionResponse{Name: "write_note", Response: map[string]any{"error": `tool "write_note" call is rejected`}}}))
	if rej.Parts[0].Text != "You rejected the write, so I did not write the note." {
		t.Fatalf("rejected: %q", rej.Parts[0].Text)
	}
	if got := Reply(req(genai.NewPartFromText("abc"))).Parts[0].Text; got != "You said: abc" {
		t.Fatalf("echo: %q", got)
	}
}

func TestStreamEndsWithTheFullText(t *testing.T) {
	var last *model.LLMResponse
	n := 0
	for r, err := range (Model{}).GenerateContent(t.Context(), req(genai.NewPartFromText("hello")), true) {
		if err != nil {
			t.Fatal(err)
		}
		n++
		last = r
	}
	if n < 3 || last.Partial || last.Content.Parts[0].Text != "Hello. I am the dev agent, and I run on the fake model." {
		t.Fatalf("n=%d last=%+v", n, last)
	}
}
