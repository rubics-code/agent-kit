package openrouter

import (
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

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

// ManagementKey is the env key of the management key.
const ManagementKey = "OPENROUTER_MANAGEMENT_KEY"

var resets = schema.Enum("daily", "weekly", "monthly")

type listArgs struct {
	IncludeDisabled bool `json:"includeDisabled,omitempty" jsonschema:"Also list disabled keys. Default false."`
}

type createArgs struct {
	Name       string          `json:"name" jsonschema:"What the key is for, for example 'Example website chatbot'"`
	LimitUSD   float64         `json:"limitUsd"`
	LimitReset string          `json:"limitReset,omitempty" jsonschema:"When the spend limit resets. Default monthly."`
	ExpiresOn  string          `json:"expiresOn,omitempty" jsonschema:"Last day the key works, YYYY-MM-DD (UTC). Default no expiry."`
	StoreAt    *schema.StoreAt `json:"storeAt,omitempty"`
}

type updateArgs struct {
	Hash       string   `json:"hash" jsonschema:"The key hash from openrouter_list_keys"`
	LimitUSD   *float64 `json:"limitUsd,omitempty"`
	LimitReset *string  `json:"limitReset,omitempty" jsonschema:"When the spend limit resets"`
	Disabled   *bool    `json:"disabled,omitempty" jsonschema:"true stops the key; false starts it again"`
}

// Group is the OpenRouter keys tool group. The spend limit is at most the
// policy's maximum, and the schema refuses a higher one before the approval.
var Group = group.New(group.Def[*Client]{
	ID:       "openrouter",
	Name:     "OpenRouter keys",
	Host:     "openrouter.ai",
	Keys:     []string{ManagementKey},
	Setup:    "Add the OpenRouter management key to the agent's secrets in Infisical, then restart the agent.",
	Guidance: guidance,
	Connect: func(c group.ConnectContext) (*Client, bool) {
		if !c.Holds(ManagementKey) {
			return nil, false
		}
		return &Client{ManagementKey: c.Get(ManagementKey), HTTP: c.Client()}, true
	},
	Tools: tools,
})

func usd(n *float64) string {
	if n == nil {
		return "no limit"
	}
	return fmt.Sprintf("$%.2f", *n)
}

func view(k Key) map[string]any {
	return map[string]any{
		"hash": k.Hash, "name": k.Name, "label": k.Label, "disabled": k.Disabled,
		"limit_usd": k.Limit, "limit_remaining_usd": k.LimitRemaining, "limit_reset": k.LimitReset,
		"usage_month_usd": k.UsageMonthly, "usage_total_usd": k.Usage,
		"last_used_at": k.LastUsedAt, "expires_at": k.ExpiresAt,
	}
}

func tools(keys *Client, shared group.Shared) ([]tool.Tool, error) {
	pol := shared.Policy
	operator := pol.Operator
	limitText := "Spend limit in USD, at most " + strconv.FormatFloat(pol.MaxKeyLimitUSD, 'f', -1, 64)
	limit := func(s *jsonschema.Schema) {
		s.ExclusiveMinimum, s.Maximum, s.Description = schema.Num(0), schema.Num(pol.MaxKeyLimitUSD), limitText
	}

	credits, err := functiontool.New(functiontool.Config{
		Name:        "openrouter_get_credits",
		Description: "Get the OpenRouter credits bought and used, in USD.",
	}, func(ctx agent.Context, _ struct{}) (Credits, error) { return keys.Credits(ctx) })
	if err != nil {
		return nil, err
	}

	list, err := functiontool.New(functiontool.Config{
		Name:        "openrouter_list_keys",
		Description: "List the OpenRouter API keys with their spend limits and usage in USD.",
	}, func(ctx agent.Context, a listArgs) (map[string]any, error) {
		ks, err := keys.ListKeys(ctx, a.IncludeDisabled)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]any, len(ks))
		for i, k := range ks {
			out[i] = view(k)
		}
		return map[string]any{"keys": out}, nil
	})
	if err != nil {
		return nil, err
	}

	createSchema, err := schema.For[createArgs](nil, func(p map[string]*jsonschema.Schema) {
		p["name"].MinLength, p["name"].MaxLength = schema.Int(1), schema.Int(100)
		limit(p["limitUsd"])
		p["limitReset"].Enum = resets
		p["expiresOn"].Pattern = `^\d{4}-\d{2}-\d{2}$`
		p["storeAt"].Description = schema.StoreAtText(operator)
	})
	if err != nil {
		return nil, err
	}
	type createData struct{ Reset string }
	create, err := approval.Tool(approval.Spec[createArgs, createData]{
		Name: "openrouter_create_key",
		Description: fmt.Sprintf("Create an OpenRouter API key with a spend limit. The tool gives the key to %s directly, "+
			"or stores it in Infisical with storeAt; you never see it. %s must approve this action.", operator, operator),
		InputSchema: createSchema,
		Policy:      pol,
		Prepare: func(ctx agent.Context, a createArgs) (approval.Prep[createData], error) {
			var none approval.Prep[createData]
			place := a.StoreAt.Place()
			if err := secrets.Ready(shared.Secrets, shared.Store, place); err != nil {
				return approval.Prep[createData]{Early: err.Error()}, nil
			}
			if place != nil {
				if err := secrets.CheckPlace(pol, *place); err != nil {
					return none, err
				}
			}
			if a.ExpiresOn != "" {
				if _, err := time.Parse(time.DateOnly, a.ExpiresOn); err != nil {
					return none, errors.New("expiresOn must be a date, YYYY-MM-DD")
				}
			}
			if a.LimitReset == "" {
				a.LimitReset = "monthly"
			}
			expires := " with no expiry"
			if a.ExpiresOn != "" {
				expires = " until " + a.ExpiresOn
			}
			where := ""
			if place != nil {
				d, err := secrets.DescribePlace(ctx, pol, shared.Store, *place)
				if err != nil {
					return none, err
				}
				where = " and store it in " + d
			}
			lim := a.LimitUSD
			return approval.Prep[createData]{
				Summary: fmt.Sprintf("Create the OpenRouter API key %q (limit %s %s%s)%s", a.Name, usd(&lim), a.LimitReset, expires, where),
				Args:    a,
				Data:    createData{Reset: a.LimitReset},
			}, nil
		},
		Run: func(ctx agent.Context, a createArgs, d createData) (any, error) {
			in := NewKey{Name: a.Name, Limit: a.LimitUSD, LimitReset: d.Reset}
			if a.ExpiresOn != "" {
				in.ExpiresAt = a.ExpiresOn + "T23:59:59Z"
			}
			k, value, err := keys.CreateKey(ctx, in)
			if err != nil {
				return nil, err
			}
			done, err := secrets.Deliver(ctx, secrets.Delivery{Policy: pol, Sink: shared.Secrets, Store: shared.Store, Place: a.StoreAt.Place()},
				"OpenRouter key "+a.Name, value)
			if err != nil {
				return nil, err
			}
			return fmt.Sprintf("Created the key %q (%s, hash %s). %s", a.Name, k.Label, k.Hash, done), nil
		},
	})
	if err != nil {
		return nil, err
	}

	updateSchema, err := schema.For[updateArgs](nil, func(p map[string]*jsonschema.Schema) {
		p["hash"].Pattern = `^[A-Za-z0-9_-]{8,128}$`
		limit(p["limitUsd"])
		p["limitReset"].Enum = resets
	})
	if err != nil {
		return nil, err
	}
	update, err := approval.Tool(approval.Spec[updateArgs, struct{}]{
		Name: "openrouter_update_key",
		Description: fmt.Sprintf("Change the spend limit of an OpenRouter API key, or disable or enable it. "+
			"A disabled key stops at once. %s must approve this action.", operator),
		InputSchema: updateSchema,
		Policy:      pol,
		Prepare: func(ctx agent.Context, a updateArgs) (approval.Prep[struct{}], error) {
			if a.LimitUSD == nil && a.LimitReset == nil && a.Disabled == nil {
				return approval.Prep[struct{}]{}, errors.New("give at least one change: limitUsd, limitReset or disabled")
			}
			cur, err := keys.Key(ctx, a.Hash)
			if err != nil {
				return approval.Prep[struct{}]{}, err
			}
			var changes []string
			if a.LimitUSD != nil {
				changes = append(changes, fmt.Sprintf("limit %s to %s", usd(cur.Limit), usd(a.LimitUSD)))
			}
			if a.LimitReset != nil {
				was := "never"
				if cur.LimitReset != nil {
					was = *cur.LimitReset
				}
				changes = append(changes, fmt.Sprintf("reset %s to %s", was, *a.LimitReset))
			}
			if a.Disabled != nil {
				if *a.Disabled {
					changes = append(changes, "disable it")
				} else {
					changes = append(changes, "enable it")
				}
			}
			return approval.Prep[struct{}]{Summary: fmt.Sprintf("Change the OpenRouter API key %q (%s): %s", cur.Name, cur.Label, strings.Join(changes, ", "))}, nil
		},
		Run: func(ctx agent.Context, a updateArgs, _ struct{}) (any, error) {
			k, err := keys.UpdateKey(ctx, a.Hash, KeyChange{Limit: a.LimitUSD, LimitReset: a.LimitReset, Disabled: a.Disabled})
			if err != nil {
				return nil, err
			}
			return view(k), nil
		},
	})
	if err != nil {
		return nil, err
	}
	return []tool.Tool{credits, list, create, update}, nil
}
