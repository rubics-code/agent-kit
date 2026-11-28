package approval

import (
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	"github.com/rubics-code/agent-kit/kit/policy"
)

// Prep is what a write tool's Prepare gives the gate.
type Prep[D any] struct {
	// Summary is the approval text. Code writes it from the arguments.
	Summary string
	// Args is what the gate hashes, when it differs from the raw arguments
	// (for example a live price that Prepare read). Nil means the raw arguments.
	Args any
	// Data goes to Run.
	Data D
	// Early, when it is not empty, is the tool's answer with no approval
	// (nothing to do, or a precondition failed).
	Early string
}

// Spec is a write tool. The gate is not optional: Run is called only after a
// person approved the exact arguments.
type Spec[A, D any] struct {
	Name        string
	Description string
	// InputSchema, when it is not nil, replaces the schema that ADK infers
	// from A. ADK checks each call against it before Prepare.
	InputSchema *jsonschema.Schema
	// Policy gives the rejection copy.
	Policy policy.Policy
	// Prepare does the reads that the summary needs. It runs again when the
	// person answers, so it must have no side effect.
	Prepare func(ctx agent.Context, args A) (Prep[D], error)
	Run     func(ctx agent.Context, args A, data D) (any, error)
}

type fnTool interface {
	tool.Tool
	Declaration() *genai.FunctionDeclaration
	Run(ctx agent.Context, args any) (map[string]any, error)
	ProcessRequest(ctx agent.Context, req *model.LLMRequest) error
}

// gated is an ADK function tool that a person must approve.
type gated struct {
	fnTool
	policy policy.Policy
}

func (gated) RequiresApproval() bool { return true }

// Run answers a rejection with the policy's copy, before the inner tool runs.
// ADK's own answer would be a bare error.
func (g gated) Run(ctx agent.Context, args any) (map[string]any, error) {
	if c := ctx.ToolConfirmation(); c != nil && !c.Confirmed {
		reason, _ := payloadMap(c.Payload)["reason"].(string)
		return map[string]any{"result": g.policy.Rejected(reason)}, nil
	}
	return g.fnTool.Run(ctx, args)
}

func payloadMap(p any) map[string]any {
	if m, ok := p.(map[string]any); ok {
		return m
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

// Tool makes a write tool behind the gate.
func Tool[A, D any](s Spec[A, D]) (tool.Tool, error) {
	if s.Prepare == nil || s.Run == nil {
		return nil, fmt.Errorf("the write tool %s needs Prepare and Run", s.Name)
	}
	inner, err := functiontool.New(functiontool.Config{Name: s.Name, Description: s.Description, InputSchema: s.InputSchema},
		func(ctx agent.Context, a A) (map[string]any, error) {
			pre, err := s.Prepare(ctx, a)
			if err != nil {
				return nil, err
			}
			if pre.Early != "" {
				return map[string]any{"result": pre.Early}, nil
			}
			var hashed any = a
			if pre.Args != nil {
				hashed = pre.Args
			}
			d, err := Gate(ctx, s.Name, pre.Summary, hashed)
			switch {
			case d == Pending && err == nil:
				return map[string]any{"status": "waiting for approval"}, nil
			case d == Rejected && err != nil:
				return map[string]any{"result": s.Policy.Rejected("the approval does not match these arguments")}, nil
			case err != nil:
				return nil, err
			case d == Rejected:
				return map[string]any{"result": s.Policy.Rejected("")}, nil
			}
			out, err := s.Run(ctx, a, pre.Data)
			if err != nil {
				return nil, err
			}
			return toMap(out)
		})
	if err != nil {
		return nil, err
	}
	f, ok := inner.(fnTool)
	if !ok {
		return nil, fmt.Errorf("the ADK function tool %s has an unexpected type %T", s.Name, inner)
	}
	return gated{fnTool: f, policy: s.Policy}, nil
}

func toMap(v any) (map[string]any, error) {
	switch x := v.(type) {
	case nil:
		return map[string]any{"result": "done"}, nil
	case string:
		return map[string]any{"result": x}, nil
	case map[string]any:
		return x, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if json.Unmarshal(b, &m) == nil && m != nil {
		return m, nil
	}
	return map[string]any{"result": json.RawMessage(b)}, nil
}
