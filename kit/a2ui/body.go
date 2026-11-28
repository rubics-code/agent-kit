package a2ui

// Bodies are Kit catalog surfaces with no title. The console draws the title,
// the agent and the update time around them, in a thread and on the dashboard.

// Status is one row of a StatusList. Tone is ok, warn, idle or bad.
type Status struct {
	Label string `json:"label"`
	State string `json:"state"`
	Tone  string `json:"tone,omitempty"`
}

// TableBody is a surface with one table.
func TableBody(surfaceID string, columns []string, rows [][]string, mono ...int) []any {
	if rows == nil {
		rows = [][]string{}
	}
	c := M{"id": "root", "component": "Table", "columns": columns, "rows": rows}
	if len(mono) > 0 {
		c["mono"] = mono
	}
	return Surface(surfaceID, KitCatalog, []any{c})
}

// StatusBody is a surface with one status list.
func StatusBody(surfaceID string, items []Status) []any {
	if items == nil {
		items = []Status{}
	}
	return Surface(surfaceID, KitCatalog, []any{M{"id": "root", "component": "StatusList", "items": items}})
}

// TextBody is a surface with one paragraph.
func TextBody(surfaceID, text string) []any {
	return Surface(surfaceID, KitCatalog, []any{M{"id": "root", "component": "Text", "text": text}})
}

// ToolCard is what a tool puts in its response, under the key "card", so that
// the console shows the card in the thread.
type ToolCard struct {
	Title string `json:"title"`
	A2UI  []any  `json:"a2ui"`
}
