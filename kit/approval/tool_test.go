package approval_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	"github.com/rubics-code/agent-kit/kit/approval"
	"github.com/rubics-code/agent-kit/kit/group"
	"github.com/rubics-code/agent-kit/kit/internal/kittest"
	"github.com/rubics-code/agent-kit/kit/policy"
)

// A sample mail service, with a create tool that makes a password.
type mailbox struct {
	mu        sync.Mutex
	writes    []string
	delivered []string
	price     int
}

type createArgs struct {
	Domain   string `json:"domain"`
	Username string `json:"username"`
	QuotaMB  int    `json:"quotaMb,omitempty"`
}

var pol = policy.Policy{Agent: "Example", Operator: "Sam"}

func (m *mailbox) tools(t *testing.T) []tool.Tool {
	t.Helper()
	list, err := functiontool.New(functiontool.Config{Name: "mail_list", Description: "List mailboxes."},
		func(agent.Context, struct{}) (map[string]any, error) {
			return map[string]any{"mailboxes": []string{"ada", "lin"}}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	create, err := approval.Tool(approval.Spec[createArgs, int]{
		Name: "mail_create", Description: "Create a mailbox.", Policy: pol,
		Prepare: func(_ agent.Context, a createArgs) (approval.Prep[int], error) {
			q := a.QuotaMB
			if q == 0 {
				q = 1024
			}
			if a.Username == "postmaster" {
				return approval.Prep[int]{Early: "postmaster exists already."}, nil
			}
			m.mu.Lock()
			price := m.price
			m.mu.Unlock()
			return approval.Prep[int]{
				Summary: fmt.Sprintf("Create the mailbox %s@%s (quota %d MB, price %d)", a.Username, a.Domain, q, price),
				Args:    map[string]any{"domain": a.Domain, "username": a.Username, "quotaMb": q, "price": price},
				Data:    q,
			}, nil
		},
		Run: func(_ agent.Context, a createArgs, q int) (any, error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.writes = append(m.writes, a.Username)
			m.delivered = append(m.delivered, "pw-SECRET-"+a.Username)
			return "Created " + a.Username + "@" + a.Domain + ". The password went to Sam's terminal. You do not have it.", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return []tool.Tool{list, create}
}

func setup(t *testing.T, script ...*genai.Content) (*mailbox, *kittest.Harness) {
	m := &mailbox{}
	guard, err := approval.ReplayGuard()
	if err != nil {
		t.Fatal(err)
	}
	h := kittest.New(t, kittest.Script(script...), m.tools(t), kittest.Config{Plugins: []*plugin.Plugin{guard}})
	return m, h
}

func call(id, name string, args map[string]any) *genai.FunctionCall {
	return &genai.FunctionCall{ID: id, Name: name, Args: args}
}

var createHello = call("c1", "mail_create", map[string]any{"domain": "example.test", "username": "hello"})

func payload(t *testing.T, fc *genai.FunctionCall) map[string]any {
	t.Helper()
	b, _ := json.Marshal(fc.Args["toolConfirmation"])
	var tc struct {
		Hint    string         `json:"hint"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(b, &tc); err != nil {
		t.Fatal(err)
	}
	tc.Payload["hint"] = tc.Hint
	return tc.Payload
}

func allText(evs []*session.Event) string {
	b, _ := json.Marshal(evs)
	return string(b)
}

func TestRunsAReadToolWithNoApproval(t *testing.T) {
	_, h := setup(t, kittest.Call(call("r1", "mail_list", nil)), kittest.Text("ada and lin"))
	evs := h.Say("who has a mailbox?")
	if len(kittest.Confirmations(evs)) != 0 {
		t.Fatal("a read asked for approval")
	}
	if got := kittest.Texts(evs); got[len(got)-1] != "ada and lin" {
		t.Fatalf("%v", got)
	}
}

func TestStopsBeforeAWriteAndShowsASummaryMadeByCode(t *testing.T) {
	m, h := setup(t, kittest.Call(createHello), kittest.Text("done"))
	evs := h.Say("make hello@")
	cs := kittest.Confirmations(evs)
	if len(cs) != 1 {
		t.Fatalf("%d confirmations", len(cs))
	}
	p := payload(t, cs[0])
	if p["tool"] != "mail_create" || p["summary"] != "Create the mailbox hello@example.test (quota 1024 MB, price 0)" || p["hint"] != p["summary"] {
		t.Fatalf("%+v", p)
	}
	if len(m.writes) != 0 || h.Model.Calls != 1 {
		t.Fatalf("writes %v, model calls %d", m.writes, h.Model.Calls)
	}
}

func approve(t *testing.T, h *kittest.Harness, fc *genai.FunctionCall) []*session.Event {
	return h.Send(kittest.Answer(true, map[string]map[string]any{fc.ID: {"argsHash": payload(t, fc)["argsHash"]}}))
}

func TestWritesOnceAfterApprovalAndThePasswordNeverEntersTheConversation(t *testing.T) {
	m, h := setup(t, kittest.Call(createHello), kittest.Text("hello@ is made"))
	cs := kittest.Confirmations(h.Say("make hello@"))
	evs := approve(t, h, cs[0])
	if len(m.writes) != 1 || m.writes[0] != "hello" {
		t.Fatalf("writes %v", m.writes)
	}
	if strings.Contains(allText(evs), "pw-SECRET") || len(m.delivered) != 1 {
		t.Fatal("the password is in the conversation")
	}
	if got := kittest.Texts(evs); got[len(got)-1] != "hello@ is made" {
		t.Fatalf("%v", got)
	}
}

func TestDoesNotWriteWhenTheOperatorRejects(t *testing.T) {
	m, h := setup(t, kittest.Call(createHello), kittest.Text("ok, not made"))
	cs := kittest.Confirmations(h.Say("make hello@"))
	evs := h.Send(kittest.Answer(false, map[string]map[string]any{cs[0].ID: {"reason": "not now"}}))
	if len(m.writes) != 0 {
		t.Fatalf("writes %v", m.writes)
	}
	r := kittest.Responses(evs)["mail_create"]
	if len(r) != 1 || r[0]["result"] != "Not done: Sam did not approve (not now). Do not try again unless Sam asks." {
		t.Fatalf("%+v", r)
	}
}

func TestRunsAToolWithNoArguments(t *testing.T) {
	_, h := setup(t, kittest.Call(call("r1", "mail_list", map[string]any{})), kittest.Text("two"))
	if r := kittest.Responses(h.Say("list")); len(r["mail_list"]) != 1 || r["mail_list"][0]["error"] != nil {
		t.Fatalf("%+v", r)
	}
}

func TestDoesNotWriteWhenTheApprovalIsForOtherArguments(t *testing.T) {
	m, h := setup(t, kittest.Call(createHello), kittest.Text("not made"))
	cs := kittest.Confirmations(h.Say("make hello@"))
	evs := h.Send(kittest.Answer(true, map[string]map[string]any{cs[0].ID: {"argsHash": "sha256:other"}}))
	r := kittest.Responses(evs)["mail_create"]
	if len(m.writes) != 0 || len(r) != 1 || !strings.Contains(r[0]["result"].(string), "the approval does not match these arguments") {
		t.Fatalf("writes %v, %+v", m.writes, r)
	}
}

func TestDoesNotWriteWhenThePriceChangesAfterApproval(t *testing.T) {
	m, h := setup(t, kittest.Call(createHello), kittest.Text("not made"))
	cs := kittest.Confirmations(h.Say("make hello@"))
	m.price = 12
	approve(t, h, cs[0])
	if len(m.writes) != 0 {
		t.Fatalf("wrote at a new price: %v", m.writes)
	}
}

func TestAsksForEachWriteWhenTheModelCallsTwoAtOnce(t *testing.T) {
	two := kittest.Call(createHello, call("c2", "mail_create", map[string]any{"domain": "example.test", "username": "info"}))
	m, h := setup(t, two, kittest.Text("both made"))
	cs := kittest.Confirmations(h.Say("make hello@ and info@"))
	if len(cs) != 2 {
		t.Fatalf("%d confirmations", len(cs))
	}
	answers := map[string]map[string]any{}
	for _, c := range cs {
		answers[c.ID] = map[string]any{"argsHash": payload(t, c)["argsHash"]}
	}
	h.Send(kittest.Answer(true, answers))
	if len(m.writes) != 2 {
		t.Fatalf("writes %v", m.writes)
	}
}

func TestRefusesArgumentsOutsideTheSchemaBeforeAnyApproval(t *testing.T) {
	bad := call("c1", "mail_create", map[string]any{"domain": "example.test", "username": "hello", "quotaMb": "lots"})
	m, h := setup(t, kittest.Call(bad), kittest.Text("I could not"))
	evs := h.Say("make hello@")
	if len(kittest.Confirmations(evs)) != 0 || len(m.writes) != 0 {
		t.Fatal("bad arguments reached the gate")
	}
	if r := kittest.Responses(evs)["mail_create"]; len(r) != 1 || r[0]["error"] == nil {
		t.Fatalf("%+v", r)
	}
}

func TestGivesAnEarlyAnswerWithNoApproval(t *testing.T) {
	pm := call("c1", "mail_create", map[string]any{"domain": "example.test", "username": "postmaster"})
	_, h := setup(t, kittest.Call(pm), kittest.Text("it exists"))
	evs := h.Say("make postmaster@")
	if len(kittest.Confirmations(evs)) != 0 || kittest.Responses(evs)["mail_create"][0]["result"] != "postmaster exists already." {
		t.Fatalf("%+v", kittest.Responses(evs))
	}
}

func TestDropsASecondAnswerBeforeTheModel(t *testing.T) {
	m, h := setup(t, kittest.Call(createHello), kittest.Text("made"), kittest.Text("this must not come"))
	cs := kittest.Confirmations(h.Say("make hello@"))
	approve(t, h, cs[0])
	calls := h.Model.Calls
	evs := approve(t, h, cs[0])
	if h.Model.Calls != calls || len(m.writes) != 1 {
		t.Fatalf("model calls %d -> %d, writes %v", calls, h.Model.Calls, m.writes)
	}
	if got := kittest.Texts(evs); len(got) != 1 || got[0] != approval.AnsweredText {
		t.Fatalf("%v", got)
	}
}

func TestTheCatalogSeesTheGateOnTheTool(t *testing.T) {
	m := &mailbox{}
	cat := group.Catalog(m.tools(t))
	if cat[0].Approval || !cat[1].Approval {
		t.Fatalf("%+v", cat)
	}
}

func TestReportsAFalseApprovalClaim(t *testing.T) {
	m := &mailbox{}
	h := kittest.New(t, kittest.Script(kittest.Text("The mailbox hello@ is waiting for your approval.")), m.tools(t),
		kittest.Config{After: []llmagent.AfterModelCallback{approval.FalseClaimCheck()}})
	evs := h.Say("make hello@")
	var code string
	for _, ev := range evs {
		if ev.ErrorCode != "" {
			code = ev.ErrorCode
		}
	}
	if code != approval.ErrFalseClaim {
		t.Fatalf("no error: %v", kittest.Texts(evs))
	}
	if got := kittest.Texts(evs); len(got) != 1 || got[0] != approval.FalseClaimText {
		t.Fatalf("%v", got)
	}
}

func TestDoesNotFlagARealRequest(t *testing.T) {
	m := &mailbox{}
	both := kittest.Call(createHello)
	both.Parts = append([]*genai.Part{{Text: "This needs your approval."}}, both.Parts...)
	h := kittest.New(t, kittest.Script(both, kittest.Text("Approved and made.")), m.tools(t),
		kittest.Config{After: []llmagent.AfterModelCallback{approval.FalseClaimCheck()}})
	evs := h.Say("make hello@")
	cs := kittest.Confirmations(evs)
	for _, ev := range append(evs, approve(t, h, cs[0])...) {
		if ev.ErrorCode != "" {
			t.Fatalf("flagged: %s", ev.ErrorCode)
		}
	}
}

func TestClaimsApproval(t *testing.T) {
	for s, want := range map[string]bool{
		"It's waiting for your approval.":      true,
		"This write needs approval from you.":  true,
		"Approval is pending.":                 true,
		"The note is written.":                 false,
		"You approved it, and it is done.":     false,
		"I need to know the name. Then I ask.": false,
	} {
		if approval.ClaimsApproval(s) != want {
			t.Errorf("%q: want %v", s, want)
		}
	}
}
