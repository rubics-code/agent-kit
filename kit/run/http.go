package run

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"google.golang.org/adk/v2/server/authn"

	"github.com/rubics-code/agent-kit/kit/credentials"
	"github.com/rubics-code/agent-kit/kit/group"
)

// protect puts the Kit's checks in front of every endpoint:
//
//   - A browser request from another origin is refused, unless the origin is
//     in Config.Origins (then it gets CORS headers).
//   - On a loopback address, a request for another host name is refused, so a
//     page cannot reach the agent by DNS rebinding.
//   - A form post is refused, because a browser sends one across sites with
//     no preflight.
//   - On an address that is not loopback, every request needs auth: a bearer
//     token from <PREFIX>_TOKEN, or a header that a trusted proxy sets, named
//     in <PREFIX>_TRUSTED_HEADER. With neither, the agent does not start.
func protect(next http.Handler, cfg Config, e credentials.Environ) (http.Handler, error) {
	loop := isLoopback(cfg.Addr)
	allowed := map[string]bool{}
	allowedHosts := map[string]bool{}
	for _, o := range cfg.Origins {
		u, err := url.Parse(o)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("the origin %q is not scheme://host[:port]", o)
		}
		allowed[u.Scheme+"://"+u.Host] = true
		allowedHosts[u.Hostname()] = true
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" {
			if allowed[o] {
				hd := w.Header()
				hd.Set("Access-Control-Allow-Origin", o)
				hd.Add("Vary", "Origin")
				hd.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				hd.Set("Access-Control-Allow-Headers", "Content-Type, Accept, Authorization, A2A-Version, X-A2A-Extensions")
				if r.Method == http.MethodOptions {
					w.WriteHeader(http.StatusNoContent)
					return
				}
			} else if !sameOrigin(o, r.Host) {
				http.Error(w, "this origin may not call the agent", http.StatusForbidden)
				return
			}
		}
		if loop && !loopbackHost(r.Host) && !allowedHosts[hostOnly(r.Host)] {
			http.Error(w, "this host name may not call the agent", http.StatusForbidden)
			return
		}
		if isForm(r) {
			http.Error(w, "send JSON, not a form", http.StatusUnsupportedMediaType)
			return
		}
		next.ServeHTTP(w, r)
	})
	if loop {
		return h, nil
	}
	a, err := authenticator(cfg, e)
	if err != nil {
		return nil, err
	}
	return authn.Middleware(a)(h), nil
}

func authenticator(cfg Config, e credentials.Environ) (authn.Authenticator, error) {
	if hdr := cfg.env(e, "TRUSTED_HEADER"); hdr != "" {
		return authn.NewHeader(hdr), nil
	}
	token := cfg.env(e, "TOKEN")
	if len(token) < 32 {
		return nil, fmt.Errorf("the agent listens on %s, which is not loopback, so it needs auth: set %s_TOKEN (32 characters or more) or %s_TRUSTED_HEADER",
			cfg.Addr, cfg.EnvPrefix, cfg.EnvPrefix)
	}
	return authn.NewCustom(func(r *http.Request) (*authn.Caller, error) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			return nil, fmt.Errorf("%w: a bearer token is needed", authn.ErrUnauthenticated)
		}
		return &authn.Caller{UserID: "operator"}, nil
	}), nil
}

func sameOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Host != "" && strings.EqualFold(u.Host, host)
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

func loopbackHost(hostport string) bool {
	h := hostOnly(hostport)
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}

func isForm(r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return false
	}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return slices.Contains([]string{"application/x-www-form-urlencoded", "multipart/form-data", "text/plain"}, ct)
}

// info answers GET /kit/info: the agent's name, model, key mode, groups and
// tools. It holds no key.
func (a *Agent) info(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	groups := a.Statuses
	if groups == nil {
		groups = []group.Status{}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"name": a.Name, "model": a.Model, "keys": a.Mode, "headless": a.Headless, "groups": groups, "tools": a.Tools,
	})
}
