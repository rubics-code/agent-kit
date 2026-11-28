package policy

import "testing"

func TestRejectedCopyComesFromThePolicy(t *testing.T) {
	got := Policy{Operator: "Sam"}.Rejected("too costly")
	if got != "Not done: Sam did not approve (too costly). Do not try again unless Sam asks." {
		t.Fatal(got)
	}
	if got := (Policy{}).Rejected(""); got != "Not done: the operator did not approve (rejected). Do not try again unless the operator asks." {
		t.Fatal(got)
	}
}
