package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"google.golang.org/genai"

	adka2a "google.golang.org/adk/v2/server/adka2a/v2"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

const (
	a2uiExtensionURI = "https://a2ui.org/a2a-extension/a2ui/v1.0"
	a2uiMIMEType     = "application/a2ui+json"
	a2uiBasicCatalog = "https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json"
	actionApprove    = "kit.approve"
	actionReject     = "kit.reject"
)

type msg = map[string]any

func a2uiPart(messages []any) *a2a.Part {
	p := a2a.NewDataPart(messages)
	p.Metadata = map[string]any{"mimeType": a2uiMIMEType}
	return p
}

// ApprovalCard gives the A2UI v1.0 messages for one pending confirmation. The
// text on the card comes from the Approval that code made, never from the model.
func ApprovalCard(confirmationID string, ap Approval) []any {
	sid := "approval-" + confirmationID
	ctx := msg{"confirmationId": confirmationID, "argsHash": ap.ArgsHash}
	button := func(id, label, action, variant string) []any {
		return []any{
			msg{"id": id, "component": "Button", "child": id + "_label", "variant": variant,
				"action": msg{"event": msg{"name": action, "context": ctx}}},
			msg{"id": id + "_label", "component": "Text", "text": label},
		}
	}
	components := []any{
		msg{"id": "root", "component": "Card", "child": "body"},
		msg{"id": "body", "component": "Column", "children": []any{"title", "summary", "hash", "buttons"}},
		msg{"id": "title", "component": "Text", "text": "Approve " + ap.Tool + "?"},
		msg{"id": "summary", "component": "Text", "text": ap.Summary},
		msg{"id": "hash", "component": "Text", "text": "Args hash: " + ap.ArgsHash},
		msg{"id": "buttons", "component": "Row", "children": []any{"approve", "reject"}},
	}
	components = append(components, button("approve", "Approve", actionApprove, "primary")...)
	components = append(components, button("reject", "Reject", actionReject, "default")...)
	return []any{
		msg{"version": "1.0", "createSurface": msg{"surfaceId": sid, "catalogId": a2uiBasicCatalog}},
		msg{"version": "1.0", "updateComponents": msg{"surfaceId": sid, "components": components}},
	}
}

func rendersA2UI(m *a2a.Message) bool {
	if m == nil {
		return false
	}
	caps, _ := m.Metadata["a2uiRendererCapabilities"].(map[string]any)
	v1, _ := caps["1.0"].(map[string]any)
	ids, _ := v1["supportedCatalogIds"].([]any)
	return slices.Contains(ids, any(a2uiBasicCatalog))
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

// addApprovalCards puts an A2UI card next to each confirmation in an
// input-required status, when the client said that it renders A2UI v1.0.
func addApprovalCards(ctx adka2a.ExecutorContext, ev *a2a.TaskStatusUpdateEvent, err error) error {
	if err != nil || ev == nil || ev.Status.State != a2a.TaskStateInputRequired || ev.Status.Message == nil {
		return err
	}
	if !rendersA2UI(ctx.RequestContext().Message) {
		return nil
	}
	var cards []*a2a.Part
	for _, p := range ev.Status.Message.Parts {
		fc, derr := decode[genai.FunctionCall](p.Data())
		if derr != nil || fc.Name != toolconfirmation.FunctionCallName {
			continue
		}
		tc, derr := decode[struct {
			Payload Approval `json:"payload"`
		}](fc.Args["toolConfirmation"])
		if derr != nil || tc.Payload.ArgsHash == "" {
			continue
		}
		cards = append(cards, a2uiPart(ApprovalCard(fc.ID, tc.Payload)))
	}
	ev.Status.Message.Parts = append(ev.Status.Message.Parts, cards...)
	return nil
}

type a2uiAction struct {
	Version string `json:"version"`
	Action  *struct {
		Name              string         `json:"name"`
		SurfaceID         string         `json:"surfaceId"`
		SourceComponentID string         `json:"sourceComponentId"`
		Context           map[string]any `json:"context"`
	} `json:"action"`
}

// a2uiToGenAI turns an A2UI Approve or Reject action from the client into the
// confirmation response that ADK expects. Other parts use ADK's converter.
func a2uiToGenAI(_ context.Context, _ a2a.Event, p *a2a.Part) (*genai.Part, error) {
	if p.Metadata["mimeType"] != a2uiMIMEType {
		return adka2a.ToGenAIPart(p)
	}
	actions, err := decode[[]a2uiAction](p.Data())
	if err != nil {
		return nil, fmt.Errorf("bad A2UI part: %w", err)
	}
	for _, a := range actions {
		if a.Action == nil || (a.Action.Name != actionApprove && a.Action.Name != actionReject) {
			continue
		}
		id, _ := a.Action.Context["confirmationId"].(string)
		return &genai.Part{FunctionResponse: &genai.FunctionResponse{
			ID:   id,
			Name: toolconfirmation.FunctionCallName,
			Response: map[string]any{
				"confirmed": a.Action.Name == actionApprove,
				"payload":   map[string]any{"argsHash": a.Action.Context["argsHash"]},
			},
		}}, nil
	}
	b, _ := json.Marshal(p.Data())
	return genai.NewPartFromText("A2UI event from the client: " + string(b)), nil
}
