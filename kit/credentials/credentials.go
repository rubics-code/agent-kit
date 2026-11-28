// Package credentials is how an agent gets its keys. There are three modes:
//
//   - env: the real values are in the environment (for example from
//     `infisical run`).
//   - agent-vault: `infisical agent-vault run` starts the agent behind the
//     Agent Vault proxy. The agent sends placeholders such as __EXAMPLE_KEY__,
//     and the proxy puts in the real values, so the process never holds a key.
//     The session's services (SessionHosts) decide which groups are on.
//   - identity: the agent logs in with its Infisical machine identity and reads
//     its keys from its own Infisical folder.
package credentials

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/rubics-code/agent-kit/kit/infisical"
)

// Mode is how the agent gets its keys.
type Mode string

const (
	Env        Mode = "env"
	AgentVault Mode = "agent-vault"
	Identity   Mode = "identity"
)

// Environ is the process environment as a map. FromOS gives the real one.
type Environ map[string]string

// FromOS reads the process environment.
func FromOS() Environ {
	e := Environ{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			e[k] = v
		}
	}
	return e
}

func (e Environ) proxy() string {
	if v := e["HTTPS_PROXY"]; v != "" {
		return v
	}
	return e["https_proxy"]
}

// Credentials gives the agent's keys.
type Credentials struct {
	Mode Mode
	get  func(name string) (string, error)
	has  func(name string) bool
}

// Get gives a key, or an error when it is not there. In Agent Vault mode it
// gives the placeholder.
func (c Credentials) Get(name string) (string, error) { return c.get(name) }

// Optional gives a key, or "" when it is not there.
func (c Credentials) Optional(name string) string {
	v, err := c.get(name)
	if err != nil {
		return ""
	}
	return v
}

// Holds says whether the session holds a key, for the tool group check.
func (c Credentials) Holds(name string) bool { return c.has(name) }

// Placeholder is the value that the Agent Vault proxy replaces. The Agent
// Vault services must use this form.
func Placeholder(name string) string { return "__" + name + "__" }

// IsAgentVault reports Agent Vault mode. `infisical agent-vault run` puts the
// session token into HTTPS_PROXY as the password of the user x-agent-vault.
// Some CLI versions also set INFISICAL_AGENT_VAULT_SESSION_TOKEN.
func IsAgentVault(e Environ) bool {
	if e["INFISICAL_AGENT_VAULT_SESSION_TOKEN"] != "" {
		return true
	}
	u, err := url.Parse(e.proxy())
	return err == nil && u.User != nil && u.User.Username() == "x-agent-vault"
}

// Load gives env mode or Agent Vault mode, from the environment.
func Load(e Environ) (Credentials, error) {
	if IsAgentVault(e) {
		if e.proxy() == "" {
			return Credentials{}, errors.New("Agent Vault mode: HTTPS_PROXY is not set")
		}
		// Go's HTTP client skips the proxy for every host when NO_PROXY is *,
		// and then the placeholders would go directly to the APIs.
		if np := firstOf(e, "NO_PROXY", "no_proxy"); strings.TrimSpace(np) == "*" {
			return Credentials{}, errors.New("Agent Vault mode: NO_PROXY=* sends the requests past the proxy. Remove it")
		}
		get := func(name string) (string, error) {
			if e[name] != "" {
				return "", fmt.Errorf("Agent Vault mode: %s is in the environment. Remove it", name)
			}
			return Placeholder(name), nil
		}
		return Credentials{Mode: AgentVault, get: get, has: func(name string) bool { return e[name] == "" }}, nil
	}
	return Credentials{
		Mode: Env,
		get: func(name string) (string, error) {
			if v := e[name]; v != "" {
				return v, nil
			}
			return "", fmt.Errorf("%s is not set", name)
		},
		has: func(name string) bool { return e[name] != "" },
	}, nil
}

func firstOf(e Environ, keys ...string) string {
	for _, k := range keys {
		if v := e[k]; v != "" {
			return v
		}
	}
	return ""
}

// IdentityFile is the agent's Infisical machine identity.
type IdentityFile struct {
	ClientID, ClientSecret string
}

// ReadIdentityFile reads the identity from a .env file, directly. A shell can
// export its own INFISICAL_CLIENT_ID, so the file must not go through the
// environment. ok is false when there is no file.
func ReadIdentityFile(path string) (id IdentityFile, ok bool, err error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return IdentityFile{}, false, nil
	}
	if err != nil {
		return IdentityFile{}, false, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, found := strings.Cut(strings.TrimPrefix(line, "export "), "=")
		if !found {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		switch strings.TrimSpace(k) {
		case "INFISICAL_CLIENT_ID":
			id.ClientID = v
		case "INFISICAL_CLIENT_SECRET":
			id.ClientSecret = v
		}
	}
	if err := sc.Err(); err != nil {
		return IdentityFile{}, false, err
	}
	if id.ClientID == "" || id.ClientSecret == "" {
		return IdentityFile{}, false, fmt.Errorf("%s needs INFISICAL_CLIENT_ID and INFISICAL_CLIENT_SECRET", path)
	}
	return id, true, nil
}

// InfisicalConfig is the agent's Infisical deployment, project and own folder.
type InfisicalConfig struct {
	URL       string
	ProjectID string
	Path      string
	Env       func(path string) string
}

// LoadIdentity logs in with the identity and reads the agent's keys from its
// folder. The secret stays in the client only.
func LoadIdentity(ctx context.Context, id IdentityFile, cfg InfisicalConfig, client *http.Client) (Credentials, *infisical.Client, error) {
	if id.ClientID == "" || id.ClientSecret == "" {
		return Credentials{}, nil, errors.New("identity mode: the client ID and the client secret are not set")
	}
	inf := infisical.New(infisical.Auth{ClientID: id.ClientID, ClientSecret: id.ClientSecret},
		infisical.Config{BaseURL: cfg.URL, ProjectID: cfg.ProjectID, Env: cfg.Env, Client: client})
	values, err := inf.ReadAll(ctx, cfg.Path)
	if err != nil {
		return Credentials{}, nil, err
	}
	return Credentials{
		Mode: Identity,
		get: func(name string) (string, error) {
			if v := values[name]; v != "" {
				return v, nil
			}
			return "", fmt.Errorf("%s is not in Infisical %s", name, cfg.Path)
		},
		has: func(name string) bool { return values[name] != "" },
	}, inf, nil
}

// SessionToken is the Agent Vault session token: the password in HTTPS_PROXY
// (user x-agent-vault). It grants this session only.
func SessionToken(e Environ) string {
	if t := e["INFISICAL_AGENT_VAULT_SESSION_TOKEN"]; t != "" {
		return t
	}
	u, err := url.Parse(e.proxy())
	if err != nil || u.User == nil || u.User.Username() != "x-agent-vault" {
		return ""
	}
	p, _ := u.User.Password()
	return p
}

// SessionHosts gives the hosts of the Agent Vault session's services that are
// ready, or nil when Infisical does not answer (an older version, for example).
func SessionHosts(ctx context.Context, infisicalURL string, e Environ, client *http.Client) map[string]bool {
	base := firstOf(e, "INFISICAL_AGENT_VAULT_URL")
	if base == "" {
		base = infisicalURL
	}
	base = strings.TrimSuffix(strings.TrimRight(base, "/"), "/api")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/agent-vault/session/services", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")
	if t := SessionToken(e); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil
	}
	var out struct {
		Services []struct {
			Hosts  []string `json:"hosts"`
			Status string   `json:"status"`
		} `json:"services"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out) != nil {
		return nil
	}
	hosts := map[string]bool{}
	for _, s := range out.Services {
		if s.Status != "" && s.Status != "ready" {
			continue
		}
		for _, h := range s.Hosts {
			hosts[strings.TrimSuffix(strings.ToLower(h), ":443")] = true
		}
	}
	return hosts
}

// CAEnv names the variables that may point to a PEM file with an extra CA,
// such as the Agent Vault CA, in order.
var CAEnv = []string{"KIT_EXTRA_CA_CERTS", "NODE_EXTRA_CA_CERTS", "SSL_CERT_FILE"}

// HTTPClient gives a client for every outside call of an agent. It keeps
// Proxy: http.ProxyFromEnvironment, so Agent Vault works, and it trusts an
// extra CA from CAEnv as well as the system roots. On Linux Go reads
// SSL_CERT_FILE itself; on macOS it does not, so the Kit adds the file.
func HTTPClient(e Environ) (*http.Client, error) {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = http.ProxyFromEnvironment
	if file := firstOf(e, CAEnv...); file != "" {
		pem, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read the CA file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%s has no PEM certificate", file)
		}
		t.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{Transport: t}, nil
}
