package secrets_test

import (
	"testing"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"

	"github.com/rubics-code/agent-kit/kit/approval"
	"github.com/rubics-code/agent-kit/kit/internal/kittest"
	"github.com/rubics-code/agent-kit/kit/policy"
	"github.com/rubics-code/agent-kit/kit/secrets"
)

type keyArgs struct {
	Name string `json:"name"`
}

// A tool that makes a secret calls secrets.Ready in Prepare, so it refuses
// before any approval when the secret has no place to go.
func keyTool(t *testing.T, sink secrets.Sink, made *int) tool.Tool {
	tl, err := approval.Tool(approval.Spec[keyArgs, struct{}]{
		Name: "key_create", Description: "Make an API key.", Policy: policy.Policy{Operator: "Sam"},
		Prepare: func(_ agent.Context, a keyArgs) (approval.Prep[struct{}], error) {
			if err := secrets.Ready(sink, nil, nil); err != nil {
				return approval.Prep[struct{}]{Early: err.Error()}, nil
			}
			return approval.Prep[struct{}]{Summary: "Make the key " + a.Name}, nil
		},
		Run: func(ctx agent.Context, a keyArgs, _ struct{}) (any, error) {
			*made++
			return secrets.Deliver(ctx, secrets.Delivery{Sink: sink}, a.Name, "sk-test")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

func TestAToolThatMakesASecretWithNoPlaceRefusesBeforeApproval(t *testing.T) {
	made := 0
	h := kittest.New(t, kittest.Script(
		kittest.Call(&genai.FunctionCall{ID: "k1", Name: "key_create", Args: map[string]any{"name": "ci"}}),
		kittest.Text("I cannot."),
	), []tool.Tool{keyTool(t, secrets.None, &made)}, kittest.Config{})
	evs := h.Say("make a key")
	r := kittest.Responses(evs)["key_create"]
	if len(kittest.Confirmations(evs)) != 0 || made != 0 || len(r) != 1 {
		t.Fatalf("confirmations %d, made %d, %+v", len(kittest.Confirmations(evs)), made, r)
	}
	if s, _ := r[0]["result"].(string); s == "" {
		t.Fatalf("%+v", r)
	}
}
