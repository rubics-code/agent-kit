// Package group defines a tool group: everything the rest of the Kit needs to
// know about a group is in its definition. The capability note, the Agent
// Vault host check and the briefing come from these definitions, so an id, a
// key or an approval flag is written once.
package group

import (
	"fmt"
	"strings"

	"google.golang.org/adk/v2/tool"

	"github.com/rubics-code/agent-kit/kit/policy"
	"github.com/rubics-code/agent-kit/kit/secrets"
)

// Shared is what a group's tools get besides their client.
type Shared struct {
	Secrets secrets.Sink
	Store   secrets.Store // nil when the agent has no secret store
	Policy  policy.Policy
}

// ConnectContext is what Connect gets. In Agent Vault mode Get returns a
// placeholder that the proxy replaces, so Connect must never log the value.
type ConnectContext struct {
	Holds func(key string) bool
	Get   func(key string) string
	Store secrets.Store
}

// HoldsAll reports whether the session holds every key.
func (c ConnectContext) HoldsAll(keys ...string) bool {
	for _, k := range keys {
		if !c.Holds(k) {
			return false
		}
	}
	return true
}

// EnableEnv is what decides whether a group is on.
type EnableEnv struct {
	Has        func(key string) bool
	HasStore   bool
	VaultHosts map[string]bool // the hosts of the Agent Vault session; nil outside that mode
	Host       string          // the group's host, after the agent's override
}

// Review is a group's own answer to "am I on?", for a group whose keys are
// not env keys.
type Review struct {
	Enabled bool
	Missing []string
	Setup   string
}

// Def is a group with a typed client.
type Def[C any] struct {
	ID       string
	Name     string   // display name, for example MXroute
	Host     string   // the service's host, for the Agent Vault check
	Keys     []string // env keys that turn the group on outside Agent Vault mode
	Setup    string   // what to do when the group is off
	Guidance string   // the group's part of the briefing
	// Review overrides enablement. Optional.
	Review func(EnableEnv) Review
	// Connect makes the client from the session's keys. ok is false when a key is missing.
	Connect func(ConnectContext) (client C, ok bool)
	// Tools makes the tools for a client. It must do no work at build time:
	// the catalog calls it with a zero client.
	Tools func(client C, shared Shared) ([]tool.Tool, error)
}

// Group is a definition with its client type erased, so a list can hold
// groups of any client.
type Group struct {
	ID, Name, Host, Setup, Guidance string
	Keys                            []string
	review                          func(EnableEnv) Review
	connect                         func(ConnectContext) (any, bool)
	tools                           func(client any, shared Shared) ([]tool.Tool, error)
}

// New erases the client type of a definition.
func New[C any](d Def[C]) Group {
	return Group{
		ID: d.ID, Name: d.Name, Host: d.Host, Setup: d.Setup, Guidance: d.Guidance, Keys: d.Keys,
		review: d.Review,
		connect: func(c ConnectContext) (any, bool) {
			if d.Connect == nil {
				return nil, false
			}
			return d.Connect(c)
		},
		tools: func(client any, s Shared) ([]tool.Tool, error) {
			c, _ := client.(C)
			return d.Tools(c, s)
		},
	}
}

// ApprovalTool is a tool that a person must approve. The approval package's
// tools have this method, so the catalog sees the flag from the tool itself
// and cannot drift from the gate.
type ApprovalTool interface {
	RequiresApproval() bool
}

// ToolMeta is one row of a catalog.
type ToolMeta struct {
	Name     string `json:"name"`
	Approval bool   `json:"approval"`
}

// Catalog lists tools with their approval flags.
func Catalog(tools []tool.Tool) []ToolMeta {
	out := make([]ToolMeta, len(tools))
	for i, t := range tools {
		a, ok := t.(ApprovalTool)
		out[i] = ToolMeta{Name: t.Name(), Approval: ok && a.RequiresApproval()}
	}
	return out
}

var definitionShared = Shared{Policy: policy.Policy{Agent: "AGENT", Operator: "OPERATOR", MaxKeyLimitUSD: 100}}

// Catalog builds the group's tools once with a zero client, to list them.
func (g Group) Catalog() ([]ToolMeta, error) {
	tools, err := g.tools(nil, definitionShared)
	if err != nil {
		return nil, fmt.Errorf("group %s: %w", g.ID, err)
	}
	return Catalog(tools), nil
}

// Status says whether a group is on and, when it is off, why.
type Status struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Enabled bool       `json:"enabled"`
	Missing []string   `json:"missing"`
	Setup   string     `json:"setup"`
	Catalog []ToolMeta `json:"catalog"`
}

const vaultSetup = "Add an Agent Vault service for this host to the access bundle, then restart the agent."

// ReviewGroup decides whether a group is on. Outside Agent Vault mode, a group
// is on when the session holds every key that it names. In Agent Vault mode
// the keys are placeholders, so a group is on when the session has a service
// for its host. hostOverride replaces the group's host when it is not empty.
func ReviewGroup(g Group, env EnableEnv, hostOverride string) (Status, error) {
	env.Host = g.Host
	if hostOverride != "" {
		env.Host = hostOverride
	}
	st := Status{ID: g.ID, Name: g.Name, Setup: g.Setup, Missing: []string{}}
	switch {
	case g.review != nil:
		r := g.review(env)
		st.Enabled, st.Missing = r.Enabled, r.Missing
		if r.Setup != "" {
			st.Setup = r.Setup
		}
	case env.VaultHosts != nil:
		st.Enabled = env.Host != "" && env.VaultHosts[strings.ToLower(env.Host)]
		if !st.Enabled {
			st.Missing = []string{env.Host}
			if env.Host == "" {
				st.Missing = []string{"the service"}
			}
		}
		st.Setup = vaultSetup
	default:
		for _, k := range g.Keys {
			if env.Has == nil || !env.Has(k) {
				st.Missing = append(st.Missing, k)
			}
		}
		st.Enabled = len(st.Missing) == 0
	}
	if st.Missing == nil {
		st.Missing = []string{}
	}
	cat, err := g.Catalog()
	st.Catalog = cat
	return st, err
}
