// Package schema makes the input schemas of tools. ADK checks each call
// against the schema before the tool runs, so a call that breaks a rule never
// gets to the approval step.
package schema

import (
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/rubics-code/agent-kit/kit/secrets"
)

// Types gives a schema to a named Go type, for example Domain.
type Types map[reflect.Type]*jsonschema.Schema

// Of gives the reflect type of T, as a key for Types.
func Of[T any]() reflect.Type { return reflect.TypeFor[T]() }

// Domain is a lowercase domain name.
type Domain string

var base = Types{
	Of[Domain](): {
		Type:        "string",
		Pattern:     `^([a-z0-9](-?[a-z0-9])*\.)+[a-z]{2,}$`,
		MaxLength:   Int(253),
		Description: "Domain name, lowercase, for example example.com",
	},
}

// For makes the schema of the argument struct T. The types map adds to the
// shared types. Patch, when it is not nil, changes the schema's properties,
// for example to set a limit that comes from the policy.
func For[T any](types Types, patch func(props map[string]*jsonschema.Schema)) (*jsonschema.Schema, error) {
	all := Types{}
	for k, v := range base {
		all[k] = v
	}
	for k, v := range types {
		all[k] = v
	}
	s, err := jsonschema.For[T](&jsonschema.ForOptions{TypeSchemas: all})
	if err != nil {
		return nil, fmt.Errorf("the schema of %s: %w", reflect.TypeFor[T](), err)
	}
	if patch != nil {
		patch(s.Properties)
	}
	return s, nil
}

// Int gives a pointer, for the schema's limits.
func Int(n int) *int { return &n }

// Num gives a pointer, for the schema's limits.
func Num(n float64) *float64 { return &n }

// Enum gives the values of an enum.
func Enum(values ...string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}

// SecretName is a secret name: upper case with underscores.
type SecretName string

// StoreAt is where a new secret goes in the secret store. The tool checks the
// path against the policy before the approval.
type StoreAt struct {
	Path string     `json:"path" jsonschema:"The Infisical folder, for example /agents/example"`
	Name SecretName `json:"name"`
}

// Place gives the store place, or nil when s is nil.
func (s *StoreAt) Place() *secrets.Place {
	if s == nil {
		return nil
	}
	return &secrets.Place{Path: s.Path, Name: string(s.Name)}
}

// StoreAtText is the description of a storeAt argument.
func StoreAtText(operator string) string {
	return "Optional. The Infisical folder and name to store the new secret in. Without it, the secret goes to " + operator + "'s terminal."
}

func init() {
	base[Of[SecretName]()] = &jsonschema.Schema{
		Type:        "string",
		Pattern:     `^[A-Z][A-Z0-9_]{0,127}$`,
		Description: "Secret name, upper case with underscores, for example API_SERVER_KEY",
	}
}
