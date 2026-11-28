// Package fakemodel is a model.LLM with fixed rules, for tests and for the
// dev agent without a key. It never calls a provider.
package fakemodel

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"regexp"
	"strings"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/model"
)

// Model answers from the last content of the request.
type Model struct{}

func (Model) Name() string { return "fake" }

var (
	writeRe = regexp.MustCompile(`(?i)note called\s+"?([^":]+?)"?\s+with the text:?\s*(.+)$`)
	cardRe  = regexp.MustCompile(`(?i)card called\s+"?([^":]+?)"?\s+with the items:?\s*(.+)$`)
)

func call(name string, args map[string]any) *genai.Content {
	return &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{
		ID: "fake-" + name, Name: name, Args: args,
	}}}}
}

func text(s string) *genai.Content { return genai.NewContentFromText(s, genai.RoleModel) }

// Reply gives the model's next content for the request.
func Reply(req *model.LLMRequest) *genai.Content {
	if len(req.Contents) == 0 {
		return text("Hello.")
	}
	last := req.Contents[len(req.Contents)-1]
	for _, p := range last.Parts {
		if fr := p.FunctionResponse; fr != nil {
			return afterTool(fr)
		}
	}
	var said string
	for _, p := range last.Parts {
		said += p.Text
	}
	said = strings.TrimSpace(said)
	switch m := writeRe.FindStringSubmatch(said); {
	case m != nil:
		return call("write_note", map[string]any{"title": strings.TrimSpace(m[1]), "body": strings.TrimSpace(strings.TrimSuffix(m[2], "."))})
	case cardRe.MatchString(said):
		c := cardRe.FindStringSubmatch(said)
		var items []any
		for it := range strings.SplitSeq(strings.TrimSuffix(c[2], "."), ",") {
			items = append(items, strings.TrimSpace(it))
		}
		return call("put_dashboard_card", map[string]any{"cardId": strings.ToLower(strings.ReplaceAll(strings.TrimSpace(c[1]), " ", "-")), "title": strings.TrimSpace(c[1]), "items": items})
	case strings.Contains(strings.ToLower(said), "notes"):
		return call("list_notes", map[string]any{})
	case strings.Contains(strings.ToLower(said), "hello"):
		return text("Hello. I am the dev agent, and I run on the fake model.")
	}
	return text("You said: " + said)
}

func afterTool(fr *genai.FunctionResponse) *genai.Content {
	if e, ok := fr.Response["error"]; ok {
		if strings.Contains(fmt.Sprint(e), "rejected") {
			return text("You rejected the write, so I did not write the note.")
		}
		return text(fmt.Sprintf("The tool %s failed: %v", fr.Name, e))
	}
	status, _ := fr.Response["status"].(string)
	switch fr.Name {
	case "write_note":
		switch status {
		case "written":
			return text(fmt.Sprintf("The note %q is written.", fr.Response["title"]))
		case "rejected":
			return text("You rejected the write, so I did not write the note.")
		}
		return text("The write waits for your approval.")
	case "list_notes":
		b, _ := json.Marshal(fr.Response["notes"])
		if string(b) == "null" || string(b) == "[]" {
			return text("You have no notes.")
		}
		return text("Your notes: " + string(b))
	case "put_dashboard_card":
		return text("The card is on the dashboard.")
	}
	return text("Done.")
}

func (Model) GenerateContent(_ context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		c := Reply(req)
		usage := &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 100, CandidatesTokenCount: 10, TotalTokenCount: 110}
		if stream && c.Parts[0].Text != "" {
			words := strings.SplitAfter(c.Parts[0].Text, " ")
			for _, w := range words {
				if !yield(&model.LLMResponse{Content: text(w), Partial: true}, nil) {
					return
				}
			}
		}
		yield(&model.LLMResponse{Content: c, UsageMetadata: usage, TurnComplete: true, FinishReason: genai.FinishReasonStop}, nil)
	}
}
