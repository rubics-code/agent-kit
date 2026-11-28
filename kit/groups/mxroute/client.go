// Package mxroute is the MXroute tool group. Each write tool is behind the
// approval gate, and the tools make the passwords, so the model never sees one.
package mxroute

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// DefaultBaseURL is the MXroute REST API.
const DefaultBaseURL = "https://api.mxroute.com"

// Account is a mailbox.
type Account struct {
	Username  string `json:"username"`
	Email     string `json:"email"`
	Quota     int    `json:"quota"` // MB, 0 is unlimited
	Usage     int    `json:"usage"` // MB
	Limit     int    `json:"limit"` // sends a day
	Sent      int    `json:"sent"`
	Suspended bool   `json:"suspended"`
}

// Forwarder sends the mail of an alias to other addresses.
type Forwarder struct {
	Alias        string   `json:"alias"`
	Email        string   `json:"email"`
	Destinations []string `json:"destinations"`
}

// DNSRecord is a record that a domain needs at its DNS host.
type DNSRecord struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// DNSInfo is the records that a domain on MXroute needs.
type DNSInfo struct {
	MXRecords []struct {
		Priority    int    `json:"priority"`
		Hostname    string `json:"hostname"`
		Description string `json:"description,omitempty"`
	} `json:"mx_records"`
	SPF          DNSRecord  `json:"spf"`
	DKIM         *DNSRecord `json:"dkim"`
	Verification *DNSRecord `json:"verification"`
}

// VerificationKey is the record that a domain must have before MXroute takes it.
type VerificationKey struct {
	Key    string    `json:"key"`
	Record DNSRecord `json:"record"`
}

// NewAccount is a mailbox to create.
type NewAccount struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Quota    int    `json:"quota"`
	Limit    int    `json:"limit"`
}

// Error is an error from MXroute.
type Error struct {
	Status        int
	Code, Message string
	Field         string
}

func (e *Error) Error() string {
	f := ""
	if e.Field != "" {
		f = " [field: " + e.Field + "]"
	}
	return fmt.Sprintf("MXroute %s (%d): %s%s", e.Code, e.Status, e.Message, f)
}

// Client is the MXroute REST API. Only the calls that the tools need.
type Client struct {
	Server, Username, APIKey string
	BaseURL                  string // DefaultBaseURL when empty
	HTTP                     *http.Client
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-Server", c.Server)
	req.Header.Set("X-Username", c.Username)
	req.Header.Set("X-API-Key", c.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNoContent {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	var env struct {
		Success *bool           `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   struct {
			Code, Message, Field string
		} `json:"error"`
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &env); err != nil {
			msg := "not JSON"
			if res.StatusCode >= 300 {
				msg += ": " + string(raw[:min(len(raw), 200)])
			}
			return &Error{Status: res.StatusCode, Code: "BAD_RESPONSE", Message: msg}
		}
	}
	if res.StatusCode >= 300 || (env.Success != nil && !*env.Success) {
		e := &Error{Status: res.StatusCode, Code: env.Error.Code, Message: env.Error.Message, Field: env.Error.Field}
		if e.Code == "" {
			e.Code = "HTTP_ERROR"
		}
		if e.Message == "" {
			e.Message = http.StatusText(res.StatusCode)
		}
		if retry := res.Header.Get("Retry-After"); retry != "" {
			e.Message += " (retry after " + retry + "s)"
		}
		return e
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return &Error{Status: res.StatusCode, Code: "BAD_RESPONSE", Message: "unexpected data: " + err.Error()}
		}
	}
	return nil
}

func d(domain string) string { return "/domains/" + url.PathEscape(domain) }

// Domains lists the domains on the account.
func (c *Client) Domains(ctx context.Context) ([]string, error) {
	out := []string{}
	err := c.do(ctx, http.MethodGet, "/domains", nil, &out)
	return out, err
}

// DNSInfo gives the records that a domain needs.
func (c *Client) DNSInfo(ctx context.Context, domain string) (DNSInfo, error) {
	var out DNSInfo
	err := c.do(ctx, http.MethodGet, d(domain)+"/dns", nil, &out)
	return out, err
}

// VerificationKey gives the account's verification record.
func (c *Client) VerificationKey(ctx context.Context) (VerificationKey, error) {
	var out VerificationKey
	err := c.do(ctx, http.MethodGet, "/verification-key", nil, &out)
	return out, err
}

// Accounts lists the mailboxes of a domain.
func (c *Client) Accounts(ctx context.Context, domain string) ([]Account, error) {
	out := []Account{}
	err := c.do(ctx, http.MethodGet, d(domain)+"/email-accounts", nil, &out)
	return out, err
}

// Forwarders lists the forwarders of a domain.
func (c *Client) Forwarders(ctx context.Context, domain string) ([]Forwarder, error) {
	out := []Forwarder{}
	err := c.do(ctx, http.MethodGet, d(domain)+"/forwarders", nil, &out)
	return out, err
}

// AddDomain adds a domain.
func (c *Client) AddDomain(ctx context.Context, domain string) error {
	return c.do(ctx, http.MethodPost, "/domains", map[string]string{"domain": domain}, nil)
}

// CreateAccount creates a mailbox.
func (c *Client) CreateAccount(ctx context.Context, domain string, a NewAccount) error {
	return c.do(ctx, http.MethodPost, d(domain)+"/email-accounts", a, nil)
}

// SetPassword gives a mailbox a new password.
func (c *Client) SetPassword(ctx context.Context, domain, username, password string) error {
	return c.do(ctx, http.MethodPatch, d(domain)+"/email-accounts/"+url.PathEscape(username), map[string]string{"password": password}, nil)
}

// CreateForwarder creates a forwarder.
func (c *Client) CreateForwarder(ctx context.Context, domain, alias string, destinations []string) error {
	return c.do(ctx, http.MethodPost, d(domain)+"/forwarders", map[string]any{"alias": alias, "destinations": destinations}, nil)
}
