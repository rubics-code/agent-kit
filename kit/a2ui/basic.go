package a2ui

import (
	"github.com/rubics-code/agent-kit/kit/approval"
)

// BasicApprovalCard is the approval card in the A2UI basic catalog. Every
// A2UI renderer can draw it.
func BasicApprovalCard(confirmationID string, ap approval.Approval) []any {
	ctx := M{"confirmationId": confirmationID, "argsHash": ap.ArgsHash}
	button := func(id, label, action, variant string) []any {
		return []any{
			M{"id": id, "component": "Button", "child": id + "_label", "variant": variant,
				"action": M{"event": M{"name": action, "context": ctx}}},
			M{"id": id + "_label", "component": "Text", "text": label},
		}
	}
	components := []any{
		M{"id": "root", "component": "Card", "child": "body"},
		M{"id": "body", "component": "Column", "children": []any{"title", "summary", "hash", "buttons"}},
		M{"id": "title", "component": "Text", "text": "Approval: " + ap.Tool},
		M{"id": "summary", "component": "Text", "text": ap.Summary},
		M{"id": "hash", "component": "Text", "text": "Args hash: " + ap.ArgsHash},
		M{"id": "buttons", "component": "Row", "children": []any{"approve", "reject"}},
	}
	components = append(components, button("approve", "Approve", ActionApprove, "primary")...)
	components = append(components, button("reject", "Reject", ActionReject, "default")...)
	return Surface("approval-"+confirmationID, BasicCatalog, components)
}
