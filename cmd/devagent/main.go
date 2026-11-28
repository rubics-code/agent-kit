// Command devagent is the Kit's agent for development. It is plain Go code on
// the Kit's runner: a notebook with a write that a person approves, and the
// dashboard. With -fake it needs no key.
package main

import (
	"context"
	"flag"
	"log"
	"strings"

	"google.golang.org/adk/v2/tool"

	"github.com/rubics-code/agent-kit/kit/fakemodel"
	"github.com/rubics-code/agent-kit/kit/policy"
	"github.com/rubics-code/agent-kit/kit/run"
	"github.com/rubics-code/agent-kit/kit/transcribe"
)

const instruction = `You are {{agent}}, the dev agent of the Rubics Code agent kit. You keep a small notebook.
Use list_notes to read the notes. Use write_note to add or change a note. Always call write_note for a write.
When a person asks to keep something on the dashboard, call put_dashboard_card. It needs no approval.
Never say that a note is written or waits for approval unless write_note said so.
{{operator}} must approve each write_note call. When a write waits for approval, say so in one short sentence and stop.
Answer in short, plain sentences.`

func main() {
	var cfg run.Config
	var origins string
	var fake bool
	flag.StringVar(&cfg.Addr, "addr", "127.0.0.1:8080", "where the console and A2A listen")
	flag.StringVar(&cfg.DB, "db", "devagent.db", "SQLite file for the sessions, tasks, notes and dashboard")
	flag.StringVar(&cfg.Model, "model", "deepseek/deepseek-v4-flash-0731", "OpenRouter model id")
	flag.StringVar(&cfg.BaseURL, "base-url", "https://openrouter.ai/api/v1", "OpenAI-compatible base URL")
	flag.StringVar(&cfg.Name, "name", "devagent", "the agent's name")
	flag.StringVar(&cfg.TranscribeModel, "stt-model", transcribe.DefaultModel, "OpenRouter audio model for hold-to-talk")
	flag.BoolVar(&fake, "fake", false, "use the fake model (no key, no provider calls)")
	flag.StringVar(&origins, "origins", "http://localhost:5173,http://127.0.0.1:5173", "browser origins that may call this agent, comma separated")
	flag.Parse()
	for o := range strings.SplitSeq(origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			cfg.Origins = append(cfg.Origins, o)
		}
	}

	cfg.Description = "The Kit's agent for development. It keeps a notebook."
	cfg.EnvPrefix = "DEVAGENT"
	cfg.System = instruction
	cfg.Policy = &policy.Policy{Agent: cfg.Name, Operator: "the operator"}
	cfg.Tools = func(k run.Kit) ([]tool.Tool, error) { return noteTools(k.DB, k.Policy) }
	cfg.CoreTools = []string{"list_notes", "write_note"}
	if fake {
		cfg.LLM = fakemodel.Model{}
		cfg.Transcriber = transcribe.Fake{Text: "say hello"}
	}
	if err := run.Run(context.Background(), cfg); err != nil {
		log.Fatal(err)
	}
}
