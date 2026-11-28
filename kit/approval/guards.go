package approval

import (
	"log/slog"
	"regexp"
	"strings"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

// AnsweredText is the reply to an approval that was answered already.
const AnsweredText = "This approval was answered already. Nothing ran again."

// ReplayGuard is a runner plugin. It drops an answer to a confirmation that
// was answered already, before the model runs. Without it, ADK does not run
// the tool again, but it calls the model once more.
func ReplayGuard() (*plugin.Plugin, error) {
	return plugin.New(plugin.Config{
		Name: "kit-replay-guard",
		BeforeRunCallback: func(ctx agent.InvocationContext) (*genai.Content, error) {
			ids := confirmationAnswers(ctx.UserContent())
			if len(ids) == 0 {
				return nil, nil
			}
			seen := map[string]int{}
			for ev := range ctx.Session().Events().All() {
				if ev.Author != "user" {
					continue
				}
				for id := range confirmationAnswers(ev.Content) {
					seen[id]++
				}
			}
			for id := range ids {
				// The current answer is in the session already, so one is new.
				if seen[id] < 2 {
					return nil, nil
				}
			}
			return genai.NewContentFromText(AnsweredText, genai.RoleModel), nil
		},
	})
}

func confirmationAnswers(c *genai.Content) map[string]bool {
	if c == nil {
		return nil
	}
	ids := map[string]bool{}
	for _, p := range c.Parts {
		if fr := p.FunctionResponse; fr != nil && fr.Name == toolconfirmation.FunctionCallName {
			ids[fr.ID] = true
		}
	}
	return ids
}

// ErrFalseClaim is the error code of a reply that says a write waits for
// approval when nothing asked for one.
const ErrFalseClaim = "KIT_FALSE_APPROVAL_CLAIM"

// FalseClaimText replaces a reply that says a write waits for approval when no
// tool asked for one.
const FalseClaimText = "I said that something waits for your approval, but I did not ask for one, so nothing was done. Ask again."

var claimRe = regexp.MustCompile(`(?i)\b(wait|waits|waiting|awaits|awaiting|pending|needs|requires|need)\b[^.!?]{0,60}\bapprov|\bapprov\w*\b[^.!?]{0,30}\b(pending|needed|required)\b`)

// ClaimsApproval reports whether a text says that something waits for approval.
func ClaimsApproval(text string) bool { return claimRe.MatchString(text) }

// FalseClaimCheck is an AfterModelCallback. It finds a reply that says
// "waits for approval" when no confirmation is pending, and reports it as an
// error. ADK ends a run when a tool asks for approval, so a final reply with
// no tool call cannot follow a real request in the same run.
func FalseClaimCheck() llmagent.AfterModelCallback {
	return func(ctx agent.Context, resp *model.LLMResponse, err error) (*model.LLMResponse, error) {
		if err != nil || resp == nil || resp.Partial || resp.Content == nil {
			return nil, nil
		}
		var text strings.Builder
		for _, p := range resp.Content.Parts {
			if p.FunctionCall != nil {
				return nil, nil
			}
			if !p.Thought {
				text.WriteString(p.Text)
			}
		}
		if !ClaimsApproval(text.String()) || len(confirmationAnswers(ctx.UserContent())) > 0 {
			return nil, nil
		}
		slog.Warn("the model said that something waits for approval, but no tool asked for one", "invocation", ctx.InvocationID())
		out := *resp
		out.Content = genai.NewContentFromText(FalseClaimText, genai.RoleModel)
		out.ErrorCode = ErrFalseClaim
		out.ErrorMessage = "the model said that something waits for approval, but no tool asked for one"
		return &out, nil
	}
}
