package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Data is a policy as a JSON file, for example policy.json in the agent's
// home. Path patterns use "*" for one folder name and "**" at the end for any
// number of folder names, so "/agents/*/**" is any folder under /agents.
type Data struct {
	Agent          string  `json:"agent"`
	Operator       string  `json:"operator"`
	MaxKeyLimitUSD float64 `json:"maxKeyLimitUsd"`
	Infisical      struct {
		Readable []string `json:"readable"`
		Writable []string `json:"writable"`
		// Envs maps paths to Infisical environments. The first match wins.
		Envs []struct {
			Path string `json:"path"`
			Env  string `json:"env"`
		} `json:"envs"`
	} `json:"infisical"`
}

var segment = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// Pattern is a path pattern.
type Pattern struct {
	parts []string
	rest  bool // ends in "**"
}

// ParsePattern checks and parses a path pattern.
func ParsePattern(s string) (Pattern, error) {
	if s == "/" {
		return Pattern{}, nil
	}
	if !strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") {
		return Pattern{}, fmt.Errorf("the pattern %q must start with / and not end with /", s)
	}
	var p Pattern
	parts := strings.Split(s[1:], "/")
	for i, part := range parts {
		switch {
		case part == "**" && i == len(parts)-1:
			p.rest = true
		case part == "*" || segment.MatchString(part):
			p.parts = append(p.parts, part)
		default:
			return Pattern{}, fmt.Errorf("the pattern %q has a bad part %q", s, part)
		}
	}
	return p, nil
}

// SplitPath checks a folder path and gives its folder names.
func SplitPath(path string) ([]string, error) {
	if path == "/" {
		return nil, nil
	}
	if !strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return nil, fmt.Errorf("the folder %q must start with / and not end with /", path)
	}
	parts := strings.Split(path[1:], "/")
	for _, part := range parts {
		if !segment.MatchString(part) {
			return nil, fmt.Errorf("the folder %q has a bad name %q: use lower-case letters, digits, - and _", path, part)
		}
	}
	return parts, nil
}

// Match reports whether a checked path matches.
func (p Pattern) Match(parts []string) bool {
	if len(parts) < len(p.parts) || (!p.rest && len(parts) != len(p.parts)) {
		return false
	}
	for i, want := range p.parts {
		if want != "*" && want != parts[i] {
			return false
		}
	}
	return true
}

func patterns(list []string) ([]Pattern, error) {
	out := make([]Pattern, 0, len(list))
	for _, s := range list {
		p, err := ParsePattern(s)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func rule(kind string, list []string, pats []Pattern) func(string) error {
	return func(path string) error {
		parts, err := SplitPath(path)
		if err != nil {
			return err
		}
		for _, p := range pats {
			if p.Match(parts) {
				return nil
			}
		}
		if len(list) == 0 {
			return fmt.Errorf("the policy allows no %s Infisical folder", kind)
		}
		return fmt.Errorf("%s is not a %s folder. Use %s", path, kind, strings.Join(list, ", "))
	}
}

// Parse makes a Policy from JSON. Unknown fields are an error, so a typo
// cannot loosen a rule.
func Parse(b []byte) (Policy, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var d Data
	if err := dec.Decode(&d); err != nil {
		return Policy{}, fmt.Errorf("the policy is not valid JSON: %w", err)
	}
	return FromData(d)
}

// Load reads a policy file.
func Load(path string) (Policy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, err
	}
	p, err := Parse(b)
	if err != nil {
		return Policy{}, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

// FromData checks the data and makes a Policy.
func FromData(d Data) (Policy, error) {
	var errs []error
	if strings.TrimSpace(d.Agent) == "" {
		errs = append(errs, errors.New("agent is empty"))
	}
	if strings.TrimSpace(d.Operator) == "" {
		errs = append(errs, errors.New("operator is empty"))
	}
	if d.MaxKeyLimitUSD < 0 {
		errs = append(errs, errors.New("maxKeyLimitUsd is below 0"))
	}
	readable, err := patterns(d.Infisical.Readable)
	errs = append(errs, err)
	writable, err := patterns(d.Infisical.Writable)
	errs = append(errs, err)
	type envRule struct {
		p   Pattern
		env string
	}
	var envs []envRule
	for _, e := range d.Infisical.Envs {
		p, err := ParsePattern(e.Path)
		if err == nil && e.Env == "" {
			err = fmt.Errorf("the env rule for %s has no env", e.Path)
		}
		errs = append(errs, err)
		envs = append(envs, envRule{p, e.Env})
	}
	if err := errors.Join(errs...); err != nil {
		return Policy{}, err
	}
	return Policy{
		Agent:          d.Agent,
		Operator:       d.Operator,
		MaxKeyLimitUSD: d.MaxKeyLimitUSD,
		Infisical: Infisical{
			Readable: rule("readable", d.Infisical.Readable, readable),
			Writable: rule("writable", d.Infisical.Writable, writable),
			EnvLabel: func(path string) string {
				parts, err := SplitPath(path)
				if err == nil {
					for _, e := range envs {
						if e.p.Match(parts) {
							return e.env
						}
					}
				}
				return "prod"
			},
		},
	}, nil
}

// Fill puts the policy's values in the {{operator}}, {{agent}} and
// {{maxKeyLimitUsd}} placeholders of a text.
func (p Policy) Fill(text string) string {
	return strings.NewReplacer(
		"{{operator}}", p.Operator,
		"{{agent}}", p.Agent,
		"{{maxKeyLimitUsd}}", strconv.FormatFloat(p.MaxKeyLimitUSD, 'f', -1, 64),
	).Replace(text)
}
