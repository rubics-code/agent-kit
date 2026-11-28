package run

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rubics-code/agent-kit/kit/credentials"
)

// The model is an OpenAI-style test server that reports usage, and an OTLP
// test collector takes the metrics. No call leaves this machine.
func TestSendsTheTokenCountOfEachModelCallToOpenTelemetry(t *testing.T) {
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		b, _ := io.ReadAll(r.Body)
		usage := `"usage":{"prompt_tokens":120,"completion_tokens":3,"total_tokens":123,"cost":0.00001,"prompt_tokens_details":{"cached_tokens":64}}`
		if !strings.Contains(string(b), `"stream":true`) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"x","object":"chat.completion","model":"test/model","provider":"Test","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Hello."}}],`+usage+`}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := func(s string) { _, _ = io.WriteString(w, "data: "+s+"\n\n") }
		chunk(`{"id":"x","object":"chat.completion.chunk","model":"test/model","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello."}}]}`)
		chunk(`{"id":"x","object":"chat.completion.chunk","model":"test/model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
		chunk(`{"id":"x","object":"chat.completion.chunk","model":"test/model","provider":"Test","choices":[],` + usage + `}`)
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer model.Close()
	var mu sync.Mutex
	var otlp bytes.Buffer
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		otlp.WriteString(r.URL.Path + "\n")
		otlp.Write(b)
		mu.Unlock()
	}))
	defer collector.Close()

	cfg := config(t, &notebook{})
	cfg.LLM = nil
	cfg.BaseURL = model.URL
	e := credentials.Environ{"OPENROUTER_API_KEY": "test-key", "EXAMPLE_HEADLESS": "1", "EXAMPLE_HEALTH_ADDR": "127.0.0.1:0",
		"OTEL_EXPORTER_OTLP_ENDPOINT": collector.URL}
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", collector.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")

	ctx, cancel := context.WithCancel(t.Context())
	out := &syncBuf{}
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, cfg, e, nil, out, io.Discard) }()
	var addr string
	for range 100 {
		if m := regexp.MustCompile(`Console and A2A on http://(\S+)\.`).FindStringSubmatch(out.String()); m != nil {
			addr = m[1]
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal(out.String())
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{"message":{"messageId":"m1","contextId":"c1","role":"ROLE_USER","parts":[{"text":"hi"}]}}}`
	req, _ := http.NewRequest("POST", "http://"+addr+"/a2a", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("A2A-Version", "1.0")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	reply, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(reply), "Hello.") {
		t.Fatalf("%s", reply)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	got := otlp.String()
	for _, want := range []string{"/v1/metrics", "gen_ai.client.token.usage", "kit.llm.cost", "gen_ai.token.type", "cached", "Example"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the OTLP export lacks %q (%d bytes)", want, len(got))
		}
	}
}
