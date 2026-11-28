package fakes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// CloudflareToken is the token that the Cloudflare fake accepts.
const CloudflareToken = "test-cloudflare-token"

// Record is a DNS record in the Cloudflare fake.
type Record struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	Priority *int   `json:"priority,omitempty"`
	TTL      int    `json:"ttl"`
	Comment  string `json:"comment,omitempty"`
}

// Pricing is the price of a domain.
type Pricing struct {
	Currency         string `json:"currency"`
	RegistrationCost string `json:"registration_cost"`
	RenewalCost      string `json:"renewal_cost"`
}

// Offer is a domain that the Registrar fake knows.
type Offer struct {
	Name        string   `json:"name"`
	Registrable bool     `json:"registrable"`
	Tier        string   `json:"tier,omitempty"`
	Pricing     *Pricing `json:"pricing,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}

// Price makes an offer that can be registered at a price in USD.
func Price(name, cost string) Offer {
	o := Offer{Name: name, Registrable: true, Tier: "standard"}
	o.Pricing = &Pricing{"USD", cost, cost}
	return o
}

// Cloudflare is the Cloudflare API v4 for DNS and Registrar, for one account
// ("acc1").
type Cloudflare struct {
	*httptest.Server
	log
	zones  map[string][]Record
	next   int
	offers map[string]Offer
	// States are the states that a registration and then each status call
	// give, in order. The last one stays.
	states []map[string]any
}

// NewCloudflare starts a fake with these zones, by zone name.
func NewCloudflare(t testing.TB, zones map[string][]Record) *Cloudflare {
	f := &Cloudflare{zones: map[string][]Record{}, offers: map[string]Offer{},
		states: []map[string]any{{"state": "succeeded", "completed": true}}}
	for zone, recs := range zones {
		f.zones[zone] = []Record{}
		for _, r := range recs {
			f.next++
			r.ID = fmt.Sprintf("rec-%d", f.next)
			if r.TTL == 0 {
				r.TTL = 1
			}
			f.zones[zone] = append(f.zones[zone], r)
		}
	}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

// SetOffer adds or replaces an offer.
func (f *Cloudflare) SetOffer(o Offer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offers[o.Name] = o
}

// SetStates sets the registration states, as JSON objects.
func (f *Cloudflare) SetStates(states ...map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states = states
}

// Records gives the records of a zone.
func (f *Cloudflare) Records(zone string) []Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Record{}, f.zones[zone]...)
}

func cfFail(w http.ResponseWriter, status, code int, msg string) {
	send(w, status, map[string]any{"success": false, "errors": []any{map[string]any{"code": code, "message": msg}}})
}

func cfOK(w http.ResponseWriter, status int, result any, extra ...any) {
	body := map[string]any{"success": true, "result": result}
	for i := 0; i+1 < len(extra); i += 2 {
		body[extra[i].(string)] = extra[i+1]
	}
	send(w, status, body)
}

func (f *Cloudflare) nextState() map[string]any {
	s := f.states[0]
	if len(f.states) > 1 {
		f.states = f.states[1:]
	}
	return s
}

func (f *Cloudflare) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+CloudflareToken {
		cfFail(w, 403, 10000, "Authentication error")
		return
	}
	body := decode(r)
	p := r.URL.Path
	parts := strings.Split(strings.Trim(p, "/"), "/")
	const reg = "/accounts/acc1/registrar/"
	switch {
	case p == "/zones" && r.Method == http.MethodGet:
		name := r.URL.Query().Get("name")
		out := []any{}
		if _, ok := f.zones[name]; ok {
			out = append(out, map[string]any{"id": "zone-" + name, "name": name})
		}
		cfOK(w, 200, out)
	case len(parts) == 3 && parts[0] == "zones" && parts[2] == "dns_records":
		f.dns(w, r, strings.TrimPrefix(parts[1], "zone-"), body)
	case p == "/accounts":
		cfOK(w, 200, []any{map[string]any{"id": "acc1"}})
	case p == reg+"domain-search":
		q := strings.ToLower(r.URL.Query().Get("q"))
		out := []Offer{}
		for _, o := range f.offers {
			if strings.Contains(o.Name, q) {
				out = append(out, o)
			}
		}
		cfOK(w, 200, map[string]any{"domains": out})
	case p == reg+"domain-check":
		out := []Offer{}
		ds, _ := body["domains"].([]any)
		for _, d := range ds {
			name, _ := d.(string)
			o, ok := f.offers[name]
			if !ok {
				o = Offer{Name: name, Reason: "domain_unavailable"}
			}
			out = append(out, o)
		}
		cfOK(w, 200, map[string]any{"domains": out})
	case p == reg+"registrations" && r.Method == http.MethodPost:
		f.add("register", body)
		cfOK(w, http.StatusAccepted, f.nextState())
	case strings.HasPrefix(p, reg+"registrations/") && strings.HasSuffix(p, "/registration-status"):
		cfOK(w, 200, f.nextState())
	default:
		cfFail(w, 404, 7000, "No route for that URI")
	}
}

func (f *Cloudflare) dns(w http.ResponseWriter, r *http.Request, zone string, body map[string]any) {
	recs, ok := f.zones[zone]
	if !ok {
		cfFail(w, 404, 7003, "Could not route to the zone")
		return
	}
	if r.Method == http.MethodGet {
		cfOK(w, 200, recs, "result_info", map[string]any{"page": 1, "total_pages": 1})
		return
	}
	body["zoneId"] = "zone-" + zone
	f.add("createRecord", body)
	nr := Record{TTL: 1}
	nr.Type, _ = body["type"].(string)
	nr.Name, _ = body["name"].(string)
	nr.Content, _ = body["content"].(string)
	nr.Comment, _ = body["comment"].(string)
	if p, ok := body["priority"].(float64); ok {
		n := int(p)
		nr.Priority = &n
	}
	for _, x := range recs {
		if x.Type == nr.Type && x.Name == nr.Name && x.Content == nr.Content {
			cfFail(w, 400, 81058, "An identical record already exists.")
			return
		}
	}
	f.next++
	nr.ID = fmt.Sprintf("rec-%d", f.next)
	f.zones[zone] = append(recs, nr)
	cfOK(w, 200, nr)
}
