// Package openrouter is the OpenRouter keys tool group. It needs a
// management key, which manages the other keys and cannot call a model.
package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// DefaultBaseURL is the OpenRouter API.
const DefaultBaseURL = "https://openrouter.ai/api/v1"

// Key is an OpenRouter API key. The label is masked; the value is never here.
type Key struct {
	Hash           string   `json:"hash"`
	Name           string   `json:"name"`
	Label          string   `json:"label"`
	Disabled       bool     `json:"disabled"`
	Limit          *float64 `json:"limit"` // USD
	LimitRemaining *float64 `json:"limit_remaining"`
	LimitReset     *string  `json:"limit_reset"`
	Usage          float64  `json:"usage"` // USD, all time
	UsageDaily     float64  `json:"usage_daily"`
	UsageWeekly    float64  `json:"usage_weekly"`
	UsageMonthly   float64  `json:"usage_monthly"`
	CreatedAt      string   `json:"created_at"`
	LastUsedAt     *string  `json:"last_used_at"`
	ExpiresAt      *string  `json:"expires_at"`
}

// Credits are the credits bought and used, in USD.
type Credits struct {
	TotalCredits float64 `json:"total_credits"`
	TotalUsage   float64 `json:"total_usage"`
}

// NewKey is a key to create.
type NewKey struct {
	Name       string  `json:"name"`
	Limit      float64 `json:"limit"`
	LimitReset string  `json:"limit_reset"`
	ExpiresAt  string  `json:"expires_at,omitempty"` // ISO 8601 UTC with seconds
}

// KeyChange is a change to a key.
type KeyChange struct {
	Limit      *float64 `json:"limit,omitempty"`
	LimitReset *string  `json:"limit_reset,omitempty"`
	Disabled   *bool    `json:"disabled,omitempty"`
}

// Error is an error from OpenRouter.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("OpenRouter (%d): %s", e.Status, e.Message) }

// Client is the OpenRouter key management API.
type Client struct {
	ManagementKey string
	BaseURL       string // DefaultBaseURL when empty
	HTTP          *http.Client
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
	req.Header.Set("Authorization", "Bearer "+c.ManagementKey)
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
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return err
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		msg := http.StatusText(res.StatusCode)
		if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
			msg = e.Error.Message
		}
		return &Error{Status: res.StatusCode, Message: msg}
	}
	if out == nil {
		return nil
	}
	// The body of a good response can hold a new key, so no error repeats it.
	if err := json.Unmarshal(raw, out); err != nil {
		return &Error{Status: res.StatusCode, Message: "the response is not the expected JSON"}
	}
	return nil
}

// Credits gives the credits bought and used.
func (c *Client) Credits(ctx context.Context) (Credits, error) {
	var out struct{ Data Credits }
	err := c.do(ctx, http.MethodGet, "/credits", nil, &out)
	return out.Data, err
}

// ListKeys reads every page of keys, up to 1000.
func (c *Client) ListKeys(ctx context.Context, includeDisabled bool) ([]Key, error) {
	keys := []Key{}
	for offset := 0; ; {
		q := url.Values{"offset": {strconv.Itoa(offset)}}
		if includeDisabled {
			q.Set("include_disabled", "true")
		}
		var page struct{ Data []Key }
		if err := c.do(ctx, http.MethodGet, "/keys?"+q.Encode(), nil, &page); err != nil {
			return nil, err
		}
		keys = append(keys, page.Data...)
		if len(page.Data) == 0 || len(keys) >= 1000 {
			return keys, nil
		}
		offset += len(page.Data)
	}
}

// Key gives one key.
func (c *Client) Key(ctx context.Context, hash string) (Key, error) {
	var out struct{ Data Key }
	err := c.do(ctx, http.MethodGet, "/keys/"+url.PathEscape(hash), nil, &out)
	return out.Data, err
}

// CreateKey creates a key, and gives its value one time.
func (c *Client) CreateKey(ctx context.Context, in NewKey) (Key, string, error) {
	var out struct {
		Data Key    `json:"data"`
		Key  string `json:"key"`
	}
	err := c.do(ctx, http.MethodPost, "/keys", in, &out)
	return out.Data, out.Key, err
}

// UpdateKey changes a key.
func (c *Client) UpdateKey(ctx context.Context, hash string, ch KeyChange) (Key, error) {
	var out struct{ Data Key }
	err := c.do(ctx, http.MethodPatch, "/keys/"+url.PathEscape(hash), ch, &out)
	return out.Data, err
}
