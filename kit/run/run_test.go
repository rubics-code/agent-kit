package run

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"

	"github.com/rubics-code/agent-kit/kit/approval"
	"github.com/rubics-code/agent-kit/kit/credentials"
	"github.com/rubics-code/agent-kit/kit/fakemodel"
	"github.com/rubics-code/agent-kit/kit/group"
	"github.com/rubics-code/agent-kit/kit/policy"
	"github.com/rubics-code/agent-kit/kit/secrets"
	"github.com/rubics-code/agent-kit/kit/transcribe"
)

type note struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type notebook struct {
	mu     sync.Mutex
	writes []note
}

func (n *notebook) tools(k Kit) ([]tool.Tool, error) {
	w, err := approval.Tool(approval.Spec[note, struct{}]{
		Name: "write_note", Description: "Write a note.", Policy: k.Policy,
		Prepare: func(_ agent.Context, a note) (approval.Prep[struct{}], error) {
			return approval.Prep[struct{}]{Summary: fmt.Sprintf("Write the note %q", a.Title)}, nil
		},
		Run: func(_ agent.Context, a note, _ struct{}) (any, error) {
			n.mu.Lock()
			defer n.mu.Unlock()
			n.writes = append(n.writes, a)
			return map[string]any{"status": "written", "title": a.Title}, nil
		},
	})
	return []tool.Tool{w}, err
}

func config(t *testing.T, n *notebook) Config {
	return Config{
		Name: "Example", Description: "A test agent.", Model: "test/model", LLM: fakemodel.Model{},
		Transcriber: transcribe.Fake{Text: "say hello"},
		System:      "You are {{agent}}. {{operator}} approves each change.",
		Policy:      &policy.Policy{Agent: "Example", Operator: "Sam"},
		Tools:       n.tools,
		Addr:        "127.0.0.1:0",
		DB:          filepath.Join(t.TempDir(), "agent.db"),
		Origins:     []string{"http://localhost:5173"},
	}
}

func build(t *testing.T, cfg Config, e credentials.Environ) (*Agent, *bytes.Buffer) {
	t.Helper()
	var errOut bytes.Buffer
	a, err := Build(t.Context(), cfg, e, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a, &errOut
}

func do(h http.Handler, method, path string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, body)
	r.Host = "127.0.0.1:4141"
	for k, v := range hdr {
		if k == "Host" {
			r.Host = v
			continue
		}
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// web chat server

func TestRefusesARequestFromAnotherOrigin(t *testing.T) {
	a, _ := build(t, config(t, &notebook{}), credentials.Environ{})
	if w := do(a.Handler, "GET", "/kit/info", nil, map[string]string{"Origin": "http://evil.test"}); w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestAcceptsTheOwnOriginAListedOriginAndARequestWithNoOrigin(t *testing.T) {
	a, _ := build(t, config(t, &notebook{}), credentials.Environ{})
	for _, o := range []string{"", "http://127.0.0.1:4141", "http://localhost:5173"} {
		if w := do(a.Handler, "GET", "/kit/info", nil, map[string]string{"Origin": o}); w.Code != 200 {
			t.Fatalf("%q: %d", o, w.Code)
		}
	}
	w := do(a.Handler, "OPTIONS", "/a2a", nil, map[string]string{"Origin": "http://localhost:5173"})
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
}

func TestRefusesARequestForAnotherHostName(t *testing.T) {
	a, _ := build(t, config(t, &notebook{}), credentials.Environ{})
	if w := do(a.Handler, "GET", "/", nil, map[string]string{"Host": "rebind.example.test:4141"}); w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestRefusesAFormPost(t *testing.T) {
	a, _ := build(t, config(t, &notebook{}), credentials.Environ{})
	for _, ct := range []string{"application/x-www-form-urlencoded", "text/plain", "multipart/form-data; boundary=x"} {
		if w := do(a.Handler, "POST", "/a2a", strings.NewReader("x"), map[string]string{"Content-Type": ct}); w.Code != 415 {
			t.Fatalf("%s: %d", ct, w.Code)
		}
	}
}

func TestGivesTheModelTheGroupsAndTheToolsWithNoKey(t *testing.T) {
	cfg := config(t, &notebook{})
	cfg.Groups = []group.Group{group.New(group.Def[string]{ID: "mail", Name: "Mail", Keys: []string{"MAIL_KEY"}, Setup: "Add MAIL_KEY.",
		Connect: func(c group.ConnectContext) (string, bool) { return c.Get("MAIL_KEY"), c.Holds("MAIL_KEY") },
		Tools:   func(string, group.Shared) ([]tool.Tool, error) { return nil, nil }})}
	a, _ := build(t, cfg, credentials.Environ{"OPENROUTER_API_KEY": "sk-secret-value"})
	w := do(a.Handler, "GET", "/kit/info", nil, nil)
	var info struct {
		Name, Model, Keys string
		Groups            []group.Status
		Tools             []string
	}
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Name != "Example" || info.Model != "test/model" || info.Keys != "env" || len(info.Groups) != 1 || info.Groups[0].Enabled {
		t.Fatalf("%s", w.Body)
	}
	if strings.Join(info.Tools, ",") != "write_note,put_dashboard_card" || strings.Contains(w.Body.String(), "sk-secret") {
		t.Fatalf("%s", w.Body)
	}
	if !strings.Contains(a.Briefing, "You are Example. Sam approves each change.") || !strings.Contains(a.Briefing, "- Mail: OFF. Missing: MAIL_KEY.") {
		t.Fatal(a.Briefing)
	}
}

func a2aSend(t *testing.T, h http.Handler, text string) string {
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"m-%d","contextId":"c1","role":"ROLE_USER","parts":[{"text":%q}]}}}`, time.Now().UnixNano(), text)
	w := do(h, "POST", "/a2a", strings.NewReader(body), map[string]string{"Content-Type": "application/json", "A2A-Version": "1.0"})
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	return w.Body.String()
}

func TestAnswersAMessage(t *testing.T) {
	a, _ := build(t, config(t, &notebook{}), credentials.Environ{})
	if got := a2aSend(t, a.Handler, "say hello"); !strings.Contains(got, "I run on the fake model") {
		t.Fatal(got)
	}
}

func TestStopsAtAnApprovalAndWritesOnlyAfterTheOperatorApproves(t *testing.T) {
	n := &notebook{}
	a, _ := build(t, config(t, n), credentials.Environ{})
	var out bytes.Buffer
	in := strings.NewReader("Write a note called Plants with the text: water on Friday.\nyes\nexit\n")
	if err := a.Chat(t.Context(), in, &out); !errors.Is(err, ErrExit) {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `Example wants to: Write the note "Plants"`) || len(n.writes) != 1 || n.writes[0].Body != "water on Friday" {
		t.Fatalf("writes %v\n%s", n.writes, out.String())
	}
	if !strings.Contains(out.String(), `The note "Plants" is written.`) {
		t.Fatal(out.String())
	}
}

func TestDoesNotWriteWhenTheOperatorRejects(t *testing.T) {
	n := &notebook{}
	a, _ := build(t, config(t, n), credentials.Environ{})
	var out bytes.Buffer
	in := strings.NewReader("Write a note called Plants with the text: water.\nno\nnot now\n")
	if err := a.Chat(t.Context(), in, &out); err != nil {
		t.Fatal(err)
	}
	if len(n.writes) != 0 || !strings.Contains(out.String(), "You rejected the write") {
		t.Fatalf("writes %v\n%s", n.writes, out.String())
	}
}

func wav() []byte {
	b := make([]byte, 64)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], 56)
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 16000)
	binary.LittleEndian.PutUint32(b[28:], 32000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], 20)
	return b
}

func TestTurnsAWAVIntoText(t *testing.T) {
	a, _ := build(t, config(t, &notebook{}), credentials.Environ{})
	w := do(a.Handler, "POST", "/kit/transcribe", bytes.NewReader(wav()), map[string]string{"Content-Type": "audio/wav"})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "say hello") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestRefusesAudioThatIsNotAWAV(t *testing.T) {
	a, _ := build(t, config(t, &notebook{}), credentials.Environ{})
	if w := do(a.Handler, "POST", "/kit/transcribe", strings.NewReader("mp3?"), map[string]string{"Content-Type": "audio/mpeg"}); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestRefusesANameThatCouldBreakTheHTMLOrTheScript(t *testing.T) {
	for _, name := range []string{"", "<script>", `Ex"ample`, "a/b", "1abc", strings.Repeat("x", 41)} {
		cfg := config(t, &notebook{})
		cfg.Name = name
		if _, err := Build(t.Context(), cfg, credentials.Environ{}, io.Discard); err == nil {
			t.Fatalf("%q passed", name)
		}
	}
}

func TestGivesAnEmptyGroupListNotNull(t *testing.T) {
	a, _ := build(t, config(t, &notebook{}), credentials.Environ{})
	if w := do(a.Handler, "GET", "/kit/info", nil, nil); !strings.Contains(w.Body.String(), `"groups":[]`) {
		t.Fatal(w.Body.String())
	}
}

func TestServesTheConsole(t *testing.T) {
	a, _ := build(t, config(t, &notebook{}), credentials.Environ{})
	if w := do(a.Handler, "GET", "/dashboard", nil, nil); w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("%d", w.Code)
	}
}

// container mode

func TestAnswersTheHealthCheck(t *testing.T) {
	w := httptest.NewRecorder()
	Health().ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 || w.Body.String() != `{"status":"ok"}` {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

func TestRunsHeadlessWithAHealthServerAndStopsCleanly(t *testing.T) {
	cfg := config(t, &notebook{})
	e := credentials.Environ{"EXAMPLE_HEADLESS": "1", "EXAMPLE_HEALTH_ADDR": "127.0.0.1:0"}
	ctx, cancel := context.WithCancel(t.Context())
	out, errOut := &syncBuf{}, &syncBuf{}
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, cfg, e, strings.NewReader(""), out, errOut) }()
	var health string
	for range 100 {
		if m := regexp.MustCompile(`Health: (\S+)/healthz`).FindStringSubmatch(out.String()); m != nil {
			health = m[1]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if health == "" {
		t.Fatalf("no health address:\n%s%s", out.String(), errOut.String())
	}
	res, err := http.Get("http://" + health + "/healthz")
	if err != nil || res.StatusCode != 200 {
		t.Fatal(err)
	}
	res.Body.Close()
	if !strings.Contains(errOut.String(), "Container mode without Infisical") {
		t.Fatal(errOut.String())
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the agent did not stop")
	}
}

func TestAContainerWithNoStoreRefusesANewSecret(t *testing.T) {
	a, _ := build(t, config(t, &notebook{}), credentials.Environ{"EXAMPLE_HEADLESS": "1"})
	if a.Kit.Secrets != secrets.None {
		t.Fatalf("%T", a.Kit.Secrets)
	}
	laptop, _ := build(t, config(t, &notebook{}), credentials.Environ{})
	if laptop.Kit.Secrets == secrets.None || laptop.Kit.Secrets.Where() != "the operator's terminal" {
		t.Fatal(laptop.Kit.Secrets.Where())
	}
}

// auth

func TestNeedsAuthOnAnAddressThatIsNotLoopback(t *testing.T) {
	cfg := config(t, &notebook{})
	cfg.Addr = "0.0.0.0:4141"
	if _, err := Build(t.Context(), cfg, credentials.Environ{}, io.Discard); err == nil || !strings.Contains(err.Error(), "EXAMPLE_TOKEN") {
		t.Fatal(err)
	}
	token := strings.Repeat("t", 40)
	a, _ := build(t, cfg, credentials.Environ{"EXAMPLE_TOKEN": token})
	if w := do(a.Handler, "GET", "/kit/info", nil, map[string]string{"Host": "agent.example.test"}); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := do(a.Handler, "GET", "/kit/info", nil, map[string]string{"Host": "agent.example.test", "Authorization": "Bearer " + token}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	cfg.DB = filepath.Join(t.TempDir(), "b.db")
	b, _ := build(t, cfg, credentials.Environ{"EXAMPLE_TRUSTED_HEADER": "X-Auth-User"})
	if w := do(b.Handler, "GET", "/kit/info", nil, map[string]string{"Host": "agent.example.test", "X-Auth-User": "sam"}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := do(b.Handler, "GET", "/kit/info", nil, map[string]string{"Host": "agent.example.test"}); w.Code != 401 {
		t.Fatal(w.Code)
	}
}

// Agent Vault mode

func TestTurnsOnOnlyTheGroupsOfTheVaultSession(t *testing.T) {
	inf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sess" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"services": []any{map[string]any{"hosts": []string{"dns.example.test"}, "status": "ready"}}})
	}))
	defer inf.Close()
	mk := func(id, host string) group.Group {
		return group.New(group.Def[string]{ID: id, Name: id, Host: host, Keys: []string{strings.ToUpper(id) + "_KEY"},
			Connect: func(c group.ConnectContext) (string, bool) { return c.Get(strings.ToUpper(id) + "_KEY"), true },
			Tools:   func(string, group.Shared) ([]tool.Tool, error) { return nil, nil }})
	}
	cfg := config(t, &notebook{})
	cfg.Groups = []group.Group{mk("dns", "dns.example.test"), mk("mail", "mail.example.test")}
	cfg.Infisical = &Infisical{InfisicalConfig: credentials.InfisicalConfig{URL: inf.URL, ProjectID: "p", Path: "/agents/example"}, TokenKey: "INFISICAL_TOKEN"}
	a, _ := build(t, cfg, credentials.Environ{"HTTPS_PROXY": "http://x-agent-vault:sess@127.0.0.1:1"})
	if a.Mode != credentials.AgentVault || !a.Statuses[0].Enabled || a.Statuses[1].Enabled || a.Statuses[1].Missing[0] != "mail.example.test" {
		t.Fatalf("%s %+v", a.Mode, a.Statuses)
	}
}

func TestTheEndOfStdinDoesNotStopTheAgent(t *testing.T) {
	cfg := config(t, &notebook{})
	ctx, cancel := context.WithCancel(t.Context())
	out := &syncBuf{}
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, cfg, credentials.Environ{}, strings.NewReader(""), out, io.Discard) }()
	select {
	case err := <-done:
		t.Fatalf("the agent stopped at the end of stdin: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestExitStopsTheAgent(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		done <- Serve(t.Context(), config(t, &notebook{}), credentials.Environ{}, strings.NewReader("exit\n"), &syncBuf{}, io.Discard)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("exit did not stop the agent")
	}
}

func TestServesTheConsoleFromAFolder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<title>built console</title>"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, _ := build(t, config(t, &notebook{}), credentials.Environ{"EXAMPLE_CONSOLE_DIR": dir})
	if w := do(a.Handler, "GET", "/", nil, nil); !strings.Contains(w.Body.String(), "built console") {
		t.Fatal(w.Body.String())
	}
	cfg := config(t, &notebook{})
	cfg.ConsoleDir = t.TempDir()
	if _, err := Build(t.Context(), cfg, credentials.Environ{}, io.Discard); err == nil || !strings.Contains(err.Error(), "no index.html") {
		t.Fatal(err)
	}
}

func TestAnEvalCanGiveTheStoreAndTheSink(t *testing.T) {
	cfg := config(t, &notebook{})
	store := secrets.NewMemoryStore("/example")
	cfg.Store, cfg.Secrets = store, secrets.None
	a, _ := build(t, cfg, credentials.Environ{})
	if a.Kit.Store != store || a.Kit.Secrets != secrets.None {
		t.Fatalf("%T %T", a.Kit.Store, a.Kit.Secrets)
	}
}
