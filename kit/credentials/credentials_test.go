package credentials

import (
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var vault = Environ{"INFISICAL_AGENT_VAULT_SESSION_TOKEN": "session", "HTTPS_PROXY": "http://localhost:17323"}

func with(e Environ, kv ...string) Environ {
	out := Environ{}
	for k, v := range e {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] == "" {
			delete(out, kv[i])
		} else {
			out[kv[i]] = kv[i+1]
		}
	}
	return out
}

// loadCredentials

func TestReadsTheRealValuesFromTheEnvironmentInEnvMode(t *testing.T) {
	c, err := Load(Environ{"MAIL_API_KEY": "real"})
	if err != nil || c.Mode != Env {
		t.Fatal(c.Mode, err)
	}
	if v, _ := c.Get("MAIL_API_KEY"); v != "real" {
		t.Fatal(v)
	}
	if c.Optional("OTHER_KEY") != "" {
		t.Fatal("an optional key that is not there has a value")
	}
	if _, err := c.Get("MODEL_KEY"); err == nil || err.Error() != "MODEL_KEY is not set" {
		t.Fatal(err)
	}
}

func TestGivesPlaceholdersNotKeysInAgentVaultMode(t *testing.T) {
	c, err := Load(vault)
	if err != nil || c.Mode != AgentVault {
		t.Fatal(c.Mode, err)
	}
	if v, _ := c.Get("MAIL_API_KEY"); v != "__MAIL_API_KEY__" {
		t.Fatal(v)
	}
	if c.Optional("OTHER_KEY") != "__OTHER_KEY__" || !c.Holds("OTHER_KEY") {
		t.Fatal("no placeholder for an optional key")
	}
}

func TestStopsWhenTheClientWouldNotSendTheRequestsThroughTheProxy(t *testing.T) {
	if _, err := Load(with(vault, "NO_PROXY", "*")); err == nil || !strings.Contains(err.Error(), "NO_PROXY=*") {
		t.Fatal(err)
	}
	if _, err := Load(with(vault, "HTTPS_PROXY", "")); err == nil || !strings.Contains(err.Error(), "HTTPS_PROXY is not set") {
		t.Fatal(err)
	}
}

func TestStopsWhenARealKeyIsAlsoInTheEnvironmentInAgentVaultMode(t *testing.T) {
	c, _ := Load(with(vault, "MAIL_API_KEY", "real"))
	if _, err := c.Get("MAIL_API_KEY"); err == nil || !strings.Contains(err.Error(), "MAIL_API_KEY is in the environment") {
		t.Fatal(err)
	}
}

func TestSeesAgentVaultModeFromTheProxyUserAlone(t *testing.T) {
	c, err := Load(Environ{"HTTPS_PROXY": "http://x-agent-vault:tok@100.64.0.1:17323"})
	if err != nil || c.Mode != AgentVault {
		t.Fatal(c.Mode, err)
	}
	if v, _ := c.Get("MAIL_API_KEY"); v != "__MAIL_API_KEY__" {
		t.Fatal(v)
	}
}

func TestStaysInEnvModeBehindAnotherProxy(t *testing.T) {
	c, _ := Load(Environ{"HTTPS_PROXY": "http://corp:pw@proxy:8080", "MAIL_API_KEY": "real"})
	if c.Mode != Env {
		t.Fatal(c.Mode)
	}
}

// sessionHosts

func services(t *testing.T, status string, hosts ...[]string) (*httptest.Server, *[]*http.Request) {
	var reqs []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, r)
		var svcs []map[string]any
		for _, h := range hosts {
			svcs = append(svcs, map[string]any{"hosts": h, "status": status})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"services": svcs})
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

func TestAsksTheInfisicalAPIFromINFISICAL_AGENT_VAULT_URLWithOrWithoutAPI(t *testing.T) {
	srv, reqs := services(t, "ready", []string{"openrouter.ai"}, []string{"API.MAIL.EXAMPLE.TEST:443"})
	hosts := SessionHosts(t.Context(), "", Environ{"INFISICAL_AGENT_VAULT_URL": srv.URL + "/api/"}, nil)
	if len(*reqs) != 1 || (*reqs)[0].URL.Path != "/api/v1/agent-vault/session/services" {
		t.Fatalf("%+v", *reqs)
	}
	if !hosts["openrouter.ai"] || !hosts["api.mail.example.test"] || len(hosts) != 2 {
		t.Fatalf("%v", hosts)
	}
}

func TestSendsTheSessionTokenFromHTTPS_PROXYAsABearerToken(t *testing.T) {
	srv, reqs := services(t, "ready", []string{"openrouter.ai"})
	SessionHosts(t.Context(), srv.URL, Environ{"HTTPS_PROXY": "http://x-agent-vault:agv_abc%2F1@100.64.0.1:17323"}, nil)
	if got := (*reqs)[0].Header.Get("Authorization"); got != "Bearer agv_abc/1" {
		t.Fatal(got)
	}
}

func TestLeavesOutAServiceThatIsNotReady(t *testing.T) {
	srv, _ := services(t, "pending", []string{"api.dns.example.test"})
	if hosts := SessionHosts(t.Context(), srv.URL, Environ{}, nil); hosts == nil || len(hosts) != 0 {
		t.Fatalf("%v", hosts)
	}
}

func TestIsNilWhenInfisicalDoesNotAnswer(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	if SessionHosts(t.Context(), srv.URL, Environ{}, nil) != nil {
		t.Fatal("a 404 gave hosts")
	}
	if SessionHosts(t.Context(), "http://127.0.0.1:1", Environ{}, nil) != nil {
		t.Fatal("no server gave hosts")
	}
}

// loadIdentityCredentials

func TestReadsTheAgentsKeysFromItsFolder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/login") {
			_ = json.NewEncoder(w).Encode(map[string]any{"accessToken": "tok", "expiresIn": 3600})
			return
		}
		if r.URL.Query().Get("secretPath") != "/agents/example" {
			t.Errorf("path %s", r.URL.Query().Get("secretPath"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"secrets": []any{map[string]any{"secretKey": "MODEL_KEY", "secretValue": "sk-x"}}})
	}))
	defer srv.Close()
	c, inf, err := LoadIdentity(t.Context(), IdentityFile{"id", "s"}, InfisicalConfig{URL: srv.URL, ProjectID: "p", Path: "/agents/example"}, nil)
	if err != nil || c.Mode != Identity || inf == nil {
		t.Fatal(err)
	}
	if v, _ := c.Get("MODEL_KEY"); v != "sk-x" || c.Optional("MAIL_API_KEY") != "" {
		t.Fatal(v)
	}
	if _, err := c.Get("MAIL_API_KEY"); err == nil || !strings.Contains(err.Error(), "not in Infisical /agents/example") {
		t.Fatal(err)
	}
}

// readIdentityFile

func TestTakesTheValuesFromTheFileNotFromTheShell(t *testing.T) {
	file := filepath.Join(t.TempDir(), ".env")
	_ = os.WriteFile(file, []byte("# identity\nINFISICAL_CLIENT_ID=from-file\nexport INFISICAL_CLIENT_SECRET=\"s\"\n"), 0o600)
	t.Setenv("INFISICAL_CLIENT_ID", "from-shell")
	id, ok, err := ReadIdentityFile(file)
	if err != nil || !ok || id != (IdentityFile{"from-file", "s"}) {
		t.Fatalf("%+v %v %v", id, ok, err)
	}
}

func TestIsNothingWhenThereIsNoFile(t *testing.T) {
	if _, ok, err := ReadIdentityFile("/nonexistent/agent/.env"); ok || err != nil {
		t.Fatal(ok, err)
	}
	half := filepath.Join(t.TempDir(), ".env")
	_ = os.WriteFile(half, []byte("INFISICAL_CLIENT_ID=x\n"), 0o600)
	if _, _, err := ReadIdentityFile(half); err == nil {
		t.Fatal("a file with no secret passed")
	}
}

// HTTP client

func TestTheHTTPClientKeepsTheProxyFromTheEnvironment(t *testing.T) {
	c, err := HTTPClient(Environ{})
	if err != nil {
		t.Fatal(err)
	}
	p := c.Transport.(*http.Transport).Proxy
	if reflect.ValueOf(p).Pointer() != reflect.ValueOf(http.ProxyFromEnvironment).Pointer() {
		t.Fatal("the client does not use ProxyFromEnvironment")
	}
}

func TestTheHTTPClientTrustsTheVaultCA(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer srv.Close()
	plain, _ := HTTPClient(Environ{})
	if _, err := plain.Get(srv.URL); err == nil {
		t.Fatal("the test CA is trusted with no CA file")
	}
	file := filepath.Join(t.TempDir(), "vault-ca.pem")
	_ = os.WriteFile(file, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600)
	c, err := HTTPClient(Environ{"KIT_EXTRA_CA_CERTS": file})
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Get(srv.URL)
	if err != nil || res.StatusCode != 200 {
		t.Fatal(err)
	}
	if _, err := HTTPClient(Environ{"SSL_CERT_FILE": "/nonexistent.pem"}); err == nil {
		t.Fatal("a missing CA file passed")
	}
}
