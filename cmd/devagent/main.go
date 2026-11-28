// Command devagent is the Kit's agent for development. It serves A2A, the ADK
// REST API and the Kit endpoints. With -fake it needs no key.
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/openaimodel"
	"google.golang.org/adk/v2/runner"
	adka2a "google.golang.org/adk/v2/server/adka2a/v2"
	"google.golang.org/adk/v2/server/adkrest"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/database"

	"github.com/rubics-code/agent-kit/kit/a2ui"
	"github.com/rubics-code/agent-kit/kit/fakemodel"
	"github.com/rubics-code/agent-kit/kit/taskstore"
)

const instruction = `You are the dev agent of the Rubics Code agent kit. You keep a small notebook.
Use list_notes to read the notes. Use write_note to add or change a note. Always call write_note for a write.
Never say that a note is written or waits for approval unless write_note said so.
A person must approve each write_note call. When a write waits for approval, say so in one short sentence and stop.
Answer in short, plain sentences.`

type config struct {
	addr, db, model, baseURL string
	fake                     bool
	origins                  []string
}

func main() {
	var c config
	var origins string
	flag.StringVar(&c.addr, "addr", "127.0.0.1:8080", "listen address")
	flag.StringVar(&c.db, "db", "devagent.db", "SQLite file for the sessions, tasks and tools")
	flag.StringVar(&c.model, "model", "deepseek/deepseek-v4-flash-0731", "OpenRouter model id")
	flag.StringVar(&c.baseURL, "base-url", "https://openrouter.ai/api/v1", "OpenAI-compatible base URL")
	flag.BoolVar(&c.fake, "fake", false, "use the fake model (no key, no provider calls)")
	flag.StringVar(&origins, "origins", "http://localhost:5173,http://127.0.0.1:5173", "browser origins that may call this agent, comma separated")
	flag.Parse()
	for o := range strings.SplitSeq(origins, ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.origins = append(c.origins, o)
		}
	}

	h, err := build(context.Background(), c)
	if err != nil {
		log.Fatal(err)
	}
	ln, err := net.Listen("tcp", c.addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("devagent on http://%s (fake model: %v)", ln.Addr(), c.fake)
	log.Fatal(http.Serve(ln, h))
}

func newModel(ctx context.Context, c config) (model.LLM, error) {
	if c.fake {
		return fakemodel.Model{}, nil
	}
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		log.Fatal("export OPENROUTER_API_KEY, or use -fake")
	}
	return openaimodel.NewModel(ctx, c.model, &openaimodel.ClientConfig{APIKey: key, BaseURL: c.baseURL, API: openaimodel.APIChatCompletions})
}

func build(ctx context.Context, c config) (http.Handler, error) {
	m, err := newModel(ctx, c)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(sqlite.Open(c.db+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_txlock=immediate"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	sessions, err := database.NewSessionServiceFromDB(db)
	if err != nil {
		return nil, err
	}
	if err := database.AutoMigrate(sessions); err != nil {
		return nil, err
	}
	tasks, err := taskstore.New(db, nil)
	if err != nil {
		return nil, err
	}
	tools, err := noteTools(db)
	if err != nil {
		return nil, err
	}

	a, err := llmagent.New(llmagent.Config{
		Name:        "devagent",
		Description: "The Kit's agent for development. It keeps a notebook.",
		Model:       m,
		Instruction: instruction,
		Tools:       tools,
	})
	if err != nil {
		return nil, err
	}
	return routes(a, sessions, tasks, c)
}

// routes serves A2A on /a2a, the ADK REST API on /api, and the agent card.
func routes(a agent.Agent, sessions session.Service, tasks *taskstore.Store, c config) (http.Handler, error) {
	mux := http.NewServeMux()
	rest, err := adkrest.NewServer(adkrest.ServerConfig{
		SessionService: sessions,
		AgentLoader:    agent.NewSingleLoader(a),
		BindHost:       c.addr,
		AllowedOrigins: c.origins,
	})
	if err != nil {
		return nil, err
	}
	mux.Handle("/api/", http.StripPrefix("/api", rest))

	card := &a2a.AgentCard{
		Name:        a.Name(),
		Description: a.Description(),
		SupportedInterfaces: []*a2a.AgentInterface{{
			URL:             "http://" + c.addr + "/a2a",
			ProtocolBinding: a2a.TransportProtocolJSONRPC,
			ProtocolVersion: a2a.Version,
		}},
		Version:            "0.1.0",
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills:             adka2a.BuildAgentSkills(a),
		Capabilities: a2a.AgentCapabilities{
			Streaming:  true,
			Extensions: []a2a.AgentExtension{a2ui.Extension(a2ui.BasicCatalog)},
		},
	}
	exec := adka2a.NewExecutor(adka2a.ExecutorConfig{
		RunnerConfig:         runner.Config{AppName: a.Name(), Agent: a, SessionService: sessions},
		RunConfig:            agent.RunConfig{StreamingMode: agent.StreamingModeSSE},
		A2APartConverter:     a2ui.ToGenAI,
		AfterExecuteCallback: a2ui.ApprovalCards(a2ui.BasicCatalog, a2ui.BasicApprovalCard),
	})
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	mux.Handle("/a2a", a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(exec, a2asrv.WithTaskStore(tasks))))
	return mux, nil
}
