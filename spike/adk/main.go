// Command spike is the phase 0 test agent for the Go Kit on Google ADK Go v2.
// See README.md for how to run it and FINDINGS.md for the results.
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model/openaimodel"
	"google.golang.org/adk/v2/runner"
	adka2a "google.golang.org/adk/v2/server/adka2a/v2"
	"google.golang.org/adk/v2/server/adkrest"
	"google.golang.org/adk/v2/session/database"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/toolsearch"
)

const instruction = `You are the spike agent of the Rubics Code agent kit. You keep a small notebook.
Use list_notes to read the notes. Use write_note to add or change a note.
A person must approve each write_note call. When a write is waiting for approval, say so in one short sentence and stop.
Do not write a note again after a person rejects it, unless the person asks again.
Answer in short, plain sentences.`

type catalog []tool.Tool

func (catalog) Name() string                                       { return "spike_catalog" }
func (c catalog) Tools(agent.ReadonlyContext) ([]tool.Tool, error) { return c, nil }

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	dbPath := flag.String("db", "spike.db", "SQLite file for the sessions and the notes")
	modelName := flag.String("model", "deepseek/deepseek-v4-flash-0731", "OpenRouter model id")
	baseURL := flag.String("base-url", "https://openrouter.ai/api/v1", "OpenAI-compatible base URL")
	useSearch := flag.Bool("toolsearch", false, "hide the tools behind search_tools")
	fillers := flag.Int("fillers", len(fillerSpecs), "number of filler tools (0 to 19)")
	rawUsage := flag.Bool("raw-usage", true, "log the provider's raw usage object (wraps the HTTP client)")
	usagePath := flag.String("usage-log", "", "JSON lines file for the token log (default stderr)")
	tag := flag.String("tag", "", "label for the lines in the token log")
	flag.Parse()

	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		log.Fatal("export OPENROUTER_API_KEY first")
	}
	ctx := context.Background()

	ul := &usageLog{out: os.Stderr, tag: *tag}
	if *usagePath != "" {
		f, err := os.OpenFile(*usagePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		ul.out = f
	}

	cfg := &openaimodel.ClientConfig{APIKey: key, BaseURL: *baseURL, API: openaimodel.APIChatCompletions}
	if *rawUsage {
		// A clone of the default transport keeps ProxyFromEnvironment.
		cfg.HTTPClient = &http.Client{Transport: &rawUsageTransport{base: http.DefaultTransport.(*http.Transport).Clone(), log: ul}}
	}
	m, err := openaimodel.NewModel(ctx, *modelName, cfg)
	if err != nil {
		log.Fatal(err)
	}

	db, err := gorm.Open(sqlite.Open(*dbPath), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		log.Fatal(err)
	}
	if err := db.AutoMigrate(&Note{}); err != nil {
		log.Fatal(err)
	}
	sessions, err := database.NewSessionServiceFromDB(db)
	if err != nil {
		log.Fatal(err)
	}
	if err := database.AutoMigrate(sessions); err != nil {
		log.Fatal(err)
	}

	core, err := noteTools(db)
	if err != nil {
		log.Fatal(err)
	}
	extra, err := fillerTools(*fillers)
	if err != nil {
		log.Fatal(err)
	}
	all := append(append([]tool.Tool{}, core...), extra...)

	acfg := llmagent.Config{
		Name:                 "spike",
		Description:          "Test agent for the Go Kit spike. It keeps a notebook.",
		Model:                m,
		Instruction:          instruction,
		BeforeModelCallbacks: []llmagent.BeforeModelCallback{ul.before},
		AfterModelCallbacks:  []llmagent.AfterModelCallback{ul.after},
	}
	if *useSearch {
		var gatedNames []string
		for _, t := range extra {
			gatedNames = append(gatedNames, t.Name())
		}
		gated, err := toolsearch.New(catalog(all), toolsearch.Config{
			CoreToolNames:  []string{"list_notes", writeToolName},
			GatedToolNames: gatedNames,
		})
		if err != nil {
			log.Fatal(err)
		}
		acfg.Toolsets = []tool.Toolset{gated}
	} else {
		acfg.Tools = all
	}
	a, err := llmagent.New(acfg)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()

	rest, err := adkrest.NewServer(adkrest.ServerConfig{
		SessionService: sessions,
		AgentLoader:    agent.NewSingleLoader(a),
		BindHost:       "127.0.0.1",
		AllowedOrigins: []string{"http://localhost:5173", "http://127.0.0.1:5173"},
	})
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/api/", http.StripPrefix("/api", rest))

	card := &a2a.AgentCard{
		Name:        a.Name(),
		Description: a.Description(),
		SupportedInterfaces: []*a2a.AgentInterface{{
			URL:             "http://" + *addr + "/a2a",
			ProtocolBinding: a2a.TransportProtocolJSONRPC,
			ProtocolVersion: a2a.Version,
		}},
		Version:            "0.0.0-spike",
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills:             adka2a.BuildAgentSkills(a),
		Capabilities: a2a.AgentCapabilities{
			Streaming: true,
			Extensions: []a2a.AgentExtension{{
				URI:         a2uiExtensionURI,
				Description: "Ability to render A2UI v1.0",
				Params:      map[string]any{"supportedCatalogIds": []string{a2uiBasicCatalog}, "acceptsInlineCatalogs": false},
			}},
		},
	}
	exec := adka2a.NewExecutor(adka2a.ExecutorConfig{
		RunnerConfig:         runner.Config{AppName: a.Name(), Agent: a, SessionService: sessions},
		A2APartConverter:     a2uiToGenAI,
		AfterExecuteCallback: addApprovalCards,
	})
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	mux.Handle("/a2a", a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(exec)))

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("spike on http://%s (REST /api, A2A /a2a, model %s, toolsearch %v, %d tools)", *addr, *modelName, *useSearch, len(all))
	log.Fatal(http.Serve(ln, mux))
}
