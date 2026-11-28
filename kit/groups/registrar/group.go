package registrar

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"

	"github.com/rubics-code/agent-kit/kit/approval"
	"github.com/rubics-code/agent-kit/kit/group"
	"github.com/rubics-code/agent-kit/kit/groups/cloudflare"
	"github.com/rubics-code/agent-kit/kit/schema"
)

//go:embed guidance.md
var guidance string

// DashboardOnly are the extensions that the API cannot register. The
// dashboard can.
var DashboardOnly = []string{"giving", "mom", "inc", "lol", "sh", "link", "cc", "new"}

const polls = 10

type searchArgs struct {
	Query string `json:"query" jsonschema:"A word, a phrase or a domain name"`
	Limit int    `json:"limit,omitempty" jsonschema:"The maximum number of results, default 20"`
}

type checkArgs struct {
	Domains []schema.Domain `json:"domains"`
}

type registerArgs struct {
	Domain schema.Domain `json:"domain"`
}

// Group is the Cloudflare Registrar tool group. The price in the approval
// comes from a live check in the same call, never from the model.
var Group = group.New(group.Def[*Client]{
	ID:   "registrar",
	Name: "Cloudflare Registrar",
	Host: cloudflare.Host,
	Keys: []string{cloudflare.TokenKey},
	Setup: "Give the Cloudflare API token Registrar permission, and add a default payment method and a default contact " +
		"in the Cloudflare dashboard. Then restart the agent.",
	Guidance: guidance,
	Connect: func(c group.ConnectContext) (*Client, bool) {
		if !c.Holds(cloudflare.TokenKey) {
			return nil, false
		}
		return &Client{API: cloudflare.API{Token: c.Get(cloudflare.TokenKey), HTTP: c.Client()}, PollDelay: 3 * time.Second}, true
	},
	Tools: tools,
})

func views(offers []Offer) map[string]any {
	if offers == nil {
		offers = []Offer{}
	}
	return map[string]any{"domains": offers}
}

func report(d, operator string, s Status) string {
	switch s.State {
	case "succeeded":
		exp := ""
		if s.Context != nil && s.Context.Registration != nil && s.Context.Registration.ExpiresAt != "" {
			exp = ", expires " + s.Context.Registration.ExpiresAt
		}
		return fmt.Sprintf("Registered %s%s. Auto-renew is off. Terraform must add the DNS zone before mail records.", d, exp)
	case "action_required":
		action, sent := "unknown", ""
		if s.Context != nil {
			if s.Context.Action != "" {
				action = s.Context.Action
			}
			if s.Context.ConfirmationSentTo != "" {
				sent = " (sent to " + s.Context.ConfirmationSentTo + ")"
			}
		}
		return fmt.Sprintf("Not finished: Cloudflare needs an action for %s: %s%s. Tell %s. Do not register again.", d, action, sent, operator)
	case "failed":
		code, msg := "", ""
		if s.Error != nil {
			code, msg = s.Error.Code, s.Error.Message
		}
		return strings.TrimSpace(fmt.Sprintf("Failed: Cloudflare did not register %s: %s %s", d, code, msg))
	case "blocked":
		return fmt.Sprintf("Blocked: Cloudflare blocked the registration of %s. Tell %s to look in the dashboard. Do not register again.", d, operator)
	}
	return fmt.Sprintf("Still %s: Cloudflare has not finished %s. Tell %s to look in the dashboard later. Do not register again.", s.State, d, operator)
}

func tools(reg *Client, shared group.Shared) ([]tool.Tool, error) {
	pol := shared.Policy
	operator := pol.Operator
	dashboard := func(d, why string) string {
		return fmt.Sprintf("Not done: the API cannot register %s (%s). Tell %s to register it in the Cloudflare dashboard.", d, why, operator)
	}

	searchSchema, err := schema.For[searchArgs](nil, func(p map[string]*jsonschema.Schema) {
		p["query"].MinLength, p["query"].MaxLength = schema.Int(1), schema.Int(100)
		p["limit"].Minimum, p["limit"].Maximum = schema.Num(1), schema.Num(50)
	})
	if err != nil {
		return nil, err
	}
	search, err := functiontool.New(functiontool.Config{
		Name:        "cloudflare_search_domains",
		Description: "Search Cloudflare Registrar for domains that match a word or a name. The availability is advisory: the register tool checks it again.",
		InputSchema: searchSchema,
	}, func(ctx agent.Context, a searchArgs) (map[string]any, error) {
		limit := a.Limit
		if limit == 0 {
			limit = 20
		}
		offers, err := reg.Search(ctx, a.Query, limit)
		if err != nil {
			return nil, err
		}
		return views(offers), nil
	})
	if err != nil {
		return nil, err
	}

	checkSchema, err := schema.For[checkArgs](nil, func(p map[string]*jsonschema.Schema) {
		p["domains"].MinItems, p["domains"].MaxItems = schema.Int(1), schema.Int(20)
	})
	if err != nil {
		return nil, err
	}
	check, err := functiontool.New(functiontool.Config{
		Name:        "cloudflare_check_domains",
		Description: "Check with the registry, now, if domains are available, and get their prices.",
		InputSchema: checkSchema,
	}, func(ctx agent.Context, a checkArgs) (map[string]any, error) {
		ds := make([]string, len(a.Domains))
		for i, d := range a.Domains {
			ds[i] = string(d)
		}
		offers, err := reg.Check(ctx, ds)
		if err != nil {
			return nil, err
		}
		return views(offers), nil
	})
	if err != nil {
		return nil, err
	}

	registerSchema, err := schema.For[registerArgs](nil, nil)
	if err != nil {
		return nil, err
	}
	register, err := approval.Tool(approval.Spec[registerArgs, struct{}]{
		Name:        "cloudflare_register_domain",
		Description: fmt.Sprintf("Register one domain in Cloudflare for one year. This costs money and has no refund. %s must approve this action.", operator),
		InputSchema: registerSchema,
		Policy:      pol,
		Prepare: func(ctx agent.Context, a registerArgs) (approval.Prep[struct{}], error) {
			d := string(a.Domain)
			early := func(s string) (approval.Prep[struct{}], error) { return approval.Prep[struct{}]{Early: s}, nil }
			ext := d[strings.LastIndex(d, ".")+1:]
			if slices.Contains(DashboardOnly, ext) {
				return early(dashboard(d, "."+ext))
			}
			offers, err := reg.Check(ctx, []string{d})
			if err != nil {
				return approval.Prep[struct{}]{}, err
			}
			var o Offer
			if len(offers) > 0 {
				o = offers[0]
			}
			if !o.Registrable || o.Pricing == nil {
				switch {
				case o.Reason == "domain_premium" || o.Tier == "premium":
					return early(dashboard(d, "premium"))
				case strings.HasPrefix(o.Reason, "extension_not_supported"):
					return early(dashboard(d, o.Reason))
				}
				reason := o.Reason
				if reason == "" {
					reason = "unknown"
				}
				return early(fmt.Sprintf("Not done: %s is not available (%s).", d, reason))
			}
			if o.Tier == "premium" {
				return early(dashboard(d, "premium"))
			}
			p := o.Pricing
			return approval.Prep[struct{}]{
				Summary: fmt.Sprintf("Register %s in Cloudflare for 1 year: %s %s now, renewal %s %s a year. Auto-renew off, WHOIS redaction on, default contact. No refund.",
					d, p.RegistrationCost, p.Currency, p.RenewalCost, p.Currency),
				// The gate hashes the price too, so an approval cannot cover a changed price.
				Args: map[string]any{"domain": d, "currency": p.Currency, "registration_cost": p.RegistrationCost},
			}, nil
		},
		Run: func(ctx agent.Context, a registerArgs, _ struct{}) (any, error) {
			d := string(a.Domain)
			s, err := reg.Register(ctx, d)
			if err != nil {
				return nil, err
			}
			for i := 0; i < polls && (s.State == "pending" || s.State == "in_progress"); i++ {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(reg.PollDelay):
				}
				if s, err = reg.Status(ctx, d); err != nil {
					return nil, err
				}
			}
			return report(d, operator, s), nil
		},
	})
	if err != nil {
		return nil, err
	}
	return []tool.Tool{search, check, register}, nil
}
