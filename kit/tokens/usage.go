package tokens

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Usage is the usage that a provider reports for one model call.
type Usage struct {
	Model      string
	Provider   string
	Prompt     int64
	Completion int64
	Cached     int64
	Reasoning  int64
	CostUSD    float64
	HasCost    bool
}

type rawUsage struct {
	PromptTokens        int64    `json:"prompt_tokens"`
	CompletionTokens    int64    `json:"completion_tokens"`
	Cost                *float64 `json:"cost"`
	PromptTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// parse reads the usage from one JSON object of a Chat Completions answer.
func parse(b []byte, u *Usage) bool {
	var v struct {
		Model    string          `json:"model"`
		Provider string          `json:"provider"`
		Usage    json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(b, &v) != nil || len(v.Usage) == 0 || string(v.Usage) == "null" {
		return false
	}
	var r rawUsage
	if json.Unmarshal(v.Usage, &r) != nil {
		return false
	}
	*u = Usage{Model: v.Model, Provider: v.Provider, Prompt: r.PromptTokens, Completion: r.CompletionTokens,
		Cached: r.PromptTokensDetails.CachedTokens, Reasoning: r.CompletionTokensDetails.ReasoningTokens}
	if r.Cost != nil {
		u.CostUSD, u.HasCost = *r.Cost, true
	}
	return true
}

// Meter records Usage as OpenTelemetry metrics, and on the current span.
type Meter struct {
	tokens metric.Int64Histogram
	cost   metric.Float64Counter
	calls  metric.Int64Counter
	agent  string
}

// NewMeter makes the instruments. A nil provider means the global one.
func NewMeter(mp metric.MeterProvider, agentName string) (*Meter, error) {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}
	m := mp.Meter("github.com/rubics-code/agent-kit/kit/tokens")
	tokens, err := m.Int64Histogram("gen_ai.client.token.usage", metric.WithUnit("{token}"),
		metric.WithDescription("Tokens of each model call, by gen_ai.token.type: input, output, cached or reasoning."))
	if err != nil {
		return nil, err
	}
	cost, err := m.Float64Counter("kit.llm.cost", metric.WithUnit("USD"), metric.WithDescription("The cost that the provider reports for model calls."))
	if err != nil {
		return nil, err
	}
	calls, err := m.Int64Counter("kit.llm.calls", metric.WithDescription("Model calls with a usage report."))
	if err != nil {
		return nil, err
	}
	return &Meter{tokens: tokens, cost: cost, calls: calls, agent: agentName}, nil
}

// Record records one call.
func (m *Meter) Record(ctx context.Context, u Usage) {
	base := []attribute.KeyValue{
		attribute.String("gen_ai.response.model", u.Model),
		attribute.String("gen_ai.provider.name", u.Provider),
		attribute.String("kit.agent", m.agent),
	}
	with := func(kind string) metric.MeasurementOption {
		return metric.WithAttributes(append(base, attribute.String("gen_ai.token.type", kind))...)
	}
	m.tokens.Record(ctx, u.Prompt, with("input"))
	m.tokens.Record(ctx, u.Completion, with("output"))
	m.tokens.Record(ctx, u.Cached, with("cached"))
	m.tokens.Record(ctx, u.Reasoning, with("reasoning"))
	m.calls.Add(ctx, 1, metric.WithAttributes(base...))
	if u.HasCost {
		m.cost.Add(ctx, u.CostUSD, metric.WithAttributes(base...))
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.Int64("gen_ai.usage.input_tokens", u.Prompt),
		attribute.Int64("gen_ai.usage.output_tokens", u.Completion),
		attribute.Int64("gen_ai.usage.cached_tokens", u.Cached),
		attribute.Int64("gen_ai.usage.reasoning_tokens", u.Reasoning),
	)
	if u.HasCost {
		span.SetAttributes(attribute.Float64("kit.llm.cost_usd", u.CostUSD))
	}
}

// Transport reads the usage of each Chat Completions answer as it passes, and
// records it. It does not hold back a stream, and it never reads a header, so
// it never sees a key. Wrap the HTTP client of the model with it.
type Transport struct {
	Base  http.RoundTripper
	Meter *Meter
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := t.Base.RoundTrip(req)
	if err != nil || res.StatusCode != http.StatusOK || !strings.HasSuffix(req.URL.Path, "/chat/completions") {
		return res, err
	}
	sse := strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream")
	res.Body = &usageBody{ReadCloser: res.Body, sse: sse, done: func(u Usage) { t.Meter.Record(req.Context(), u) }}
	return res, nil
}

// usageBody passes the body through, and finds the usage on the way.
type usageBody struct {
	io.ReadCloser
	sse  bool
	done func(Usage)

	once  sync.Once
	buf   bytes.Buffer // a non-stream answer, or the current line of a stream
	usage Usage
	found bool
}

const maxBody = 4 << 20

func (b *usageBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.feed(p[:n])
	if err == io.EOF {
		b.finish()
	}
	return n, err
}

func (b *usageBody) feed(chunk []byte) {
	if !b.sse {
		if b.buf.Len()+len(chunk) <= maxBody {
			b.buf.Write(chunk)
		}
		return
	}
	for len(chunk) > 0 {
		i := bytes.IndexByte(chunk, '\n')
		if i < 0 {
			if b.buf.Len()+len(chunk) <= maxBody {
				b.buf.Write(chunk)
			}
			return
		}
		b.buf.Write(chunk[:i])
		b.line(b.buf.Bytes())
		b.buf.Reset()
		chunk = chunk[i+1:]
	}
}

func (b *usageBody) line(l []byte) {
	data, ok := bytes.CutPrefix(bytes.TrimRight(l, "\r"), []byte("data: "))
	if !ok || bytes.Equal(data, []byte("[DONE]")) || !bytes.Contains(data, []byte(`"usage"`)) {
		return
	}
	var u Usage
	if parse(data, &u) {
		b.usage, b.found = u, true
	}
}

func (b *usageBody) finish() {
	b.once.Do(func() {
		if b.sse {
			if b.buf.Len() > 0 {
				b.line(b.buf.Bytes())
			}
		} else {
			b.found = parse(b.buf.Bytes(), &b.usage)
		}
		if b.found {
			b.done(b.usage)
		}
	})
}

func (b *usageBody) Close() error {
	b.finish()
	return b.ReadCloser.Close()
}
