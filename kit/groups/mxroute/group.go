package mxroute

import (
	"context"
	_ "embed"
	"fmt"
	"net/mail"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	"github.com/rubics-code/agent-kit/kit/approval"
	"github.com/rubics-code/agent-kit/kit/group"
	"github.com/rubics-code/agent-kit/kit/schema"
	"github.com/rubics-code/agent-kit/kit/secrets"
)

//go:embed guidance.md
var guidance string

// The env keys of the group.
const (
	ServerKey   = "MXROUTE_SERVER"
	UsernameKey = "MXROUTE_USERNAME"
	APIKeyKey   = "MXROUTE_API_KEY"
)

const passwordLength = 24

// LocalPart is the part of an address before the @. MXroute: it starts with
// a letter or a digit, and has letters, digits, '.', '_' and '-'.
type LocalPart string

var types = schema.Types{
	schema.Of[LocalPart](): {
		Type:        "string",
		Pattern:     `^[a-z0-9][a-z0-9._-]{0,63}$`,
		Description: "The part before the @, lowercase, for example hello",
	},
}

type domainArgs struct {
	Domain schema.Domain `json:"domain"`
}

type mailboxArgs struct {
	Domain         schema.Domain   `json:"domain"`
	Username       LocalPart       `json:"username"`
	QuotaMB        *int            `json:"quotaMb,omitempty" jsonschema:"Storage quota in MB. 0 = unlimited. Default 1024."`
	DailySendLimit int             `json:"dailySendLimit,omitempty" jsonschema:"Emails per day. Default 9600."`
	StoreAt        *schema.StoreAt `json:"storeAt,omitempty"`
}

type resetArgs struct {
	Domain   schema.Domain   `json:"domain"`
	Username LocalPart       `json:"username"`
	StoreAt  *schema.StoreAt `json:"storeAt,omitempty"`
}

type forwarderArgs struct {
	Domain       schema.Domain `json:"domain"`
	Alias        LocalPart     `json:"alias"`
	Destinations []string      `json:"destinations" jsonschema:"Addresses that get the mail"`
}

// Group is the MXroute tool group.
var Group = group.New(group.Def[*Client]{
	ID:       "mxroute",
	Name:     "MXroute",
	Host:     "api.mxroute.com",
	Keys:     []string{ServerKey, UsernameKey, APIKeyKey},
	Setup:    "Add the MXroute keys to the agent's secrets in Infisical, then restart the agent.",
	Guidance: guidance,
	Connect: func(c group.ConnectContext) (*Client, bool) {
		if !c.HoldsAll(ServerKey, UsernameKey, APIKeyKey) {
			return nil, false
		}
		return &Client{Server: c.Get(ServerKey), Username: c.Get(UsernameKey), APIKey: c.Get(APIKeyKey), HTTP: c.Client()}, true
	},
	Tools: tools,
})

func read[A any, R any](name, desc string, run func(agent.Context, A) (R, error)) (tool.Tool, error) {
	s, err := schema.For[A](types, nil)
	if err != nil {
		return nil, err
	}
	return functiontool.New(functiontool.Config{Name: name, Description: desc, InputSchema: s}, run)
}

func tools(mx *Client, shared group.Shared) ([]tool.Tool, error) {
	pol := shared.Policy
	operator := pol.Operator

	// ready checks, before any approval, that a new password has a place to go.
	ready := func(at *schema.StoreAt) (string, error) {
		place := at.Place()
		if err := secrets.Ready(shared.Secrets, shared.Store, place); err != nil {
			return err.Error(), nil
		}
		if place != nil {
			return "", secrets.CheckPlace(pol, *place)
		}
		return "", nil
	}
	where := func(ctx context.Context, at *schema.StoreAt) (string, error) {
		place := at.Place()
		if place == nil || shared.Store == nil {
			return "", nil
		}
		d, err := secrets.DescribePlace(ctx, pol, shared.Store, *place)
		if err != nil {
			return "", err
		}
		return " and store the password in " + d, nil
	}
	deliver := func(ctx context.Context, at *schema.StoreAt, label, password string) (string, error) {
		return secrets.Deliver(ctx, secrets.Delivery{Policy: pol, Sink: shared.Secrets, Store: shared.Store, Place: at.Place()}, label, password)
	}

	listDomains, err := read("mxroute_list_domains", "List the domains on the MXroute account.",
		func(ctx agent.Context, _ struct{}) (map[string]any, error) {
			ds, err := mx.Domains(ctx)
			return map[string]any{"domains": ds}, err
		})
	if err != nil {
		return nil, err
	}
	listMailboxes, err := read("mxroute_list_mailboxes", "List the mailboxes of a domain, with quota (MB), usage (MB) and daily send limit.",
		func(ctx agent.Context, a domainArgs) (map[string]any, error) {
			as, err := mx.Accounts(ctx, string(a.Domain))
			return map[string]any{"mailboxes": as}, err
		})
	if err != nil {
		return nil, err
	}
	listForwarders, err := read("mxroute_list_forwarders", "List the forwarders of a domain.",
		func(ctx agent.Context, a domainArgs) (map[string]any, error) {
			fs, err := mx.Forwarders(ctx, string(a.Domain))
			return map[string]any{"forwarders": fs}, err
		})
	if err != nil {
		return nil, err
	}
	dnsRecords, err := read("mxroute_get_dns_records", "Get the DNS records (MX, SPF, DKIM) that a domain on MXroute needs at its DNS host.",
		func(ctx agent.Context, a domainArgs) (DNSInfo, error) { return mx.DNSInfo(ctx, string(a.Domain)) })
	if err != nil {
		return nil, err
	}
	verification, err := read("mxroute_get_verification_record",
		"Get the TXT record that must exist in a domain's DNS before MXroute accepts the domain. Use it before mxroute_add_domain.",
		func(ctx agent.Context, a domainArgs) (map[string]any, error) {
			v, err := mx.VerificationKey(ctx)
			if err != nil {
				return nil, err
			}
			return map[string]any{"type": v.Record.Type, "name": v.Record.Name + "." + string(a.Domain), "value": v.Record.Value}, nil
		})
	if err != nil {
		return nil, err
	}

	domainSchema, err := schema.For[domainArgs](types, nil)
	if err != nil {
		return nil, err
	}
	addDomain, err := approval.Tool(approval.Spec[domainArgs, struct{}]{
		Name:        "mxroute_add_domain",
		Description: fmt.Sprintf("Add a domain to MXroute. The verification TXT record must be live first. %s must approve this action.", operator),
		InputSchema: domainSchema,
		Policy:      pol,
		Prepare: func(_ agent.Context, a domainArgs) (approval.Prep[struct{}], error) {
			return approval.Prep[struct{}]{Summary: fmt.Sprintf("Add the domain %s to MXroute", a.Domain)}, nil
		},
		Run: func(ctx agent.Context, a domainArgs, _ struct{}) (any, error) {
			if err := mx.AddDomain(ctx, string(a.Domain)); err != nil {
				return nil, err
			}
			return fmt.Sprintf("Added %s. Next, get its DNS records with mxroute_get_dns_records.", a.Domain), nil
		},
	})
	if err != nil {
		return nil, err
	}

	mailboxSchema, err := schema.For[mailboxArgs](types, func(p map[string]*jsonschema.Schema) {
		p["quotaMb"].Minimum, p["quotaMb"].Maximum = schema.Num(0), schema.Num(102400)
		p["dailySendLimit"].Minimum, p["dailySendLimit"].Maximum = schema.Num(1), schema.Num(9600)
		p["storeAt"].Description = schema.StoreAtText(operator)
	})
	if err != nil {
		return nil, err
	}
	createMailbox, err := approval.Tool(approval.Spec[mailboxArgs, struct{}]{
		Name: "mxroute_create_mailbox",
		Description: fmt.Sprintf("Create a mailbox. The tool makes the password and gives it to %s directly, or stores it in Infisical "+
			"with storeAt; never ask for one. %s must approve this action.", operator, operator),
		InputSchema: mailboxSchema,
		Policy:      pol,
		Prepare: func(ctx agent.Context, a mailboxArgs) (approval.Prep[struct{}], error) {
			var none approval.Prep[struct{}]
			if early, err := ready(a.StoreAt); early != "" || err != nil {
				return approval.Prep[struct{}]{Early: early}, err
			}
			if a.QuotaMB == nil {
				q := 1024
				a.QuotaMB = &q
			}
			if a.DailySendLimit == 0 {
				a.DailySendLimit = 9600
			}
			quota := "unlimited"
			if *a.QuotaMB != 0 {
				quota = fmt.Sprintf("%d MB", *a.QuotaMB)
			}
			w, err := where(ctx, a.StoreAt)
			if err != nil {
				return none, err
			}
			return approval.Prep[struct{}]{
				Summary: fmt.Sprintf("Create the mailbox %s@%s (quota %s, send limit %d per day)%s", a.Username, a.Domain, quota, a.DailySendLimit, w),
				Args:    a,
			}, nil
		},
		Run: func(ctx agent.Context, a mailboxArgs, _ struct{}) (any, error) {
			quota, limit := 1024, a.DailySendLimit
			if a.QuotaMB != nil {
				quota = *a.QuotaMB
			}
			if limit == 0 {
				limit = 9600
			}
			password := secrets.GeneratePassword(passwordLength)
			if err := mx.CreateAccount(ctx, string(a.Domain), NewAccount{Username: string(a.Username), Password: password, Quota: quota, Limit: limit}); err != nil {
				return nil, err
			}
			label := fmt.Sprintf("%s@%s", a.Username, a.Domain)
			done, err := deliver(ctx, a.StoreAt, label, password)
			if err != nil {
				return nil, err
			}
			return fmt.Sprintf("Created %s. %s", label, done), nil
		},
	})
	if err != nil {
		return nil, err
	}

	resetSchema, err := schema.For[resetArgs](types, func(p map[string]*jsonschema.Schema) {
		p["storeAt"].Description = schema.StoreAtText(operator)
	})
	if err != nil {
		return nil, err
	}
	resetPassword, err := approval.Tool(approval.Spec[resetArgs, struct{}]{
		Name: "mxroute_reset_mailbox_password",
		Description: fmt.Sprintf("Give a mailbox a new password. The tool makes it and gives it to %s directly, or stores it in Infisical "+
			"with storeAt. %s must approve this action.", operator, operator),
		InputSchema: resetSchema,
		Policy:      pol,
		Prepare: func(ctx agent.Context, a resetArgs) (approval.Prep[struct{}], error) {
			if early, err := ready(a.StoreAt); early != "" || err != nil {
				return approval.Prep[struct{}]{Early: early}, err
			}
			w, err := where(ctx, a.StoreAt)
			if err != nil {
				return approval.Prep[struct{}]{}, err
			}
			return approval.Prep[struct{}]{Summary: fmt.Sprintf("Set a new password for %s@%s. The old password stops working%s.", a.Username, a.Domain, w)}, nil
		},
		Run: func(ctx agent.Context, a resetArgs, _ struct{}) (any, error) {
			password := secrets.GeneratePassword(passwordLength)
			if err := mx.SetPassword(ctx, string(a.Domain), string(a.Username), password); err != nil {
				return nil, err
			}
			label := fmt.Sprintf("%s@%s", a.Username, a.Domain)
			done, err := deliver(ctx, a.StoreAt, label, password)
			if err != nil {
				return nil, err
			}
			return fmt.Sprintf("New password set for %s. %s", label, done), nil
		},
	})
	if err != nil {
		return nil, err
	}

	forwarderSchema, err := schema.For[forwarderArgs](types, func(p map[string]*jsonschema.Schema) {
		p["destinations"].MinItems, p["destinations"].MaxItems = schema.Int(1), schema.Int(10)
		p["destinations"].Items.Pattern = `^[^@\s]+@[^@\s]+\.[^@\s]+$`
	})
	if err != nil {
		return nil, err
	}
	createForwarder, err := approval.Tool(approval.Spec[forwarderArgs, struct{}]{
		Name: "mxroute_create_forwarder",
		Description: fmt.Sprintf("Create a forwarder. A forwarder to Gmail, Yahoo or AOL turns on Expert Spam Filtering for the domain. "+
			"%s must approve this action.", operator),
		InputSchema: forwarderSchema,
		Policy:      pol,
		Prepare: func(_ agent.Context, a forwarderArgs) (approval.Prep[struct{}], error) {
			for _, to := range a.Destinations {
				if addr, err := mail.ParseAddress(to); err != nil || addr.Address != to {
					return approval.Prep[struct{}]{}, fmt.Errorf("%q is not an email address", to)
				}
			}
			return approval.Prep[struct{}]{Summary: fmt.Sprintf("Forward %s@%s to %s", a.Alias, a.Domain, strings.Join(a.Destinations, ", "))}, nil
		},
		Run: func(ctx agent.Context, a forwarderArgs, _ struct{}) (any, error) {
			if err := mx.CreateForwarder(ctx, string(a.Domain), string(a.Alias), a.Destinations); err != nil {
				return nil, err
			}
			return fmt.Sprintf("Created the forwarder %s@%s.", a.Alias, a.Domain), nil
		},
	})
	if err != nil {
		return nil, err
	}

	return []tool.Tool{listDomains, listMailboxes, listForwarders, dnsRecords, verification,
		addDomain, createMailbox, resetPassword, createForwarder}, nil
}
