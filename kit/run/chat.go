package run

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

// ErrExit is Chat's answer when the operator types exit.
var ErrExit = errors.New("the operator typed exit")

// Chat is the terminal chat on a laptop. It reads lines from in, and asks on
// the terminal for each approval. It gives ErrExit when the operator types
// exit, and nil at the end of in.
func (a *Agent) Chat(ctx context.Context, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	read := func(prompt string) (string, bool) {
		fmt.Fprint(out, prompt)
		if !sc.Scan() {
			return "", false
		}
		return strings.TrimSpace(sc.Text()), true
	}
	const user = "operator"
	id := uuid.NewString()
	if _, err := a.Sessions.Create(ctx, &session.CreateRequest{AppName: a.appName, UserID: user, SessionID: id}); err != nil {
		return err
	}
	for {
		line, ok := read("You: ")
		if !ok {
			return nil
		}
		if line == "exit" {
			return ErrExit
		}
		if line == "" {
			continue
		}
		msg := genai.NewContentFromText(line, genai.RoleUser)
		for msg != nil {
			var text strings.Builder
			var asks []*genai.FunctionCall
			for ev, err := range a.Runner.Run(ctx, user, id, msg, agent.RunConfig{}) {
				if err != nil {
					fmt.Fprintf(out, "\nError: %v\n\n", err)
					break
				}
				if ev.ErrorMessage != "" {
					fmt.Fprintf(out, "\nError: %s\n", ev.ErrorMessage)
				}
				if ev.Content == nil || ev.Author == "user" {
					continue
				}
				for _, p := range ev.Content.Parts {
					if p.Text != "" && !p.Thought {
						text.WriteString(p.Text)
					}
					if fc := p.FunctionCall; fc != nil && fc.Name == toolconfirmation.FunctionCallName {
						asks = append(asks, fc)
					}
				}
			}
			if t := strings.TrimSpace(text.String()); t != "" {
				fmt.Fprintf(out, "\n%s: %s\n\n", a.Name, t)
			}
			msg = nil
			if len(asks) == 0 {
				break
			}
			msg = &genai.Content{Role: genai.RoleUser}
			for _, fc := range asks {
				summary, hash := approvalOf(fc)
				fmt.Fprintf(out, "\n  %s wants to: %s\n", a.Name, summary)
				answer, _ := read("  Approve? Type yes to approve: ")
				payload := map[string]any{"argsHash": hash}
				ok := strings.EqualFold(answer, "yes")
				if !ok {
					reason, _ := read("  Reason (optional): ")
					if reason != "" {
						payload["reason"] = reason
					}
				}
				msg.Parts = append(msg.Parts, &genai.Part{FunctionResponse: &genai.FunctionResponse{
					ID: fc.ID, Name: toolconfirmation.FunctionCallName, Response: map[string]any{"confirmed": ok, "payload": payload},
				}})
			}
		}
	}
}

func approvalOf(fc *genai.FunctionCall) (summary, hash string) {
	b, _ := json.Marshal(fc.Args["toolConfirmation"])
	var tc struct {
		Hint    string `json:"hint"`
		Payload struct {
			Summary  string `json:"summary"`
			ArgsHash string `json:"argsHash"`
		} `json:"payload"`
	}
	_ = json.Unmarshal(b, &tc)
	summary = tc.Payload.Summary
	if summary == "" {
		summary = tc.Hint
	}
	return summary, tc.Payload.ArgsHash
}
