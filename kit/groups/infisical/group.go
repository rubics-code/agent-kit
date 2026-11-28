// Package infisical is the Infisical secret tool group. The readable and
// writable paths come from the agent's policy, and the identity's Infisical
// role is a second limit behind them. No tool ever gives a secret value.
package infisical

import (
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"fmt"
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

type pathArgs struct {
	Path string `json:"path" jsonschema:"An Infisical folder that your policy lets you read, for example /agents"`
}

type randomArgs struct {
	Path   string            `json:"path" jsonschema:"An Infisical folder that your policy lets you write, for example /agents/example"`
	Name   schema.SecretName `json:"name"`
	Bytes  int               `json:"bytes,omitempty" jsonschema:"Random bytes. Default 32."`
	Format string            `json:"format,omitempty" jsonschema:"Default hex."`
}

// Group is the Infisical tool group. Its client is the agent's secret store.
// The real host is the agent's Infisical deployment, so the agent gives it
// (run.Config.Infisical).
var Group = group.New(group.Def[secrets.Store]{
	ID:       group.StoreGroupID,
	Name:     "Infisical",
	Host:     "infisical",
	Setup:    "Give the agent an Infisical identity file (run.Config.IdentityFile), then restart the agent.",
	Guidance: guidance,
	Review: func(env group.EnableEnv) group.Review {
		if env.VaultHosts != nil {
			on := env.Host != "" && env.VaultHosts[strings.ToLower(env.Host)]
			missing := []string{}
			if !on && env.Host != "" {
				missing = []string{env.Host}
			}
			return group.Review{Enabled: on, Missing: missing}
		}
		if env.HasStore {
			return group.Review{Enabled: true}
		}
		return group.Review{Missing: []string{"INFISICAL_CLIENT_ID", "INFISICAL_CLIENT_SECRET"}}
	},
	Connect: func(c group.ConnectContext) (secrets.Store, bool) { return c.Store, c.Store != nil },
	Tools:   tools,
})

func check(rule func(string) error, path string) error {
	if rule == nil {
		return fmt.Errorf("the policy names no Infisical path for this")
	}
	return rule(path)
}

func tools(store secrets.Store, shared group.Shared) ([]tool.Tool, error) {
	pol := shared.Policy
	operator := pol.Operator

	listFolders, err := functiontool.New(functiontool.Config{
		Name:        "infisical_list_folders",
		Description: "List the folders in an Infisical folder, for example /agents to see the agents.",
	}, func(ctx agent.Context, a pathArgs) (map[string]any, error) {
		if err := check(pol.Infisical.Readable, a.Path); err != nil {
			return nil, err
		}
		fs, err := store.ListFolders(ctx, a.Path)
		if err != nil {
			return nil, err
		}
		return map[string]any{"folders": nonNil(fs)}, nil
	})
	if err != nil {
		return nil, err
	}

	listNames, err := functiontool.New(functiontool.Config{
		Name:        "infisical_list_secret_names",
		Description: "List the secret names in an Infisical folder. You never see a value.",
	}, func(ctx agent.Context, a pathArgs) (map[string]any, error) {
		if err := check(pol.Infisical.Readable, a.Path); err != nil {
			return nil, err
		}
		ns, err := store.ListSecretNames(ctx, a.Path)
		if err != nil {
			return nil, err
		}
		return map[string]any{"names": nonNil(ns)}, nil
	})
	if err != nil {
		return nil, err
	}

	randomSchema, err := schema.For[randomArgs](nil, func(p map[string]*jsonschema.Schema) {
		p["bytes"].Minimum, p["bytes"].Maximum = schema.Num(16), schema.Num(64)
		p["format"].Enum = schema.Enum("hex", "base64url")
	})
	if err != nil {
		return nil, err
	}
	random, err := approval.Tool(approval.Spec[randomArgs, struct{}]{
		Name: "infisical_create_random_secret",
		Description: fmt.Sprintf("Make a new random secret, for example an API_SERVER_KEY, and store it in Infisical. "+
			"It does not replace a secret that exists. %s must approve this action.", operator),
		InputSchema: randomSchema,
		Policy:      pol,
		Prepare: func(ctx agent.Context, a randomArgs) (approval.Prep[struct{}], error) {
			var none approval.Prep[struct{}]
			place := secrets.Place{Path: a.Path, Name: string(a.Name)}
			if err := secrets.CheckPlace(pol, place); err != nil {
				return none, err
			}
			_, exists, err := store.Check(ctx, place)
			if err != nil {
				return none, err
			}
			if exists {
				return approval.Prep[struct{}]{Early: fmt.Sprintf("Not done: %s already exists. You cannot replace a value with a random one. Tell %s.",
					secrets.PlaceText(pol, place), operator)}, nil
			}
			if a.Bytes == 0 {
				a.Bytes = 32
			}
			if a.Format == "" {
				a.Format = "hex"
			}
			where, err := secrets.DescribePlace(ctx, pol, store, place)
			if err != nil {
				return none, err
			}
			return approval.Prep[struct{}]{
				Summary: fmt.Sprintf("Make a random secret (%d bytes, %s) and store it in %s", a.Bytes, a.Format, where),
				Args:    a,
			}, nil
		},
		Run: func(ctx agent.Context, a randomArgs, _ struct{}) (any, error) {
			n, format := a.Bytes, a.Format
			if n == 0 {
				n = 32
			}
			b := make([]byte, n)
			if _, err := rand.Read(b); err != nil {
				return nil, err
			}
			value := hex.EncodeToString(b)
			if format == "base64url" {
				value = base64.RawURLEncoding.EncodeToString(b)
			}
			place := secrets.Place{Path: a.Path, Name: string(a.Name)}
			if err := store.Put(ctx, place, value); err != nil {
				return nil, err
			}
			return fmt.Sprintf("Stored a new random secret in %s. You do not have it.", secrets.PlaceText(pol, place)), nil
		},
	})
	if err != nil {
		return nil, err
	}
	return []tool.Tool{listFolders, listNames, random}, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
