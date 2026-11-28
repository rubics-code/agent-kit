package tokens

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/rubics-code/agent-kit/kit/internal/kittest"
)

func instruction(r *model.LLMRequest) string {
	if r.Config == nil || r.Config.SystemInstruction == nil {
		return ""
	}
	var b strings.Builder
	for _, p := range r.Config.SystemInstruction.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

func declared(r *model.LLMRequest) []string {
	var out []string
	if r.Config == nil {
		return nil
	}
	for _, t := range r.Config.Tools {
		for _, f := range t.FunctionDeclarations {
			out = append(out, f.Name)
		}
	}
	return out
}

func TestTheDateAndTheMemoryComeAfterAFixedPrefix(t *testing.T) {
	day := time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)
	memory := "Sam likes short answers."
	ctx := DateAndMemory(func() time.Time { return day }, func() string { return memory })
	m := kittest.Script(kittest.Text("one"), kittest.Text("two"))
	h := kittest.New(t, m, nil, kittest.Config{Instruction: "You are Example.", Before: []llmagent.BeforeModelCallback{StablePrefix(ctx)}})
	h.Say("hi")
	day = day.Add(24 * time.Hour)
	memory = "Sam likes long answers."
	evs := h.Say("again")

	r1, r2 := m.Requests[0], m.Requests[1]
	if instruction(r1) != instruction(r2) || strings.Contains(instruction(r1), "2026") || strings.Contains(instruction(r1), "Sam likes") {
		t.Fatalf("the instruction changed or holds the date or memory:\n%s\n---\n%s", instruction(r1), instruction(r2))
	}
	first := r2.Contents[0].Parts[0].Text
	if !strings.Contains(first, "Today is Sunday 29 November 2026.") || !strings.Contains(first, "long answers") || !strings.HasPrefix(first, "<kit-context>") {
		t.Fatalf("%s", first)
	}
	for _, ev := range evs {
		if ev.Content != nil && strings.Contains(ev.Content.Parts[0].Text, ContextTag) {
			t.Fatal("the context went into the session")
		}
	}
}

func tools(t *testing.T, names ...string) []tool.Tool {
	var out []tool.Tool
	for _, n := range names {
		tl, err := functiontool.New(functiontool.Config{Name: n, Description: "The " + n + " tool."},
			func(agent.Context, struct{}) (map[string]any, error) { return nil, nil })
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, tl)
	}
	return out
}

func TestToolSearchSendsOnlyTheCoreToolsAndTheSearch(t *testing.T) {
	all := tools(t, "notes_list", "notes_write", "dns_list", "dns_write", "mail_list")
	on, err := OnDemand(all, []string{"notes_list", "notes_write"}, true)
	if err != nil {
		t.Fatal(err)
	}
	m := kittest.Script(kittest.Text("ok"))
	kittest.New(t, m, nil, kittest.Config{Toolsets: on}).Say("hi")
	got := declared(m.Requests[0])
	slices.Sort(got)
	if strings.Join(got, ",") != "notes_list,notes_write,search_tools" {
		t.Fatal(got)
	}

	off, _ := OnDemand(all, []string{"notes_list"}, false)
	m = kittest.Script(kittest.Text("ok"))
	kittest.New(t, m, nil, kittest.Config{Toolsets: off}).Say("hi")
	if len(declared(m.Requests[0])) != 5 {
		t.Fatal(declared(m.Requests[0]))
	}
	if _, err := OnDemand(all, []string{"missing"}, true); err == nil {
		t.Fatal("a missing core tool passed")
	}
}

func TestTheCompactionDefaultsAreValid(t *testing.T) {
	c, err := Compaction(0, 0)
	if err != nil || c.TokenThreshold != 24000 || c.EventRetentionSize != 12 {
		t.Fatalf("%+v %v", c, err)
	}
}

func collect(t *testing.T, r *sdkmetric.ManualReader) map[string]float64 {
	var rm metricdata.ResourceMetrics
	if err := r.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch d := m.Data.(type) {
			case metricdata.Histogram[int64]:
				for _, p := range d.DataPoints {
					kind, _ := p.Attributes.Value("gen_ai.token.type")
					out[m.Name+"/"+kind.AsString()] += float64(p.Sum)
				}
			case metricdata.Sum[float64]:
				for _, p := range d.DataPoints {
					out[m.Name] += p.Value
				}
			case metricdata.Sum[int64]:
				for _, p := range d.DataPoints {
					out[m.Name] += float64(p.Value)
				}
			}
		}
	}
	return out
}

const usageJSON = `"usage":{"prompt_tokens":4958,"completion_tokens":44,"cost":0.0000172,"prompt_tokens_details":{"cached_tokens":4864},"completion_tokens_details":{"reasoning_tokens":11}}`

func meterAndServer(t *testing.T, contentType, body string) (*sdkmetric.ManualReader, *http.Client, string) {
	reader := sdkmetric.NewManualReader()
	meter, err := NewMeter(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)), "example")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return reader, &http.Client{Transport: &Transport{Base: http.DefaultTransport, Meter: meter}}, srv.URL + "/v1/chat/completions"
}

func TestCountsTheTokensAndTheCostOfACall(t *testing.T) {
	body := `{"model":"example/model","provider":"Example",` + usageJSON + `}`
	reader, c, url := meterAndServer(t, "application/json", body)
	res, err := c.Post(url, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(got) != body {
		t.Fatal("the body changed")
	}
	m := collect(t, reader)
	if m["gen_ai.client.token.usage/input"] != 4958 || m["gen_ai.client.token.usage/cached"] != 4864 || m["gen_ai.client.token.usage/output"] != 44 || m["gen_ai.client.token.usage/reasoning"] != 11 {
		t.Fatalf("%v", m)
	}
	if m["kit.llm.cost"] != 0.0000172 || m["kit.llm.calls"] != 1 {
		t.Fatalf("%v", m)
	}
}

func TestCountsTheUsageAtTheEndOfAStreamAndPassesTheStreamThrough(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n" +
		"data: {\"model\":\"example/model\",\"choices\":[]," + usageJSON + "}\n\n" +
		"data: [DONE]\n\n"
	reader, c, url := meterAndServer(t, "text/event-stream", stream)
	res, err := c.Post(url, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(got) != stream {
		t.Fatal("the stream changed")
	}
	if m := collect(t, reader); m["gen_ai.client.token.usage/input"] != 4958 || m["kit.llm.calls"] != 1 {
		t.Fatalf("%v", m)
	}
}

func TestRecordsNothingForAnotherPathOrAnError(t *testing.T) {
	reader, c, url := meterAndServer(t, "application/json", `{`+usageJSON+`}`)
	res, _ := c.Get(strings.Replace(url, "/chat/completions", "/models", 1))
	_, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if m := collect(t, reader); m["kit.llm.calls"] != 0 {
		t.Fatalf("%v", m)
	}
}
