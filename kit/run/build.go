package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model/openaimodel"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	adka2a "google.golang.org/adk/v2/server/adka2a/v2"
	"google.golang.org/adk/v2/server/adkrest"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/compaction"
	"google.golang.org/adk/v2/session/database"
	"google.golang.org/adk/v2/tool"

	"github.com/rubics-code/agent-kit/kit/a2ui"
	"github.com/rubics-code/agent-kit/kit/approval"
	"github.com/rubics-code/agent-kit/kit/briefing"
	"github.com/rubics-code/agent-kit/kit/credentials"
	"github.com/rubics-code/agent-kit/kit/dashboard"
	"github.com/rubics-code/agent-kit/kit/group"
	"github.com/rubics-code/agent-kit/kit/infisical"
	"github.com/rubics-code/agent-kit/kit/policy"
	"github.com/rubics-code/agent-kit/kit/secrets"
	"github.com/rubics-code/agent-kit/kit/taskstore"
	"github.com/rubics-code/agent-kit/kit/tokens"
	"github.com/rubics-code/agent-kit/kit/transcribe"
	"github.com/rubics-code/agent-kit/web"
)

// Agent is a built agent, ready to serve.
type Agent struct {
	Name     string
	Model    string
	Mode     credentials.Mode
	Headless bool
	Statuses []group.Status
	Tools    []string
	Briefing string
	Handler  http.Handler
	Runner   *runner.Runner
	Sessions session.Service
	Kit      Kit

	appName string
	cfg     Config
	db      *gorm.DB
}

// Close releases the database.
func (a *Agent) Close() error {
	if a.db == nil {
		return nil
	}
	sqlDB, err := a.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func openDB(path string) (*gorm.DB, error) {
	return gorm.Open(sqlite.Open(path+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_txlock=immediate"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
}

// keys gets the keys in the mode that the environment shows. Agent Vault
// wins: in that mode the process holds no keys. <PREFIX>_KEYS=user skips the
// identity file and uses the caller's own keys.
func keys(ctx context.Context, c *Config, e credentials.Environ, client *http.Client) (credentials.Credentials, map[string]bool, secrets.Store, error) {
	vault := credentials.IsAgentVault(e)
	if !vault && c.env(e, "KEYS") != "user" && c.IdentityFile != "" && c.Infisical != nil {
		id, ok, err := credentials.ReadIdentityFile(c.IdentityFile)
		if err != nil {
			return credentials.Credentials{}, nil, nil, err
		}
		if ok {
			creds, inf, err := credentials.LoadIdentity(ctx, id, c.Infisical.InfisicalConfig, client)
			return creds, nil, inf, err
		}
	}
	creds, err := credentials.Load(e)
	if err != nil || creds.Mode != credentials.AgentVault || c.Infisical == nil {
		return creds, nil, nil, err
	}
	hosts := credentials.SessionHosts(ctx, c.Infisical.URL, e, client)
	u, err := url.Parse(c.Infisical.URL)
	if err != nil {
		return creds, hosts, nil, err
	}
	if hosts != nil && !hosts[strings.ToLower(u.Host)] {
		return creds, hosts, nil, nil
	}
	token, err := creds.Get(c.Infisical.TokenKey)
	if err != nil {
		return creds, hosts, nil, err
	}
	return creds, hosts, infisical.New(infisical.Auth{Token: token}, infisical.Config{
		BaseURL: c.Infisical.URL, ProjectID: c.Infisical.ProjectID, Env: c.Infisical.Env, Client: client,
	}), nil
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Build makes the agent from its config and the environment. It listens on
// nothing; Run does that.
func Build(ctx context.Context, cfg Config, e credentials.Environ, stderr io.Writer) (*Agent, error) {
	if err := cfg.defaults(); err != nil {
		return nil, err
	}
	cfg.environ = e
	if v := cfg.env(e, "ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := cfg.env(e, "DB"); v != "" {
		cfg.DB = v
	}
	if v := cfg.env(e, "MODEL"); v != "" {
		cfg.Model = v
	}
	headless := cfg.env(e, "HEADLESS") == "1"

	client := cfg.HTTPClient
	if client == nil {
		c, err := credentials.HTTPClient(e)
		if err != nil {
			return nil, err
		}
		client = c
	}
	creds, vaultHosts, store, err := keys(ctx, &cfg, e, client)
	if err != nil {
		return nil, err
	}
	if cfg.Store != nil {
		store = cfg.Store
	}
	if creds.Mode == credentials.AgentVault && cfg.Infisical != nil && vaultHosts == nil {
		fmt.Fprintln(stderr, "Agent Vault: Infisical did not list the session's services, so every tool group is on.")
	}

	system, pol, memory := cfg.System, cfg.Policy, []briefing.File(nil)
	if cfg.Home != "" {
		h, err := briefing.LoadHome(cfg.Home)
		if err != nil {
			return nil, err
		}
		system, pol, memory = h.System, &h.Policy, h.Memory
	}
	if system == "" || pol == nil {
		return nil, errors.New("the agent needs a home, or a system prompt and a policy")
	}

	var sink secrets.Sink = secrets.Terminal(stderr)
	switch {
	case cfg.Secrets != nil:
		sink = cfg.Secrets
	case headless:
		inbox := cfg.SecretInbox
		if inbox == "" && cfg.Infisical != nil {
			inbox = cfg.Infisical.Path + "/inbox"
		}
		switch {
		case store != nil && inbox != "":
			sink = secrets.Inbox(store, inbox, cfg.Now)
		default:
			sink = secrets.None
			fmt.Fprintln(stderr, "Container mode without Infisical: a new secret must go to Infisical with storeAt.")
		}
	}

	db, err := openDB(cfg.DB)
	if err != nil {
		return nil, err
	}
	a := &Agent{Name: cfg.Name, Model: cfg.Model, Mode: creds.Mode, Headless: headless, cfg: cfg, db: db}
	if err := a.build(ctx, creds, vaultHosts, store, sink, *pol, system, memory, client); err != nil {
		_ = a.Close()
		return nil, err
	}
	return a, nil
}

func (a *Agent) build(ctx context.Context, creds credentials.Credentials, vaultHosts map[string]bool, store secrets.Store,
	sink secrets.Sink, pol policy.Policy, system string, memory []briefing.File, client *http.Client) error {
	cfg := a.cfg
	a.Kit = Kit{DB: a.db, Policy: pol, Secrets: sink, Store: store, HTTPClient: client, Mode: creds.Mode, Headless: a.Headless}

	services := group.Connect(cfg.Groups, group.ConnectContext{Holds: creds.Holds, Get: creds.Optional, Store: store, HTTPClient: client})
	hosts := map[string]string{}
	if cfg.Infisical != nil {
		if u, err := url.Parse(cfg.Infisical.URL); err == nil {
			hosts[group.StoreGroupID] = u.Host
		}
	}
	asm, err := group.Assemble(cfg.Groups, services, sink, pol, group.Enable{Has: creds.Holds, VaultHosts: vaultHosts, Hosts: hosts})
	if err != nil {
		return err
	}
	a.Statuses = asm.Statuses
	tools := append([]tool.Tool{}, asm.Tools...)
	if cfg.Tools != nil {
		own, err := cfg.Tools(a.Kit)
		if err != nil {
			return err
		}
		tools = append(tools, own...)
	}
	dash, err := dashboard.New(a.db, cfg.Name)
	if err != nil {
		return err
	}
	cardTool, err := dash.Tool()
	if err != nil {
		return err
	}
	tools = append(tools, cardTool)
	for _, t := range tools {
		a.Tools = append(a.Tools, t.Name())
	}
	a.Briefing = briefing.Assemble(system, asm, pol)

	meter, err := tokens.NewMeter(nil, cfg.Name)
	if err != nil {
		return err
	}
	m := cfg.LLM
	if m == nil {
		key, err := creds.Get(cfg.ModelKey)
		if err != nil {
			return err
		}
		mc := &http.Client{Transport: &tokens.Transport{Base: client.Transport, Meter: meter}}
		m, err = openaimodel.NewModel(ctx, cfg.Model, &openaimodel.ClientConfig{APIKey: key, BaseURL: cfg.BaseURL, API: openaimodel.APIChatCompletions, HTTPClient: mc})
		if err != nil {
			return err
		}
	}
	stt := cfg.Transcriber
	if stt == nil {
		key, _ := creds.Get(cfg.ModelKey)
		stt = transcribe.OpenRouter{APIKey: key, BaseURL: cfg.BaseURL, Model: cfg.TranscribeModel, Client: client}
	}

	toolsets, err := tokens.OnDemand(tools, cfg.CoreTools, cfg.ToolSearch)
	if err != nil {
		return err
	}
	memText := func() string {
		var b strings.Builder
		for _, f := range memory {
			b.WriteString(f.Text)
			b.WriteString("\n\n")
		}
		return b.String()
	}
	brief := a.Briefing
	ag, err := llmagent.New(llmagent.Config{
		Name:                 strings.ReplaceAll(strings.ToLower(cfg.Name), " ", "_"),
		Description:          cfg.Description,
		Model:                m,
		InstructionProvider:  func(agent.ReadonlyContext) (string, error) { return brief, nil },
		Toolsets:             toolsets,
		BeforeModelCallbacks: []llmagent.BeforeModelCallback{tokens.StablePrefix(tokens.DateAndMemory(cfg.Now, memText))},
		AfterModelCallbacks:  []llmagent.AfterModelCallback{approval.FalseClaimCheck()},
	})
	if err != nil {
		return err
	}

	sessions, err := database.NewSessionServiceFromDB(a.db)
	if err != nil {
		return err
	}
	if err := database.AutoMigrate(sessions); err != nil {
		return err
	}
	a.Sessions = sessions
	tasks, err := taskstore.New(a.db, nil)
	if err != nil {
		return err
	}
	guard, err := approval.ReplayGuard()
	if err != nil {
		return err
	}
	var comp *compaction.Config
	if cfg.CompactAt >= 0 {
		if comp, err = tokens.Compaction(cfg.CompactAt, 0); err != nil {
			return err
		}
	}
	plugins := runner.PluginConfig{Plugins: []*plugin.Plugin{guard}}
	a.appName = ag.Name()
	rc := runner.Config{AppName: ag.Name(), Agent: ag, SessionService: sessions, PluginConfig: plugins, Compaction: comp}
	if a.Runner, err = runner.New(rc); err != nil {
		return err
	}

	mux := http.NewServeMux()
	rest, err := adkrest.NewServer(adkrest.ServerConfig{
		SessionService: sessions, AgentLoader: agent.NewSingleLoader(ag), PluginConfig: plugins, Compaction: comp,
		BindHost: cfg.Addr, AllowedOrigins: cfg.Origins,
	})
	if err != nil {
		return err
	}
	mux.Handle("/api/", http.StripPrefix("/api", rest))
	card := &a2a.AgentCard{
		Name: cfg.Name, Description: cfg.Description, Version: "0.1.0",
		SupportedInterfaces: []*a2a.AgentInterface{{URL: "http://" + cfg.Addr + "/a2a", ProtocolBinding: a2a.TransportProtocolJSONRPC, ProtocolVersion: a2a.Version}},
		DefaultInputModes:   []string{"text/plain"}, DefaultOutputModes: []string{"text/plain"},
		Skills:       adka2a.BuildAgentSkills(ag),
		Capabilities: a2a.AgentCapabilities{Streaming: true, Extensions: []a2a.AgentExtension{a2ui.Extension(a2ui.KitCatalog, a2ui.BasicCatalog)}},
	}
	exec := adka2a.NewExecutor(adka2a.ExecutorConfig{
		RunnerConfig:         rc,
		RunConfig:            agent.RunConfig{StreamingMode: agent.StreamingModeSSE},
		A2APartConverter:     a2ui.ToGenAI,
		AfterExecuteCallback: a2ui.ApprovalCards(a2ui.DefaultChoices...),
	})
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	mux.Handle("/a2a", a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(exec, a2asrv.WithTaskStore(tasks))))
	mux.Handle("/kit/dashboard", dash.Handler())
	mux.Handle("/kit/dashboard/", dash.Handler())
	mux.Handle("/kit/transcribe", transcribe.Handler(stt))
	mux.HandleFunc("GET /kit/info", a.info)
	if cfg.Routes != nil {
		if err := cfg.Routes(a.Kit, mux); err != nil {
			return err
		}
	}
	console := web.Dist()
	dir := cfg.ConsoleDir
	if v := cfg.env(a.env(), "CONSOLE_DIR"); v != "" {
		dir = v
	}
	if dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
			return fmt.Errorf("the console folder %s has no index.html: %w", dir, err)
		}
		console = os.DirFS(dir)
	}
	mux.Handle("/", web.Handler(console))
	h, err := protect(mux, cfg, a.env())
	if err != nil {
		return err
	}
	a.Handler = h
	return nil
}

// env is set by Run before Build; tests give it in Build's environment.
func (a *Agent) env() credentials.Environ { return a.cfg.environ }
