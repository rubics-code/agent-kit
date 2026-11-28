package approval

import "testing"

type args struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func TestArgsHashIsStableAndBindsToArgs(t *testing.T) {
	a := args{Title: "t", Body: "b"}
	h1, _ := ArgsHash("write_note", a)
	h2, _ := ArgsHash("write_note", map[string]any{"body": "b", "title": "t"})
	if h1 != h2 {
		t.Fatalf("struct and map give other hashes: %s %s", h1, h2)
	}
	if h3, _ := ArgsHash("other_tool", a); h3 == h1 {
		t.Fatal("the tool name is not in the hash")
	}
	if err := CheckBinding("write_note", a, map[string]any{"argsHash": h1}); err != nil {
		t.Fatal(err)
	}
	if err := CheckBinding("write_note", args{Title: "t", Body: "B"}, map[string]any{"argsHash": h1}); err == nil {
		t.Fatal("changed args passed the check")
	}
	if err := CheckBinding("write_note", a, map[string]any{}); err == nil {
		t.Fatal("missing hash passed the check")
	}
	if err := CheckBinding("write_note", a, "not a map"); err == nil {
		t.Fatal("bad payload passed the check")
	}
}
