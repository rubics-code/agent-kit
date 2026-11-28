// Package run starts an agent from its config: the keys, the tool groups, the
// model, the servers (A2A, the ADK REST API, the Kit endpoints and the
// console), and a terminal chat on a laptop or a health server in a container.
// An agent is plain Go code that calls run.Run.
package run

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"gorm.io/gorm"

	"github.com/rubics-code/agent-kit/kit/credentials"
	"github.com/rubics-code/agent-kit/kit/group"
	"github.com/rubics-code/agent-kit/kit/policy"
	"github.com/rubics-code/agent-kit/kit/secrets"
	"github.com/rubics-code/agent-kit/kit/transcribe"
)

// Config is an agent, as data.
type Config struct {
	// Name is the agent's name, in the copy and the console.
	Name        string
	Description string
	// Model is the model id on OpenRouter. Pin it in code, so that a change is a PR.
	Model string
	// ModelKey names the key with the model's API key. Default OPENROUTER_API_KEY.
	ModelKey string
	// BaseURL is the OpenAI-compatible endpoint. Default OpenRouter.
	BaseURL string
	// LLM replaces the model, for tests and for a dev agent with no key.
	LLM model.LLM
	// Transcriber replaces the speech-to-text client.
	Transcriber     transcribe.Transcriber
	TranscribeModel string

	// Home is the agent's home folder (prompts/system.md, policy.json,
	// memory/). Without it, System and Policy must be set.
	Home   string
	System string
	Policy *policy.Policy

	// Groups are the tool groups.
	Groups []group.Group
	// Tools gives the agent's own tools, which are always on. They are fixed
	// at start: an agent never adds or changes a tool at runtime.
	Tools func(k Kit) ([]tool.Tool, error)
	// Routes adds the agent's own HTTP endpoints.
	Routes func(k Kit, mux *http.ServeMux) error
	// CoreTools are sent on every call. With ToolSearch, the others wait
	// behind search_tools.
	CoreTools  []string
	ToolSearch bool
	// CompactAt is the prompt size in tokens that starts compaction of a
	// thread. Zero means the Kit's default; a negative value turns it off.
	CompactAt int

	// Infisical is the agent's Infisical project. TokenKey names the key with
	// a token, for Agent Vault mode.
	Infisical *Infisical
	// IdentityFile is the .env file with the agent's machine identity.
	IdentityFile string
	// SecretInbox is the folder for a new secret in a container. Default
	// <Infisical.Path>/inbox.
	SecretInbox string

	// EnvPrefix is the prefix of the agent's own variables. Default: the name
	// in upper case.
	EnvPrefix string
	// Addr is where the console and A2A listen. Default 127.0.0.1:4141.
	Addr string
	// HealthAddr is where /healthz listens in a container. Default :8080.
	HealthAddr string
	// DB is the SQLite file. Default <name>.db.
	DB string
	// Origins are the browser origins that may call the agent from another origin.
	Origins []string

	// ConsoleDir serves the console from a folder of built files, in place of
	// the console in the binary. <PREFIX>_CONSOLE_DIR replaces it. An agent
	// in another module uses it, because its copy of the Kit has no build.
	ConsoleDir string

	// HTTPClient replaces the Kit's client for outside calls. Store and
	// Secrets replace the secret store and the secret sink. They are for
	// evals, which run the agent against fake services.
	HTTPClient *http.Client
	Store      secrets.Store
	Secrets    secrets.Sink

	// Now replaces the clock, for tests.
	Now func() time.Time

	environ credentials.Environ
}

// Infisical is the agent's Infisical deployment.
type Infisical struct {
	credentials.InfisicalConfig
	TokenKey string
}

// Kit is what an agent's Tools and Routes get.
type Kit struct {
	DB         *gorm.DB
	Policy     policy.Policy
	Secrets    secrets.Sink
	Store      secrets.Store
	HTTPClient *http.Client
	Mode       credentials.Mode
	Headless   bool
}

// Names that are safe in HTML, in a script and in a header.
var nameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 _.-]{0,39}$`)

func (c *Config) defaults() error {
	if !nameRe.MatchString(c.Name) {
		return fmt.Errorf("the agent name %q must be 1 to 40 letters, digits, spaces, dots, dashes or underscores, and start with a letter", c.Name)
	}
	if c.EnvPrefix == "" {
		c.EnvPrefix = strings.Trim(regexp.MustCompile(`[^A-Z0-9]+`).ReplaceAllString(strings.ToUpper(c.Name), "_"), "_")
	}
	if c.ModelKey == "" {
		c.ModelKey = "OPENROUTER_API_KEY"
	}
	if c.BaseURL == "" {
		c.BaseURL = "https://openrouter.ai/api/v1"
	}
	if c.Addr == "" {
		c.Addr = "127.0.0.1:4141"
	}
	if c.HealthAddr == "" {
		c.HealthAddr = ":8080"
	}
	if c.DB == "" {
		c.DB = strings.ToLower(c.EnvPrefix) + ".db"
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Model == "" && c.LLM == nil {
		return fmt.Errorf("the agent %s has no model", c.Name)
	}
	return nil
}

// env reads one of the agent's own variables: <PREFIX>_<name>.
func (c *Config) env(e credentials.Environ, name string) string {
	return e[c.EnvPrefix+"_"+name]
}
