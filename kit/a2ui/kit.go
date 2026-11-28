package a2ui

import (
	"fmt"
	"sort"

	"github.com/rubics-code/agent-kit/kit/approval"
)

// KitCatalog is the id of the Kit's own A2UI catalog. The web client in
// web/src/a2ui/kit.tsx renders it.
const KitCatalog = "https://github.com/rubics-code/agent-kit/catalogs/kit/v1"

// Entry is one label and value in a Fields or Approval component.
type Entry struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Mono  bool   `json:"mono,omitempty"`
}

// Entries gives the arguments as entries, sorted by name.
func Entries(args map[string]any) []Entry {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Entry, 0, len(keys))
	for _, k := range keys {
		v, ok := args[k].(string)
		if !ok {
			v = fmt.Sprint(args[k])
		}
		out = append(out, Entry{Label: k, Value: v, Mono: true})
	}
	return out
}

// KitApprovalCard is the approval card in the Kit catalog.
func KitApprovalCard(confirmationID string, ap approval.Approval) []any {
	ctx := M{"confirmationId": confirmationID, "argsHash": ap.ArgsHash}
	event := func(name string) M { return M{"event": M{"name": name, "context": ctx}} }
	return Surface("approval-"+confirmationID, KitCatalog, []any{M{
		"id": "root", "component": "Approval",
		"tool": ap.Tool, "summary": ap.Summary, "argsHash": ap.ArgsHash, "entries": Entries(ap.Args),
		"approve": event(ActionApprove), "reject": event(ActionReject),
	}})
}

// Choice pairs a catalog with the approval card in that catalog.
type Choice struct {
	Catalog string
	Build   CardBuilder
}

// DefaultChoices sends the Kit card to a client that renders the Kit
// catalog, and the basic card to other A2UI clients.
var DefaultChoices = []Choice{{KitCatalog, KitApprovalCard}, {BasicCatalog, BasicApprovalCard}}
