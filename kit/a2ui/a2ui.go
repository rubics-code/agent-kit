// Package a2ui builds A2UI v1.0 messages and carries them over A2A.
package a2ui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"google.golang.org/genai"

	adka2a "google.golang.org/adk/v2/server/adka2a/v2"
	"google.golang.org/adk/v2/tool/toolconfirmation"

	"github.com/rubics-code/agent-kit/kit/approval"
)

const (
	ExtensionURI = "https://a2ui.org/a2a-extension/a2ui/v1.0"
	MIMEType     = "application/a2ui+json"
	BasicCatalog = "https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json"

	ActionApprove = "kit.approve"
	ActionReject  = "kit.reject"
)

// M is one JSON object in an A2UI message.
type M = map[string]any

// Part wraps A2UI messages in one A2A data part.
func Part(messages []any) *a2a.Part {
	p := a2a.NewDataPart(messages)
	p.Metadata = map[string]any{"mimeType": MIMEType}
	return p
}

// Surface gives the createSurface and updateComponents messages for one surface.
func Surface(id, catalog string, components []any) []any {
	return []any{
		M{"version": "1.0", "createSurface": M{"surfaceId": id, "catalogId": catalog}},
		M{"version": "1.0", "updateComponents": M{"surfaceId": id, "components": components}},
	}
}

// Extension is the A2A agent card entry for A2UI v1.0.
func Extension(catalogs ...string) a2a.AgentExtension {
	return a2a.AgentExtension{
		URI:         ExtensionURI,
		Description: "Ability to render A2UI v1.0",
		Params:      map[string]any{"supportedCatalogIds": catalogs, "acceptsInlineCatalogs": false},
	}
}

// Renders reports whether the client message says that it renders one of the catalogs.
func Renders(m *a2a.Message, catalogs ...string) bool {
	if m == nil {
		return false
	}
	caps, _ := m.Metadata["a2uiRendererCapabilities"].(map[string]any)
	v1, _ := caps["1.0"].(map[string]any)
	ids, _ := v1["supportedCatalogIds"].([]any)
	for _, c := range catalogs {
		if slices.Contains(ids, any(c)) {
			return true
		}
	}
	return false
}

func decode[T any](v any) (T, error) {
	var out T
	b, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(b, &out)
	return out, err
}

// CardBuilder makes the A2UI messages for one pending confirmation.
type CardBuilder func(confirmationID string, ap approval.Approval) []any

// ApprovalCards gives an AfterExecuteCallback that puts an A2UI card next to
// each confirmation in an input-required status. It adds a card only when the
// client renders the catalog.
func ApprovalCards(catalog string, build CardBuilder) adka2a.AfterExecuteCallback {
	return func(ctx adka2a.ExecutorContext, ev *a2a.TaskStatusUpdateEvent, err error) error {
		if err != nil || ev == nil || ev.Status.State != a2a.TaskStateInputRequired || ev.Status.Message == nil {
			return err
		}
		if !Renders(ctx.RequestContext().Message, catalog) {
			return nil
		}
		var cards []*a2a.Part
		for _, p := range ev.Status.Message.Parts {
			id, ap, ok := PendingApproval(p)
			if ok {
				cards = append(cards, Part(build(id, ap)))
			}
		}
		ev.Status.Message.Parts = append(ev.Status.Message.Parts, cards...)
		return nil
	}
}

// PendingApproval reads the Kit approval from an adk_request_confirmation part.
func PendingApproval(p *a2a.Part) (string, approval.Approval, bool) {
	fc, err := decode[genai.FunctionCall](p.Data())
	if err != nil || fc.Name != toolconfirmation.FunctionCallName {
		return "", approval.Approval{}, false
	}
	tc, err := decode[struct {
		Payload approval.Approval `json:"payload"`
	}](fc.Args["toolConfirmation"])
	if err != nil || tc.Payload.ArgsHash == "" {
		return "", approval.Approval{}, false
	}
	return fc.ID, tc.Payload, true
}

type action struct {
	Action *struct {
		Name    string         `json:"name"`
		Context map[string]any `json:"context"`
	} `json:"action"`
}

// ToGenAI is an A2APartConverter. It turns an A2UI Approve or Reject action
// into the confirmation response that ADK expects. Other A2UI actions reach
// the model as text. Other parts use ADK's converter.
func ToGenAI(_ context.Context, _ a2a.Event, p *a2a.Part) (*genai.Part, error) {
	if p.Metadata["mimeType"] != MIMEType {
		return adka2a.ToGenAIPart(p)
	}
	actions, err := decode[[]action](p.Data())
	if err != nil {
		return nil, fmt.Errorf("bad A2UI part: %w", err)
	}
	for _, a := range actions {
		if a.Action == nil || (a.Action.Name != ActionApprove && a.Action.Name != ActionReject) {
			continue
		}
		id, _ := a.Action.Context["confirmationId"].(string)
		return &genai.Part{FunctionResponse: &genai.FunctionResponse{
			ID:   id,
			Name: toolconfirmation.FunctionCallName,
			Response: map[string]any{
				"confirmed": a.Action.Name == ActionApprove,
				"payload":   map[string]any{"argsHash": a.Action.Context["argsHash"]},
			},
		}}, nil
	}
	b, _ := json.Marshal(p.Data())
	return genai.NewPartFromText("A2UI event from the client: " + string(b)), nil
}
