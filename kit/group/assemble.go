package group

import (
	"fmt"
	"strings"

	"google.golang.org/adk/v2/tool"

	"github.com/rubics-code/agent-kit/kit/policy"
	"github.com/rubics-code/agent-kit/kit/secrets"
)

// StoreGroupID is the id of the group whose client is the agent's secret
// store. When that group is on, every group's tools get its client as Store.
const StoreGroupID = "infisical"

// Enable says which groups are on.
type Enable struct {
	Has        func(key string) bool
	VaultHosts map[string]bool   // the Agent Vault session's hosts; nil outside that mode
	Hosts      map[string]string // overrides a group's host, by group id
}

// Services are the clients by group id.
type Services map[string]any

// Connect makes each group's client from the session's keys.
func Connect(groups []Group, ctx ConnectContext) Services {
	out := Services{}
	for _, g := range groups {
		if c, ok := g.connect(ctx); ok {
			out[g.ID] = c
		}
	}
	return out
}

// Assembly is what an agent gets from its group list.
type Assembly struct {
	Tools    []tool.Tool
	Statuses []Status
	// Guidance is the guidance of each group that is on, in list order.
	Guidance []string
	// Note is the capability note for the briefing.
	Note string
}

// Assemble turns a group list into tools, statuses and the briefing parts.
// A group's tools mount only when the group is on and its client exists.
func Assemble(groups []Group, services Services, sink secrets.Sink, p policy.Policy, en Enable) (Assembly, error) {
	var a Assembly
	_, hasStore := services[StoreGroupID]
	seen := map[string]bool{}
	for _, g := range groups {
		if seen[g.ID] {
			return a, fmt.Errorf("two groups have the id %q", g.ID)
		}
		seen[g.ID] = true
		st, err := ReviewGroup(g, EnableEnv{Has: en.Has, HasStore: hasStore, VaultHosts: en.VaultHosts}, en.Hosts[g.ID])
		if err != nil {
			return a, err
		}
		a.Statuses = append(a.Statuses, st)
	}
	on := map[string]bool{}
	for _, st := range a.Statuses {
		on[st.ID] = st.Enabled
	}
	shared := Shared{Secrets: sink, Policy: p}
	if s, ok := services[StoreGroupID].(secrets.Store); ok && on[StoreGroupID] {
		shared.Store = s
	}
	names := map[string]string{}
	for _, g := range groups {
		c, ok := services[g.ID]
		if !on[g.ID] || !ok {
			continue
		}
		tools, err := g.tools(c, shared)
		if err != nil {
			return a, fmt.Errorf("group %s: %w", g.ID, err)
		}
		for _, t := range tools {
			if other, dup := names[t.Name()]; dup {
				return a, fmt.Errorf("the tool %s is in the groups %s and %s", t.Name(), other, g.ID)
			}
			names[t.Name()] = g.ID
		}
		a.Tools = append(a.Tools, tools...)
		if g.Guidance != "" {
			a.Guidance = append(a.Guidance, g.Guidance)
		}
	}
	a.Note = CapabilityNote(a.Statuses, p.Operator)
	return a, nil
}

// CapabilityNote says which groups are on and, for each one that is off, why.
// It comes from the statuses, never from a person's hand.
func CapabilityNote(statuses []Status, operator string) string {
	if operator == "" {
		operator = "the operator"
	}
	lines := make([]string, len(statuses))
	for i, g := range statuses {
		if g.Enabled {
			lines[i] = fmt.Sprintf("- %s: on.", g.Name)
		} else {
			lines[i] = fmt.Sprintf("- %s: OFF. Missing: %s. %s", g.Name, strings.Join(g.Missing, ", "), g.Setup)
		}
	}
	return fmt.Sprintf("Your tool groups now. If %s asks why a tool is not there, give this reason:\n%s", operator, strings.Join(lines, "\n"))
}

// Keys gives every key that a group list can use, once each.
func Keys(groups []Group) []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range groups {
		for _, k := range g.Keys {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	return out
}
