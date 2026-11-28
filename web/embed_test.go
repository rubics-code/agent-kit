package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(h http.Handler, p string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
	return w
}

func TestHandler(t *testing.T) {
	h := Handler(fstest.MapFS{
		"index.html":    {Data: []byte("<div id=root></div>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	})
	if w := get(h, "/"); w.Code != 200 || !strings.Contains(w.Body.String(), "root") || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("index: %d %v", w.Code, w.Header())
	}
	if w := get(h, "/assets/app.js"); w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d %v", w.Code, w.Header())
	}
	if w := get(h, "/dashboard"); w.Code != 200 || !strings.Contains(w.Body.String(), "root") {
		t.Fatalf("page path: %d", w.Code)
	}
	if w := get(h, "/assets/missing.js"); w.Code != 404 {
		t.Fatalf("missing asset: %d", w.Code)
	}
	if w := get(h, "/../../etc/passwd"); w.Code != 200 || !strings.Contains(w.Body.String(), "root") {
		t.Fatalf("path escape: %d %s", w.Code, w.Body)
	}
}

func TestNotBuilt(t *testing.T) {
	if w := get(Handler(fstest.MapFS{".keep": {}}), "/"); !strings.Contains(w.Body.String(), "not built") {
		t.Fatalf("%s", w.Body)
	}
}
