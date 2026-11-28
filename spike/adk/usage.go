package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
)

// usageLog writes one JSON line for each model call. It never writes a header
// or a key.
type usageLog struct {
	mu  sync.Mutex
	out io.Writer
	tag string
}

func (u *usageLog) write(kind string, v map[string]any) {
	v["kind"] = kind
	v["tag"] = u.tag
	v["time"] = time.Now().UTC().Format(time.RFC3339Nano)
	b, _ := json.Marshal(v)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.out.Write(append(b, '\n'))
}

func (u *usageLog) before(_ agent.Context, req *model.LLMRequest) (*model.LLMResponse, error) {
	var names []string
	schemaBytes := 0
	if req.Config != nil {
		for _, t := range req.Config.Tools {
			for _, fd := range t.FunctionDeclarations {
				names = append(names, fd.Name)
				b, _ := json.Marshal(fd)
				schemaBytes += len(b)
			}
		}
	}
	u.write("request", map[string]any{"tools": names, "toolSchemaBytes": schemaBytes, "contents": len(req.Contents)})
	return nil, nil
}

func (u *usageLog) after(_ agent.Context, resp *model.LLMResponse, err error) (*model.LLMResponse, error) {
	if resp == nil || resp.UsageMetadata == nil || resp.Partial {
		return nil, nil
	}
	m := resp.UsageMetadata
	u.write("adk_usage", map[string]any{
		"prompt": m.PromptTokenCount, "cached": m.CachedContentTokenCount,
		"completion": m.CandidatesTokenCount, "reasoning": m.ThoughtsTokenCount, "total": m.TotalTokenCount,
	})
	return nil, nil
}

// rawUsageTransport copies the "usage" object out of each provider response,
// so that the log shows the fields that the provider sent.
type rawUsageTransport struct {
	base http.RoundTripper
	log  *usageLog
}

func (t *rawUsageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil || !strings.HasSuffix(req.URL.Path, "/chat/completions") {
		return resp, err
	}
	body, rerr := io.ReadAll(resp.Body)
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if rerr != nil {
		return resp, nil
	}
	var usage, provider, modelName any
	pick := func(b []byte) {
		var v struct {
			Usage    json.RawMessage `json:"usage"`
			Provider string          `json:"provider"`
			Model    string          `json:"model"`
		}
		if json.Unmarshal(b, &v) == nil && len(v.Usage) > 0 && string(v.Usage) != "null" {
			usage, provider, modelName = v.Usage, v.Provider, v.Model
		}
	}
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		sc := bufio.NewScanner(bytes.NewReader(body))
		sc.Buffer(make([]byte, 1<<20), 1<<24)
		for sc.Scan() {
			if line, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				pick([]byte(line))
			}
		}
	} else {
		pick(body)
	}
	if usage == nil {
		log.Printf("no usage in the %d response from %s", resp.StatusCode, req.URL.Host)
		return resp, nil
	}
	t.log.write("raw_usage", map[string]any{"status": resp.StatusCode, "provider": provider, "model": modelName, "usage": usage})
	return resp, nil
}
