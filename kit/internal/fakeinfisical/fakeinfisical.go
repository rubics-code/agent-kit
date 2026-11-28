// Package fakeinfisical is the part of the Infisical API that the Kit's
// client uses, in memory, for tests. It never shows a value when a request
// asks for names only.
package fakeinfisical

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rubics-code/agent-kit/kit/infisical"
)

// Token is the static token that the fake accepts.
const Token = "test-infisical-token"

// Write is one write that the fake got.
type Write struct {
	Method, Path, Env string
}

// Server is a fake Infisical project.
type Server struct {
	*httptest.Server
	mu      sync.Mutex
	folders map[string]map[string]string // path to secrets
	writes  []Write
	paths   []string // every path that a request named
	// FailPut, when it is not empty, is the error message of each secret write.
	FailPut string
}

// New starts a fake with these folders. The root folder always exists.
func New(t *testing.T, seed map[string]map[string]string) *Server {
	s := &Server{folders: map[string]map[string]string{"/": {}}}
	for p, secrets := range seed {
		for at := p; at != "/" && at != ""; at = parent(at) {
			if s.folders[at] == nil {
				s.folders[at] = map[string]string{}
			}
		}
		for k, v := range secrets {
			s.folders[p][k] = v
		}
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func parent(p string) string {
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return "/"
	}
	return p[:i]
}

// Client gives the Kit's Infisical client for this fake.
func (s *Server) Client(env func(path string) string) *infisical.Client {
	return infisical.New(infisical.Auth{Token: Token}, infisical.Config{BaseURL: s.URL, ProjectID: "test-project", Env: env, Client: s.Server.Client()})
}

// Value gives a secret's value, and whether it exists.
func (s *Server) Value(path, name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.folders[path][name]
	return v, ok
}

// Writes gives the writes so far.
func (s *Server) Writes() []Write {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.writes)
}

// Asked reports whether any request named this path.
func (s *Server) Asked(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Contains(s.paths, path)
}

func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+Token {
		send(w, 401, map[string]any{"message": "Token missing"})
		return
	}
	q := r.URL.Query()
	var body map[string]string
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	switch {
	case r.URL.Path == "/api/v1/folders" && r.Method == http.MethodGet:
		p := q.Get("path")
		s.paths = append(s.paths, p)
		if s.folders[p] == nil {
			send(w, 404, map[string]any{"message": "Folder not found"})
			return
		}
		out := []map[string]string{}
		for f := range s.folders {
			if f != "/" && parent(f) == p {
				out = append(out, map[string]string{"name": f[strings.LastIndex(f, "/")+1:]})
			}
		}
		send(w, 200, map[string]any{"folders": out})
	case r.URL.Path == "/api/v1/folders" && r.Method == http.MethodPost:
		p := strings.TrimSuffix(body["path"], "/") + "/" + body["name"]
		s.writes = append(s.writes, Write{Method: "POST", Path: p, Env: body["environment"]})
		s.folders[p] = map[string]string{}
		send(w, 200, map[string]any{"folder": map[string]string{"name": body["name"]}})
	case r.URL.Path == "/api/v3/secrets/raw" && r.Method == http.MethodGet:
		p := q.Get("secretPath")
		s.paths = append(s.paths, p)
		out := []map[string]string{}
		for k, v := range s.folders[p] {
			item := map[string]string{"secretKey": k}
			if q.Get("viewSecretValue") != "false" {
				item["secretValue"] = v
			}
			out = append(out, item)
		}
		send(w, 200, map[string]any{"secrets": out})
	case strings.HasPrefix(r.URL.Path, "/api/v3/secrets/raw/"):
		name := strings.TrimPrefix(r.URL.Path, "/api/v3/secrets/raw/")
		p := body["secretPath"]
		s.paths = append(s.paths, p)
		if s.FailPut != "" {
			send(w, 403, map[string]any{"message": s.FailPut})
			return
		}
		s.writes = append(s.writes, Write{Method: r.Method, Path: p + "/" + name, Env: body["environment"]})
		if s.folders[p] == nil {
			send(w, 404, map[string]any{"message": "Folder not found"})
			return
		}
		s.folders[p][name] = body["secretValue"]
		send(w, 200, map[string]any{"secret": map[string]string{"secretKey": name}})
	default:
		send(w, 404, map[string]any{"message": "Not found"})
	}
}
