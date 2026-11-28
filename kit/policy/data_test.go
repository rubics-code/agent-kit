package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `{
  "agent": "Example",
  "operator": "Sam",
  "maxKeyLimitUsd": 25,
  "infisical": {
    "readable": ["/ops", "/ops/**", "/agents", "/agents/**"],
    "writable": ["/ops", "/ops/**", "/agents/*", "/agents/*/**"],
    "envs": [{"path": "/agents/**", "env": "staging"}, {"path": "/**", "env": "prod"}]
  }
}`

func TestReadsAPolicyFromData(t *testing.T) {
	p, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if p.Agent != "Example" || p.Operator != "Sam" || p.MaxKeyLimitUSD != 25 {
		t.Fatalf("%+v", p)
	}
	for path, ok := range map[string]bool{"/ops": true, "/ops/a/b": true, "/agents/example": true, "/agents/x/inbox": true, "/agents": false, "/": false, "/other": false, "/agents/../ops": false, "/ops/": false, "/Ops": false} {
		if err := p.Infisical.Writable(path); (err == nil) != ok {
			t.Errorf("writable %s: %v", path, err)
		}
	}
	if err := p.Infisical.Readable("/agents"); err != nil {
		t.Fatal(err)
	}
	if err := p.Infisical.Writable("/agents"); err == nil || !strings.Contains(err.Error(), "Use /ops, /ops/**, /agents/*, /agents/*/**") {
		t.Fatal(err)
	}
	if p.Infisical.EnvLabel("/agents/example") != "staging" || p.Infisical.EnvLabel("/ops") != "prod" || p.Infisical.EnvLabel("/") != "prod" {
		t.Fatal("env labels")
	}
}

func TestRefusesABadPolicy(t *testing.T) {
	for name, body := range map[string]string{
		"not JSON":        `{`,
		"no operator":     `{"agent":"A"}`,
		"unknown field":   `{"agent":"A","operator":"O","spendLimit":5}`,
		"bad pattern":     `{"agent":"A","operator":"O","infisical":{"writable":["agents/*"]}}`,
		"star in middle":  `{"agent":"A","operator":"O","infisical":{"writable":["/**/x"]}}`,
		"env with no env": `{"agent":"A","operator":"O","infisical":{"envs":[{"path":"/x"}]}}`,
		"negative limit":  `{"agent":"A","operator":"O","maxKeyLimitUsd":-1}`,
	} {
		if _, err := Parse([]byte(body)); err == nil {
			t.Errorf("%s passed", name)
		}
	}
}

func TestAPolicyWithNoPathsAllowsNoFolder(t *testing.T) {
	p, err := Parse([]byte(`{"agent":"A","operator":"O"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Infisical.Writable("/ops"); err == nil || !strings.Contains(err.Error(), "allows no writable") {
		t.Fatal(err)
	}
}

func TestFillsThePlaceholders(t *testing.T) {
	p, _ := Parse([]byte(sample))
	if got := p.Fill("{{agent}} asks {{operator}}; the cap is {{maxKeyLimitUsd}} USD."); got != "Example asks Sam; the cap is 25 USD." {
		t.Fatal(got)
	}
}

func TestLoadsAPolicyFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "policy.json")
	_ = os.WriteFile(f, []byte(sample), 0o600)
	if p, err := Load(f); err != nil || p.Agent != "Example" {
		t.Fatal(err)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "none.json")); err == nil {
		t.Fatal("a missing file passed")
	}
}
