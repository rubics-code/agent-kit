package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	h := cors([]string{"http://localhost:5173"}, ok)

	r := httptest.NewRequest(http.MethodOptions, "/a2a", nil)
	r.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("preflight: %d %v", w.Code, w.Header())
	}

	r = httptest.NewRequest(http.MethodPost, "/a2a", nil)
	r.Header.Set("Origin", "http://evil.test")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("Access-Control-Allow-Origin") != "" || w.Code != http.StatusTeapot {
		t.Fatalf("other origin: %d %v", w.Code, w.Header())
	}
}
