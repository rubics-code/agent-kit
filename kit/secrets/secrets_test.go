package secrets

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rubics-code/agent-kit/kit/policy"
)

var pol = policy.Policy{Agent: "Example", Operator: "Sam", Infisical: policy.Infisical{
	Writable: func(p string) error {
		if !strings.HasPrefix(p, "/agents/") {
			return fmt.Errorf("%s is outside /agents", p)
		}
		return nil
	},
	EnvLabel: func(string) string { return "staging" },
}}

var at = time.Date(2026, 11, 28, 4, 5, 6, 0, time.UTC)

// container mode

func TestPutsANewSecretInTheInboxFolderWithTheLabelAndTheTime(t *testing.T) {
	store := NewMemoryStore()
	sink := Inbox(store, "/agents/example/inbox", func() time.Time { return at })
	if err := sink.Deliver(t.Context(), "mailbox hello@example.test", "pw-1"); err != nil {
		t.Fatal(err)
	}
	v, ok := store.Value(Place{Path: "/agents/example/inbox", Name: "MAILBOX_HELLO_EXAMPLE_TEST_20261128_040506"})
	if !ok || v != "pw-1" {
		t.Fatalf("%q %v", v, ok)
	}
	if sink.Where() != "the Infisical inbox folder /agents/example/inbox" {
		t.Fatal(sink.Where())
	}
}

func TestGivesANameThatInfisicalAcceptsForAnyLabel(t *testing.T) {
	for _, label := range []string{"", "  ", "42 keys", "ünïcode", strings.Repeat("x", 300), "key: ok?"} {
		name := InboxName(label, at)
		if err := CheckName(name); err != nil || len(name) > 128 {
			t.Fatalf("%q -> %q: %v", label, name, err)
		}
	}
}

func TestRefusesASecretWithNoPlaceToGo(t *testing.T) {
	err := None.Deliver(t.Context(), "a key", "v")
	if !errors.Is(err, ErrNoPlace) || !strings.Contains(err.Error(), "storeAt") {
		t.Fatal(err)
	}
	if err := Ready(None, nil, nil); !errors.Is(err, ErrNoPlace) {
		t.Fatal(err)
	}
	if err := Ready(Terminal(&bytes.Buffer{}), nil, nil); err != nil {
		t.Fatal(err)
	}
}

// deliver

func TestSendsANewSecretToTheTerminalAndNotToTheReply(t *testing.T) {
	var out bytes.Buffer
	reply, err := Deliver(t.Context(), Delivery{Policy: pol, Sink: Terminal(&out)}, "an API key", "sk-test-123")
	if err != nil || strings.Contains(reply, "sk-test-123") || !strings.Contains(out.String(), "sk-test-123") {
		t.Fatalf("%q %v %q", reply, err, out.String())
	}
	if reply != "It went directly to the operator's terminal. You do not have it." {
		t.Fatal(reply)
	}
}

func TestStoresANewSecretInTheStoreAndSaysWhere(t *testing.T) {
	store := NewMemoryStore()
	place := &Place{Path: "/agents/example", Name: "NEW_KEY"}
	reply, err := Deliver(t.Context(), Delivery{Policy: pol, Sink: Terminal(&bytes.Buffer{}), Store: store, Place: place}, "a key", "v-1")
	if err != nil || reply != "It is stored in Infisical staging /agents/example/NEW_KEY. You do not have it." {
		t.Fatalf("%q %v", reply, err)
	}
	if v, _ := store.Value(*place); v != "v-1" {
		t.Fatal(v)
	}
}

func TestSendsTheSecretToTheTerminalWhenInfisicalFailsAfterTheKeyExists(t *testing.T) {
	store := NewMemoryStore()
	store.FailPut = errors.New("403 forbidden")
	var out bytes.Buffer
	reply, err := Deliver(t.Context(), Delivery{Policy: pol, Sink: Terminal(&out), Store: store, Place: &Place{Path: "/agents/example", Name: "K"}}, "a key", "v-2")
	if err != nil || !strings.Contains(reply, "Storing it in Infisical failed (403 forbidden)") || strings.Contains(reply, "v-2") || !strings.Contains(out.String(), "v-2") {
		t.Fatalf("%q %v", reply, err)
	}
}

func TestRefusesStoreAtWhenInfisicalIsNotSetUpBeforeAnyApproval(t *testing.T) {
	if err := Ready(Terminal(&bytes.Buffer{}), nil, &Place{Path: "/agents/example", Name: "K"}); err == nil || err.Error() != NoInfisical {
		t.Fatal(err)
	}
}

func TestSaysWhenAWriteReplacesAValueOrMakesAFolder(t *testing.T) {
	store := NewMemoryStore("/agents/example")
	ctx := t.Context()
	p := Place{Path: "/agents/example", Name: "K"}
	if s, _ := DescribePlace(ctx, pol, store, p); s != "Infisical staging /agents/example/K (new)" {
		t.Fatal(s)
	}
	_ = store.Put(ctx, p, "x")
	if s, _ := DescribePlace(ctx, pol, store, p); !strings.HasSuffix(s, "(REPLACES the current value)") {
		t.Fatal(s)
	}
	if s, _ := DescribePlace(ctx, pol, store, Place{Path: "/agents/other", Name: "K"}); !strings.HasSuffix(s, "(new, makes the folder /agents/other)") {
		t.Fatal(s)
	}
}

func TestChecksAStoreAtPlaceAgainstThePolicy(t *testing.T) {
	if err := CheckPlace(pol, Place{Path: "/agents/example", Name: "GOOD_NAME"}); err != nil {
		t.Fatal(err)
	}
	if err := CheckPlace(pol, Place{Path: "/ops", Name: "GOOD_NAME"}); err == nil {
		t.Fatal("a path outside the policy passed")
	}
	if err := CheckPlace(pol, Place{Path: "/agents/example", Name: "bad-name"}); err == nil {
		t.Fatal("a bad name passed")
	}
}

func TestMakesAStrongPassword(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		p := GeneratePassword(24)
		if len(p) != 24 || seen[p] {
			t.Fatalf("%q", p)
		}
		seen[p] = true
		for _, class := range []string{`[A-Z]`, `[a-z]`, `[2-9]`, `[-_.!]`} {
			if !regexp.MustCompile(class).MatchString(p) {
				t.Fatalf("%q has no %s", p, class)
			}
		}
		if strings.ContainsAny(p, "IlO01") {
			t.Fatalf("%q has a character that looks like another", p)
		}
	}
}
