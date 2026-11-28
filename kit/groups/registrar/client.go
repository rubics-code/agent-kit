// Package registrar is the Cloudflare Registrar tool group: search, a live
// check, and a registration behind the approval gate.
package registrar

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/rubics-code/agent-kit/kit/groups/cloudflare"
)

// Pricing is the price of a domain.
type Pricing struct {
	Currency         string `json:"currency"`
	RegistrationCost string `json:"registration_cost"`
	RenewalCost      string `json:"renewal_cost"`
}

// Offer is a domain from a search or a check.
type Offer struct {
	Name        string   `json:"name"`
	Registrable bool     `json:"registrable"`
	Tier        string   `json:"tier,omitempty"` // standard or premium
	Pricing     *Pricing `json:"pricing,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}

// Status is the state of a registration: pending, in_progress,
// action_required, blocked, succeeded or failed.
type Status struct {
	State     string `json:"state"`
	Completed bool   `json:"completed"`
	Context   *struct {
		DomainName         string `json:"domain_name,omitempty"`
		Action             string `json:"action,omitempty"`
		ConfirmationSentTo string `json:"confirmation_sent_to,omitempty"`
		Registration       *struct {
			ExpiresAt string `json:"expires_at,omitempty"`
		} `json:"registration,omitempty"`
	} `json:"context,omitempty"`
	Error *struct {
		Code    string `json:"code,omitempty"`
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
}

// Client is the Cloudflare Registrar API. Only the calls that the tools need.
type Client struct {
	API cloudflare.API
	// AccountID, when it is empty, comes from the token. The token must see
	// exactly one account, so a purchase cannot go to the wrong one.
	AccountID string
	// PollDelay is the wait between status calls.
	PollDelay time.Duration
	mu        sync.Mutex
}

func (c *Client) account(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.AccountID != "" {
		return c.AccountID, nil
	}
	var accounts []struct {
		ID string `json:"id"`
	}
	if _, err := c.API.Do(ctx, http.MethodGet, "/accounts", nil, &accounts); err != nil {
		return "", err
	}
	if len(accounts) != 1 {
		return "", &cloudflare.Error{Status: 400, Message: fmt.Sprintf("the token sees %d accounts; it must see exactly one", len(accounts))}
	}
	c.AccountID = accounts[0].ID
	return c.AccountID, nil
}

// Search finds domains that match a word or a name.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Offer, error) {
	a, err := c.account(ctx)
	if err != nil {
		return nil, err
	}
	q := url.Values{"q": {query}, "limit": {strconv.Itoa(limit)}}
	var out struct {
		Domains []Offer `json:"domains"`
	}
	_, err = c.API.Do(ctx, http.MethodGet, "/accounts/"+a+"/registrar/domain-search?"+q.Encode(), nil, &out)
	return out.Domains, err
}

// Check asks the registry now. It is not cached.
func (c *Client) Check(ctx context.Context, domains []string) ([]Offer, error) {
	a, err := c.account(ctx)
	if err != nil {
		return nil, err
	}
	var out struct {
		Domains []Offer `json:"domains"`
	}
	_, err = c.API.Do(ctx, http.MethodPost, "/accounts/"+a+"/registrar/domain-check", map[string]any{"domains": domains}, &out)
	return out.Domains, err
}

// Register registers a domain for one year, with no auto-renew, with WHOIS
// redaction, and with the account's default contact.
func (c *Client) Register(ctx context.Context, domain string) (Status, error) {
	a, err := c.account(ctx)
	if err != nil {
		return Status{}, err
	}
	var s Status
	_, err = c.API.Do(ctx, http.MethodPost, "/accounts/"+a+"/registrar/registrations", map[string]any{
		"domain_name": domain, "years": 1, "auto_renew": false, "privacy_mode": "redaction",
	}, &s)
	return s, err
}

// Status gives the state of a registration.
func (c *Client) Status(ctx context.Context, domain string) (Status, error) {
	a, err := c.account(ctx)
	if err != nil {
		return Status{}, err
	}
	var s Status
	_, err = c.API.Do(ctx, http.MethodGet, "/accounts/"+a+"/registrar/registrations/"+url.PathEscape(domain)+"/registration-status", nil, &s)
	return s, err
}
