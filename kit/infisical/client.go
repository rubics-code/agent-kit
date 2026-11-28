// Package infisical is a client for the Infisical API. It logs in with a
// machine identity (Universal Auth), or sends a static token such as an
// Agent Vault placeholder. It implements secrets.Store.
package infisical

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rubics-code/agent-kit/kit/secrets"
)

// Error is an error answer from Infisical.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("Infisical %d: %s", e.Status, e.Message) }

// Auth is a machine identity, or a static token.
type Auth struct {
	ClientID, ClientSecret string
	Token                  string // a static token; when set, the client never logs in
}

// Config says where the project is.
type Config struct {
	BaseURL   string
	ProjectID string
	// Env gives the Infisical environment of a folder path.
	Env    func(path string) string
	Client *http.Client
	Now    func() time.Time
}

// Client talks to one Infisical project.
type Client struct {
	auth Auth
	cfg  Config

	mu    sync.Mutex
	token string
	until time.Time
}

var _ secrets.Store = (*Client)(nil)

func New(auth Auth, cfg Config) *Client {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Env == nil {
		cfg.Env = func(string) string { return "prod" }
	}
	return &Client{auth: auth, cfg: cfg}
}

func (c *Client) login(ctx context.Context) (string, error) {
	if c.auth.Token != "" {
		return c.auth.Token, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && c.cfg.Now().Before(c.until) {
		return c.token, nil
	}
	body, _ := json.Marshal(map[string]string{"clientId": c.auth.ClientID, "clientSecret": c.auth.ClientSecret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/api/v1/auth/universal-auth/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := c.cfg.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var out struct {
		AccessToken string `json:"accessToken"`
		ExpiresIn   int    `json:"expiresIn"`
		Message     string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out)
	if res.StatusCode != http.StatusOK || out.AccessToken == "" {
		msg := out.Message
		if msg == "" {
			msg = res.Status
		}
		return "", &Error{res.StatusCode, "login failed: " + msg}
	}
	if out.ExpiresIn == 0 {
		out.ExpiresIn = 600
	}
	c.token = out.AccessToken
	// Renew one minute early.
	c.until = c.cfg.Now().Add(time.Duration(out.ExpiresIn-60) * time.Second)
	return c.token, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	for attempt := 0; ; attempt++ {
		token, err := c.login(ctx)
		if err != nil {
			return err
		}
		var rd io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				return err
			}
			rd = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, rd)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := c.cfg.Client.Do(req)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
		res.Body.Close()
		if err != nil {
			return err
		}
		if res.StatusCode == http.StatusUnauthorized && attempt == 0 && c.auth.Token == "" {
			c.mu.Lock()
			c.token = ""
			c.mu.Unlock()
			continue
		}
		if res.StatusCode < 200 || res.StatusCode > 299 {
			var e struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(data, &e)
			if e.Message == "" {
				e.Message = res.Status
			}
			return &Error{res.StatusCode, e.Message}
		}
		if out == nil || len(data) == 0 {
			return nil
		}
		if err := json.Unmarshal(data, out); err != nil {
			return &Error{res.StatusCode, "not JSON: " + string(data[:min(len(data), 200)])}
		}
		return nil
	}
}

func (c *Client) q(envPath string, extra map[string]string) string {
	v := url.Values{"workspaceId": {c.cfg.ProjectID}, "environment": {c.cfg.Env(envPath)}}
	for k, x := range extra {
		v.Set(k, x)
	}
	return v.Encode()
}

// ReadAll gives every secret in a folder, with values. It is for the agent's
// own keys at start only. No tool may reach it.
func (c *Client) ReadAll(ctx context.Context, path string) (map[string]string, error) {
	var out struct {
		Secrets []struct {
			SecretKey   string `json:"secretKey"`
			SecretValue string `json:"secretValue"`
		} `json:"secrets"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v3/secrets/raw?"+c.q(path, map[string]string{"secretPath": path}), nil, &out); err != nil {
		return nil, err
	}
	m := make(map[string]string, len(out.Secrets))
	for _, s := range out.Secrets {
		m[s.SecretKey] = s.SecretValue
	}
	return m, nil
}

// folders lists the folders in path, in the environment of envPath. To check
// that /agents exists, list / in the environment of /agents.
func (c *Client) folders(ctx context.Context, path, envPath string) ([]string, error) {
	var out struct {
		Folders []struct {
			Name string `json:"name"`
		} `json:"folders"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/folders?"+c.q(envPath, map[string]string{"path": path}), nil, &out); err != nil {
		return nil, err
	}
	names := make([]string, len(out.Folders))
	for i, f := range out.Folders {
		names[i] = f.Name
	}
	slices.Sort(names)
	return names, nil
}

func (c *Client) ListFolders(ctx context.Context, path string) ([]string, error) {
	return c.folders(ctx, path, path)
}

func (c *Client) ListSecretNames(ctx context.Context, path string) ([]string, error) {
	var out struct {
		Secrets []struct {
			SecretKey string `json:"secretKey"`
		} `json:"secrets"`
	}
	q := c.q(path, map[string]string{"secretPath": path, "viewSecretValue": "false"})
	if err := c.do(ctx, http.MethodGet, "/api/v3/secrets/raw?"+q, nil, &out); err != nil {
		return nil, err
	}
	names := make([]string, len(out.Secrets))
	for i, s := range out.Secrets {
		names[i] = s.SecretKey
	}
	slices.Sort(names)
	return names, nil
}

func (c *Client) folderExists(ctx context.Context, path string) (bool, error) {
	if path == "/" {
		return true, nil
	}
	i := strings.LastIndex(path, "/")
	parent, name := path[:i], path[i+1:]
	if parent == "" {
		parent = "/"
	}
	names, err := c.folders(ctx, parent, path)
	if e, ok := err.(*Error); ok && e.Status == http.StatusNotFound {
		return false, nil
	}
	return slices.Contains(names, name), err
}

func (c *Client) Check(ctx context.Context, p secrets.Place) (bool, bool, error) {
	folder, err := c.folderExists(ctx, p.Path)
	if err != nil || !folder {
		return folder, false, err
	}
	names, err := c.ListSecretNames(ctx, p.Path)
	return true, slices.Contains(names, p.Name), err
}

func (c *Client) Put(ctx context.Context, p secrets.Place, value string) error {
	at := ""
	for part := range strings.SplitSeq(p.Path, "/") {
		if part == "" {
			continue
		}
		parent := at
		if parent == "" {
			parent = "/"
		}
		at += "/" + part
		ok, err := c.folderExists(ctx, at)
		if err != nil {
			return err
		}
		if !ok {
			body := map[string]string{"workspaceId": c.cfg.ProjectID, "environment": c.cfg.Env(at), "name": part, "path": parent}
			if err := c.do(ctx, http.MethodPost, "/api/v1/folders", body, nil); err != nil {
				return err
			}
		}
	}
	names, err := c.ListSecretNames(ctx, p.Path)
	if err != nil {
		return err
	}
	method := http.MethodPost
	if slices.Contains(names, p.Name) {
		method = http.MethodPatch
	}
	body := map[string]string{"workspaceId": c.cfg.ProjectID, "environment": c.cfg.Env(p.Path), "secretPath": p.Path, "secretValue": value, "type": "shared"}
	return c.do(ctx, method, "/api/v3/secrets/raw/"+url.PathEscape(p.Name), body, nil)
}
