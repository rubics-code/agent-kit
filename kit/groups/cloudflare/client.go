// Package cloudflare is the Cloudflare DNS tool group, and the Cloudflare API
// v4 client that the DNS and Registrar groups share.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// DefaultBaseURL is the Cloudflare API v4.
const DefaultBaseURL = "https://api.cloudflare.com/client/v4"

// Error is an error from the Cloudflare API.
type Error struct {
	Status  int
	Codes   []int
	Message string
}

func (e *Error) Error() string {
	codes := ""
	if len(e.Codes) > 0 {
		s := make([]string, len(e.Codes))
		for i, c := range e.Codes {
			s[i] = strconv.Itoa(c)
		}
		codes = " [" + strings.Join(s, ", ") + "]"
	}
	return fmt.Sprintf("Cloudflare %d%s: %s", e.Status, codes, e.Message)
}

// API calls the Cloudflare API with a token.
type API struct {
	Token   string
	BaseURL string // DefaultBaseURL when empty
	HTTP    *http.Client
}

// ResultInfo is the page data of a list.
type ResultInfo struct {
	Page       int `json:"page"`
	TotalPages int `json:"total_pages"`
}

type envelope struct {
	Success *bool           `json:"success"`
	Result  json.RawMessage `json:"result"`
	Errors  []struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	ResultInfo *ResultInfo `json:"result_info"`
}

// Do sends a request and decodes the result into out. It is an error when the
// response has no result and out is not nil.
func (a *API) Do(ctx context.Context, method, path string, body, out any) (*ResultInfo, error) {
	base := a.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := a.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	res, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var env envelope
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, &Error{Status: res.StatusCode, Message: "not JSON: " + clip(string(raw), 200)}
		}
	}
	if res.StatusCode >= 300 || (env.Success != nil && !*env.Success) {
		var msgs []string
		var codes []int
		for _, e := range env.Errors {
			if e.Message != "" {
				msgs = append(msgs, e.Message)
			}
			if e.Code != nil {
				codes = append(codes, *e.Code)
			}
		}
		msg := strings.Join(msgs, "; ")
		if msg == "" {
			msg = http.StatusText(res.StatusCode)
		}
		return nil, &Error{Status: res.StatusCode, Codes: codes, Message: msg}
	}
	if out != nil {
		if len(env.Result) == 0 || string(env.Result) == "null" {
			return nil, &Error{Status: res.StatusCode, Message: "no result in the response"}
		}
		if err := json.Unmarshal(env.Result, out); err != nil {
			return nil, &Error{Status: res.StatusCode, Message: "an unexpected result: " + err.Error()}
		}
	}
	return env.ResultInfo, nil
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// Zone is a DNS zone.
type Zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Record is a DNS record. The name is fully qualified, with no final dot.
type Record struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	Priority *int   `json:"priority,omitempty"`
	TTL      int    `json:"ttl"` // 1 is automatic
	Proxied  bool   `json:"proxied,omitempty"`
	Comment  string `json:"comment,omitempty"`
}

// NewRecord is a record to create.
type NewRecord struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	Priority *int   `json:"priority,omitempty"`
	Comment  string `json:"comment"`
	TTL      int    `json:"ttl"`
}

// DNS is the Cloudflare DNS API. Only the calls that the tools need.
type DNS struct{ API API }

// FindZone gives the zone that holds the domain: the domain itself or a
// parent of it. It is nil when the token sees no such zone.
func (d *DNS) FindZone(ctx context.Context, domain string) (*Zone, error) {
	labels := strings.Split(strings.ToLower(domain), ".")
	for i := 0; i < len(labels)-1; i++ {
		name := strings.Join(labels[i:], ".")
		var zones []Zone
		if _, err := d.API.Do(ctx, http.MethodGet, "/zones?name="+url.QueryEscape(name), nil, &zones); err != nil {
			return nil, err
		}
		for _, z := range zones {
			if z.Name == name {
				return &Zone{ID: z.ID, Name: z.Name}, nil
			}
		}
	}
	return nil, nil
}

// ListRecords reads every page of the records of a zone.
func (d *DNS) ListRecords(ctx context.Context, zoneID string) ([]Record, error) {
	var all []Record
	for page := 1; ; page++ {
		var recs []Record
		info, err := d.API.Do(ctx, http.MethodGet, fmt.Sprintf("/zones/%s/dns_records?per_page=100&page=%d", url.PathEscape(zoneID), page), nil, &recs)
		if err != nil {
			return nil, err
		}
		all = append(all, recs...)
		total := 1
		if info != nil && info.TotalPages > 0 {
			total = info.TotalPages
		}
		if page >= total {
			return all, nil
		}
	}
}

// CreateRecord creates a record with an automatic TTL.
func (d *DNS) CreateRecord(ctx context.Context, zoneID string, r NewRecord) (Record, error) {
	r.TTL = 1
	var out Record
	_, err := d.API.Do(ctx, http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/dns_records", r, &out)
	return out, err
}
