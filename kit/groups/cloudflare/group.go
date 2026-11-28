package cloudflare

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	"github.com/rubics-code/agent-kit/kit/approval"
	"github.com/rubics-code/agent-kit/kit/group"
	"github.com/rubics-code/agent-kit/kit/schema"
)

//go:embed guidance.md
var guidance string

// TokenKey is the env key of the Cloudflare API token.
const TokenKey = "CLOUDFLARE_API_TOKEN"

// Host is the Cloudflare API host, for the Agent Vault check.
const Host = "api.cloudflare.com"

// RecordName is a record name: @ for the domain itself, or the name in front
// of the domain.
type RecordName string

var types = schema.Types{
	schema.Of[RecordName](): {
		Type:        "string",
		Pattern:     `^(@|[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?(\.[a-z0-9_]([a-z0-9_-]*[a-z0-9_])?)*)$`,
		Description: "@ for the domain itself, or the name in front of the domain, for example x._domainkey",
	},
}

type listArgs struct {
	Domain schema.Domain `json:"domain"`
	Type   string        `json:"type,omitempty" jsonschema:"Only this record type"`
}

type createArgs struct {
	Domain   schema.Domain `json:"domain"`
	Type     string        `json:"type" jsonschema:"MX or TXT"`
	Name     RecordName    `json:"name"`
	Content  string        `json:"content" jsonschema:"For MX the mail server host name; for TXT the text, with no quotes"`
	Priority *int          `json:"priority,omitempty" jsonschema:"MX only: the priority, for example 10"`
}

type createData struct {
	ZoneID string
	Name   string
}

// Group is the Cloudflare DNS tool group: mail records only. Terraform owns
// the A, AAAA and CNAME records, so another type could start a fight with a
// Terraform apply.
var Group = group.New(group.Def[*DNS]{
	ID:       "cloudflare",
	Name:     "Cloudflare DNS",
	Host:     Host,
	Keys:     []string{TokenKey},
	Setup:    "Add the Cloudflare API token to the agent's secrets in Infisical, then restart the agent.",
	Guidance: guidance,
	Connect: func(c group.ConnectContext) (*DNS, bool) {
		if !c.Holds(TokenKey) {
			return nil, false
		}
		return &DNS{API: API{Token: c.Get(TokenKey), HTTP: c.Client()}}, true
	},
	Tools: tools,
})

func fqdn(name, dom string) string {
	if name == "@" || name == dom {
		return dom
	}
	if strings.HasSuffix(name, "."+dom) {
		return name
	}
	return name + "." + dom
}

var quoted = regexp.MustCompile(`(?s)^"(.*)"$`)

// same compares two values. Cloudflare can give a TXT value in quotes, and an
// MX host with a final dot.
func same(typ, a, b string) bool {
	norm := func(s string) string {
		if typ == "TXT" {
			return quoted.ReplaceAllString(s, "$1")
		}
		return strings.TrimSuffix(strings.ToLower(s), ".")
	}
	return norm(a) == norm(b)
}

var spf = regexp.MustCompile(`(?i)^"?v=spf1(\s|"?$)`)

func isSPF(typ, content string) bool { return typ == "TXT" && spf.MatchString(content) }

func show(typ, name, content string, priority *int) string {
	p := ""
	if typ == "MX" && priority != nil {
		p = fmt.Sprintf(" %d", *priority)
	}
	return fmt.Sprintf("%s %s%s %s", typ, name, p, content)
}

func view(r Record) map[string]any {
	v := map[string]any{"type": r.Type, "name": r.Name, "content": r.Content, "proxied": r.Proxied}
	if r.Type == "MX" {
		v["priority"] = r.Priority
	}
	if r.TTL == 1 {
		v["ttl"] = "auto"
	} else {
		v["ttl"] = r.TTL
	}
	if r.Comment != "" {
		v["comment"] = r.Comment
	}
	return v
}

func tools(cf *DNS, shared group.Shared) ([]tool.Tool, error) {
	pol := shared.Policy
	operator, agentName := pol.Operator, pol.Agent
	comment := fmt.Sprintf("Created by %s, approved by %s", agentName, operator)

	zoneFor := func(ctx agent.Context, dom string) (*Zone, string, error) {
		z, err := cf.FindZone(ctx, dom)
		if err != nil || z != nil {
			return z, "", err
		}
		return nil, fmt.Sprintf("Not done: Cloudflare has no zone for %s that %s's token can see. Give %s the records to add by hand.", dom, agentName, operator), nil
	}

	listSchema, err := schema.For[listArgs](types, func(p map[string]*jsonschema.Schema) {
		p["type"].Enum = schema.Enum("A", "AAAA", "CNAME", "MX", "TXT", "CAA", "SRV", "NS")
	})
	if err != nil {
		return nil, err
	}
	list, err := functiontool.New(functiontool.Config{
		Name:        "cloudflare_list_dns_records",
		Description: "List the DNS records of a domain in Cloudflare, for example to check the MX, SPF and DKIM records.",
		InputSchema: listSchema,
	}, func(ctx agent.Context, a listArgs) (map[string]any, error) {
		dom := string(a.Domain)
		z, early, err := zoneFor(ctx, dom)
		if err != nil {
			return nil, err
		}
		if z == nil {
			return map[string]any{"result": early}, nil
		}
		recs, err := cf.ListRecords(ctx, z.ID)
		if err != nil {
			return nil, err
		}
		out := []map[string]any{}
		for _, r := range recs {
			if (r.Name == dom || strings.HasSuffix(r.Name, "."+dom)) && (a.Type == "" || r.Type == a.Type) {
				out = append(out, view(r))
			}
		}
		return map[string]any{"records": out}, nil
	})
	if err != nil {
		return nil, err
	}

	createSchema, err := schema.For[createArgs](types, func(p map[string]*jsonschema.Schema) {
		p["type"].Enum = schema.Enum("MX", "TXT")
		p["content"].MinLength, p["content"].MaxLength = schema.Int(1), schema.Int(2048)
		p["priority"].Minimum, p["priority"].Maximum = schema.Num(0), schema.Num(65535)
	})
	if err != nil {
		return nil, err
	}
	create, err := approval.Tool(approval.Spec[createArgs, createData]{
		Name: "cloudflare_create_dns_record",
		Description: fmt.Sprintf("Create one MX or TXT record in Cloudflare DNS, for example a record that MXroute needs. "+
			"It cannot change or delete a record, and it makes no other record types. %s must approve this action.", operator),
		InputSchema: createSchema,
		Policy:      pol,
		Prepare: func(ctx agent.Context, a createArgs) (approval.Prep[createData], error) {
			var none approval.Prep[createData]
			if (a.Type == "MX") != (a.Priority != nil) {
				return none, fmt.Errorf("give a priority for an MX record, and no priority for a TXT record")
			}
			dom := string(a.Domain)
			z, early, err := zoneFor(ctx, dom)
			if err != nil || z == nil {
				return approval.Prep[createData]{Early: early}, err
			}
			name := fqdn(string(a.Name), dom)
			recs, err := cf.ListRecords(ctx, z.ID)
			if err != nil {
				return none, err
			}
			var atName []Record
			for _, r := range recs {
				if r.Name == name {
					atName = append(atName, r)
				}
			}
			for _, r := range atName {
				if r.Type == a.Type && same(a.Type, r.Content, a.Content) && (a.Type != "MX" || (r.Priority != nil && *r.Priority == *a.Priority)) {
					return approval.Prep[createData]{Early: fmt.Sprintf("Already in Cloudflare: %s. Nothing to do.", show(r.Type, r.Name, r.Content, r.Priority))}, nil
				}
			}
			for _, r := range atName {
				if r.Type == "CNAME" {
					return approval.Prep[createData]{Early: fmt.Sprintf("Not done: %s is a CNAME (%s). No other record can be at that name. Tell %s.", name, r.Content, operator)}, nil
				}
			}
			if isSPF(a.Type, a.Content) {
				for _, r := range atName {
					if isSPF(r.Type, r.Content) {
						return approval.Prep[createData]{Early: fmt.Sprintf("Not done: %s already has an SPF record: %s. A name must have one SPF record only, and you cannot change records. Give %s both values to merge by hand.", name, r.Content, operator)}, nil
					}
				}
			}
			now := "none"
			if len(atName) > 0 {
				s := make([]string, len(atName))
				for i, r := range atName {
					s[i] = show(r.Type, r.Name, r.Content, r.Priority)
				}
				now = strings.Join(s, "; ")
			}
			return approval.Prep[createData]{
				Summary: fmt.Sprintf("Create in Cloudflare (zone %s): %s. Records at that name now: %s", z.Name, show(a.Type, name, a.Content, a.Priority), now),
				Data:    createData{ZoneID: z.ID, Name: name},
			}, nil
		},
		Run: func(ctx agent.Context, a createArgs, d createData) (any, error) {
			r := NewRecord{Type: a.Type, Name: d.Name, Content: a.Content, Comment: comment}
			if a.Type == "MX" {
				r.Priority = a.Priority
			}
			created, err := cf.CreateRecord(ctx, d.ZoneID, r)
			if err != nil {
				return nil, err
			}
			return fmt.Sprintf("Created %s.", show(created.Type, created.Name, created.Content, created.Priority)), nil
		},
	})
	if err != nil {
		return nil, err
	}
	return []tool.Tool{list, create}, nil
}
