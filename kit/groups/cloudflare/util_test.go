package cloudflare

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newRawServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

func asError(err error, target **Error) bool { return errors.As(err, target) }
