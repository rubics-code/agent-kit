// Package tokens holds the Kit's token controls: a stable prefix for the
// provider's cache, tools on demand, compaction of long threads, and a token
// count for each model call.
package tokens

import (
	"fmt"
	"strings"
	"time"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session/compaction"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolsearch"
)

// Context gives the parts of the prompt that change: the date and the memory.
type Context func(ctx agent.Context) string

// DateAndMemory is a Context with today's date and the memory text.
func DateAndMemory(now func() time.Time, memory func() string) Context {
	return func(agent.Context) string {
		var b strings.Builder
		fmt.Fprintf(&b, "Today is %s.", now().Format("Monday 2 January 2006"))
		if m := strings.TrimSpace(memory()); m != "" {
			b.WriteString("\n\nYour memory:\n")
			b.WriteString(m)
		}
		return b.String()
	}
}

// ContextTag marks the context part, so a test or a log can find it.
const ContextTag = "kit-context"

// StablePrefix is a BeforeModelCallback. The instruction and the tools stay
// fixed, so the provider can cache them. The date and the memory go after
// them, as the first content of the request, and never into the session.
func StablePrefix(c Context) llmagent.BeforeModelCallback {
	return func(ctx agent.Context, req *model.LLMRequest) (*model.LLMResponse, error) {
		text := strings.TrimSpace(c(ctx))
		if text == "" {
			return nil, nil
		}
		part := genai.NewPartFromText("<" + ContextTag + ">\n" + text + "\n</" + ContextTag + ">")
		req.Contents = append([]*genai.Content{{Role: genai.RoleUser, Parts: []*genai.Part{part}}}, req.Contents...)
		return nil, nil
	}
}

// Catalog is a fixed list of tools as an ADK toolset.
type Catalog []tool.Tool

func (Catalog) Name() string                                       { return "kit_catalog" }
func (c Catalog) Tools(agent.ReadonlyContext) ([]tool.Tool, error) { return c, nil }

// OnDemand gives the agent its core tools up front and the others behind
// search_tools. With search off, every tool goes up front. Give the agent the
// result only through llmagent.Config.Toolsets.
func OnDemand(all []tool.Tool, core []string, search bool) ([]tool.Toolset, error) {
	if !search {
		return []tool.Toolset{Catalog(all)}, nil
	}
	isCore := map[string]bool{}
	for _, n := range core {
		isCore[n] = true
	}
	var gated []string
	for _, t := range all {
		if !isCore[t.Name()] {
			gated = append(gated, t.Name())
		}
	}
	for n := range isCore {
		found := false
		for _, t := range all {
			found = found || t.Name() == n
		}
		if !found {
			return nil, fmt.Errorf("the core tool %s is not in the tool list", n)
		}
	}
	ts, err := toolsearch.New(Catalog(all), toolsearch.Config{CoreToolNames: core, GatedToolNames: gated})
	if err != nil {
		return nil, err
	}
	return []tool.Toolset{ts}, nil
}

// Compaction is the Kit's default: when a prompt reaches the token threshold,
// everything but the last events becomes a summary. Tail retention bounds the
// prompt; a sliding window alone only makes it smaller by a factor.
func Compaction(threshold, keep int) (*compaction.Config, error) {
	if threshold <= 0 {
		threshold = 24000
	}
	if keep <= 0 {
		keep = 12
	}
	c := &compaction.Config{TokenThreshold: threshold, EventRetentionSize: keep}
	return c, c.Validate()
}
