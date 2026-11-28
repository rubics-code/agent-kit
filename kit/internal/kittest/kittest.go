// Package kittest runs an agent with a scripted model, for the Kit's tests.
package kittest

import (
	"context"
	"encoding/json"
	"iter"
	"sync"
	"testing"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

// Model answers with the next content in its script, and counts the calls.
type Model struct {
	mu       sync.Mutex
	script   []*genai.Content
	Calls    int
	Requests []*model.LLMRequest
}

func (m *Model) Name() string { return "script" }

func (m *Model) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		m.mu.Lock()
		m.Calls++
		m.Requests = append(m.Requests, req)
		var c *genai.Content
		if len(m.script) > 0 {
			c, m.script = m.script[0], m.script[1:]
		} else {
			c = genai.NewContentFromText("(the script is empty)", genai.RoleModel)
		}
		m.mu.Unlock()
		yield(&model.LLMResponse{Content: c, TurnComplete: true}, nil)
	}
}

// Script makes a model that gives these contents in order.
func Script(c ...*genai.Content) *Model { return &Model{script: c} }

// Call is a model content with tool calls.
func Call(calls ...*genai.FunctionCall) *genai.Content {
	parts := make([]*genai.Part, len(calls))
	for i, fc := range calls {
		parts[i] = &genai.Part{FunctionCall: fc}
	}
	return &genai.Content{Role: genai.RoleModel, Parts: parts}
}

// Text is a model content with text.
func Text(s string) *genai.Content { return genai.NewContentFromText(s, genai.RoleModel) }

// Harness runs one agent in one session.
type Harness struct {
	T      *testing.T
	Model  *Model
	Runner *runner.Runner
	ID     string
}

// Config is what New needs besides the model and the tools.
type Config struct {
	Plugins     []*plugin.Plugin
	After       []llmagent.AfterModelCallback
	Before      []llmagent.BeforeModelCallback
	Toolsets    []tool.Toolset
	Instruction string
}

func New(t *testing.T, m *Model, tools []tool.Tool, cfg Config) *Harness {
	t.Helper()
	if cfg.Instruction == "" {
		cfg.Instruction = "test"
	}
	a, err := llmagent.New(llmagent.Config{Name: "test", Model: m, Instruction: cfg.Instruction, Tools: tools, Toolsets: cfg.Toolsets,
		AfterModelCallbacks: cfg.After, BeforeModelCallbacks: cfg.Before})
	if err != nil {
		t.Fatal(err)
	}
	svc := session.InMemoryService()
	if _, err := svc.Create(t.Context(), &session.CreateRequest{AppName: "test", UserID: "u", SessionID: "s"}); err != nil {
		t.Fatal(err)
	}
	r, err := runner.New(runner.Config{AppName: "test", Agent: a, SessionService: svc, PluginConfig: runner.PluginConfig{Plugins: cfg.Plugins}})
	if err != nil {
		t.Fatal(err)
	}
	return &Harness{T: t, Model: m, Runner: r, ID: "s"}
}

// Send runs one turn and gives its events.
func (h *Harness) Send(c *genai.Content) []*session.Event {
	h.T.Helper()
	var out []*session.Event
	for ev, err := range h.Runner.Run(h.T.Context(), "u", h.ID, c, agent.RunConfig{}) {
		if err != nil {
			h.T.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

// Say sends text from the user.
func (h *Harness) Say(s string) []*session.Event {
	return h.Send(genai.NewContentFromText(s, genai.RoleUser))
}

// Confirmations gives the pending confirmation calls in the events.
func Confirmations(evs []*session.Event) []*genai.FunctionCall {
	var out []*genai.FunctionCall
	for _, ev := range evs {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p.FunctionCall != nil && p.FunctionCall.Name == toolconfirmation.FunctionCallName {
				out = append(out, p.FunctionCall)
			}
		}
	}
	return out
}

// Answer answers confirmations with a payload.
func Answer(confirmed bool, payloads map[string]map[string]any) *genai.Content {
	c := &genai.Content{Role: genai.RoleUser}
	for id, p := range payloads {
		c.Parts = append(c.Parts, &genai.Part{FunctionResponse: &genai.FunctionResponse{
			ID: id, Name: toolconfirmation.FunctionCallName, Response: map[string]any{"confirmed": confirmed, "payload": p},
		}})
	}
	return c
}

// Responses gives the tool responses in the events, by tool name.
func Responses(evs []*session.Event) map[string][]map[string]any {
	out := map[string][]map[string]any{}
	for _, ev := range evs {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if fr := p.FunctionResponse; fr != nil {
				out[fr.Name] = append(out[fr.Name], fr.Response)
			}
		}
	}
	return out
}

// Texts gives the model's text in the events.
func Texts(evs []*session.Event) []string {
	var out []string
	for _, ev := range evs {
		if ev.Content == nil || ev.Author == "user" {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p.Text != "" {
				out = append(out, p.Text)
			}
		}
	}
	return out
}

// Fn is a tool call from the model.
func Fn(id, name string, args map[string]any) *genai.FunctionCall {
	return &genai.FunctionCall{ID: id, Name: name, Args: args}
}

// Payload gives the approval payload of a confirmation call, with its hint.
func Payload(t *testing.T, fc *genai.FunctionCall) map[string]any {
	t.Helper()
	b, _ := json.Marshal(fc.Args["toolConfirmation"])
	var tc struct {
		Hint    string         `json:"hint"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(b, &tc); err != nil || tc.Payload == nil {
		t.Fatalf("no approval payload in %v: %v", fc.Args, err)
	}
	tc.Payload["hint"] = tc.Hint
	return tc.Payload
}

// Summary gives the approval summary of a confirmation call.
func Summary(t *testing.T, fc *genai.FunctionCall) string {
	t.Helper()
	s, _ := Payload(t, fc)["summary"].(string)
	return s
}

// Approve approves a confirmation call with its own arguments hash.
func (h *Harness) Approve(fc *genai.FunctionCall) []*session.Event {
	h.T.Helper()
	return h.Send(Answer(true, map[string]map[string]any{fc.ID: {"argsHash": Payload(h.T, fc)["argsHash"]}}))
}

// Reject rejects a confirmation call.
func (h *Harness) Reject(fc *genai.FunctionCall, reason string) []*session.Event {
	h.T.Helper()
	return h.Send(Answer(false, map[string]map[string]any{fc.ID: {"argsHash": Payload(h.T, fc)["argsHash"], "reason": reason}}))
}

// AllText gives the events as JSON, to look for a value anywhere in them.
func AllText(evs []*session.Event) string {
	b, _ := json.Marshal(evs)
	return string(b)
}
