package cuhttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

func TestNewRouterDoesNotTrustForwardingHeadersFromDirectClient(t *testing.T) {
	const remoteAddr = "203.0.113.10:1234"

	var gotIP, gotRemoteAddr string
	router := NewRouter(RouterConfig{})
	router.Get("/", func(_ http.ResponseWriter, r *http.Request) {
		gotIP = middleware.GetClientIP(r.Context())
		gotRemoteAddr = r.RemoteAddr
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	req.Header.Set("X-Forwarded-For", "198.51.100.20")
	req.Header.Set("X-Real-IP", "198.51.100.21")
	req.Header.Set("True-Client-IP", "198.51.100.22")
	req.Header.Set("Forwarded", "for=198.51.100.23")
	router.ServeHTTP(httptest.NewRecorder(), req)

	if gotIP != "203.0.113.10" {
		t.Fatalf("client IP = %q, want TCP peer", gotIP)
	}
	if gotRemoteAddr != remoteAddr {
		t.Fatalf("RemoteAddr = %q, want unchanged %q", gotRemoteAddr, remoteAddr)
	}
}
