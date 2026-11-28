package infisical

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/rubics-code/agent-kit/kit/secrets"
)

type call struct {
	Method, Path, Auth string
	Query              url.Values
	Body               map[string]any
}

func server(t *testing.T, route func(c call) (int, any)) (*httptest.Server, *[]call) {
	var mu sync.Mutex
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := call{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Query: r.URL.Query()}
		_ = json.NewDecoder(r.Body).Decode(&c.Body)
		mu.Lock()
		calls = append(calls, c)
		mu.Unlock()
		status, body := route(c)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func login(c call) bool { return strings.HasSuffix(c.Path, "/login") }

var tok = map[string]any{"accessToken": "tok", "expiresIn": 3600}

func identity(srv *httptest.Server) *Client {
	return New(Auth{ClientID: "id", ClientSecret: "s"}, Config{BaseURL: srv.URL, ProjectID: "p1"})
}

func writes(calls []call) []call {
	var out []call
	for _, c := range calls {
		if c.Method != http.MethodGet && !login(c) {
			out = append(out, c)
		}
	}
	return out
}

func TestLogsInOnceAndLogsInAgainAfterA401(t *testing.T) {
	first := true
	srv, calls := server(t, func(c call) (int, any) {
		if login(c) {
			return 200, tok
		}
		if first {
			first = false
			return 401, map[string]any{"message": "expired"}
		}
		return 200, map[string]any{"folders": []any{map[string]any{"name": "tally"}, map[string]any{"name": "lyra"}}}
	})
	got, err := identity(srv).ListFolders(t.Context(), "/agents")
	if err != nil || strings.Join(got, ",") != "lyra,tally" {
		t.Fatalf("%v %v", got, err)
	}
	n := 0
	for _, c := range *calls {
		if login(c) {
			n++
		}
	}
	last := (*calls)[len(*calls)-1]
	if n != 2 || last.Auth != "Bearer tok" || last.Query.Get("workspaceId") != "p1" {
		t.Fatalf("logins %d, last %+v", n, last)
	}
}

func TestAsksForNamesWithoutValues(t *testing.T) {
	srv, calls := server(t, func(c call) (int, any) {
		if login(c) {
			return 200, tok
		}
		return 200, map[string]any{"secrets": []any{map[string]any{"secretKey": "B"}, map[string]any{"secretKey": "A"}}}
	})
	names, err := identity(srv).ListSecretNames(t.Context(), "/agents/example")
	if err != nil || strings.Join(names, ",") != "A,B" || (*calls)[len(*calls)-1].Query.Get("viewSecretValue") != "false" {
		t.Fatalf("%v %v", names, err)
	}
}

func TestMakesTheMissingFoldersThenCreatesTheSecret(t *testing.T) {
	srv, calls := server(t, func(c call) (int, any) {
		switch {
		case login(c):
			return 200, tok
		case c.Path == "/api/v1/folders" && c.Method == "GET":
			if c.Query.Get("path") == "/" {
				return 200, map[string]any{"folders": []any{map[string]any{"name": "agents"}}}
			}
			return 200, map[string]any{"folders": []any{}}
		case c.Path == "/api/v3/secrets/raw" && c.Method == "GET":
			return 200, map[string]any{"secrets": []any{}}
		}
		return 200, map[string]any{}
	})
	if err := identity(srv).Put(t.Context(), secrets.Place{Path: "/agents/nova", Name: "API_SERVER_KEY"}, "v"); err != nil {
		t.Fatal(err)
	}
	w := writes(*calls)
	if len(w) != 2 || w[0].Method+" "+w[0].Path != "POST /api/v1/folders" || w[1].Method+" "+w[1].Path != "POST /api/v3/secrets/raw/API_SERVER_KEY" {
		t.Fatalf("%+v", w)
	}
	if w[0].Body["name"] != "nova" || w[0].Body["path"] != "/agents" || w[0].Body["environment"] != "prod" {
		t.Fatalf("%+v", w[0].Body)
	}
	if w[1].Body["secretPath"] != "/agents/nova" || w[1].Body["secretValue"] != "v" || w[1].Body["type"] != "shared" {
		t.Fatalf("%+v", w[1].Body)
	}
}

func TestReplacesASecretThatExistsWithPATCH(t *testing.T) {
	srv, calls := server(t, func(c call) (int, any) {
		switch {
		case login(c):
			return 200, tok
		case c.Path == "/api/v1/folders":
			return 200, map[string]any{"folders": []any{map[string]any{"name": "ops"}}}
		case c.Path == "/api/v3/secrets/raw" && c.Method == "GET":
			return 200, map[string]any{"secrets": []any{map[string]any{"secretKey": "K"}}}
		}
		return 200, map[string]any{}
	})
	if err := identity(srv).Put(t.Context(), secrets.Place{Path: "/ops", Name: "K"}, "v"); err != nil {
		t.Fatal(err)
	}
	if last := (*calls)[len(*calls)-1]; last.Method != "PATCH" {
		t.Fatalf("%+v", last)
	}
}

func TestUsesTheEnvironmentOfEachFolderAlsoToCheckTheFolderInRoot(t *testing.T) {
	srv, calls := server(t, func(c call) (int, any) {
		if c.Path == "/api/v1/folders" {
			return 200, map[string]any{"folders": []any{map[string]any{"name": "agents"}, map[string]any{"name": "ops"}}}
		}
		return 200, map[string]any{"secrets": []any{}}
	})
	env := func(p string) string {
		if p == "/agents" || strings.HasPrefix(p, "/agents/") {
			return "staging"
		}
		return "prod"
	}
	inf := New(Auth{Token: "t"}, Config{BaseURL: srv.URL, ProjectID: "p", Env: env})
	if err := inf.Put(t.Context(), secrets.Place{Path: "/agents/nova", Name: "K"}, "v"); err != nil {
		t.Fatal(err)
	}
	if _, err := inf.ListSecretNames(t.Context(), "/ops"); err != nil {
		t.Fatal(err)
	}
	cs := *calls
	envOf := func(c call) string {
		if e := c.Query.Get("environment"); e != "" {
			return e
		}
		e, _ := c.Body["environment"].(string)
		return e
	}
	for _, c := range cs[:len(cs)-1] {
		if envOf(c) != "staging" {
			t.Fatalf("%s %s in %s", c.Method, c.Path, envOf(c))
		}
	}
	if envOf(cs[len(cs)-1]) != "prod" {
		t.Fatal("the last call is not in prod")
	}
}

func TestSendsAStaticTokenAsItIsAndNeverLogsIn(t *testing.T) {
	srv, calls := server(t, func(call) (int, any) {
		return 200, map[string]any{"folders": []any{map[string]any{"name": "lyra"}}}
	})
	inf := New(Auth{Token: "__EXAMPLE_INFISICAL_TOKEN__"}, Config{BaseURL: srv.URL, ProjectID: "p"})
	if got, err := inf.ListFolders(t.Context(), "/agents"); err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
	if len(*calls) != 1 || (*calls)[0].Path != "/api/v1/folders" || (*calls)[0].Auth != "Bearer __EXAMPLE_INFISICAL_TOKEN__" {
		t.Fatalf("%+v", *calls)
	}
}

func TestGivesAnInfisicalErrorWithTheMessage(t *testing.T) {
	srv, _ := server(t, func(c call) (int, any) {
		if login(c) {
			return 200, tok
		}
		return 403, map[string]any{"message": "no access"}
	})
	_, err := identity(srv).ListFolders(t.Context(), "/x")
	if e, ok := err.(*Error); !ok || e.Status != 403 || e.Error() != "Infisical 403: no access" {
		t.Fatalf("%v", err)
	}
}
