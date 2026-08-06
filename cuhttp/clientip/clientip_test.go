package clientip

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

func TestMiddlewareTrustModel(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		wantIP     string
	}{
		{
			name:       "direct peer ignores forwarding headers",
			remoteAddr: "203.0.113.10:1234",
			xff:        "198.51.100.20",
			wantIP:     "203.0.113.10",
		},
		{
			name:       "loopback proxy uses rightmost forwarded address",
			remoteAddr: "127.0.0.1:1234",
			xff:        "198.51.100.20, 192.0.2.30",
			wantIP:     "192.0.2.30",
		},
		{
			name:       "loopback proxy without header falls back to peer",
			remoteAddr: "[::1]:1234",
			wantIP:     "::1",
		},
		{
			name:       "loopback proxy with malformed header falls back to peer",
			remoteAddr: "127.0.0.1:1234",
			xff:        "198.51.100.20, invalid",
			wantIP:     "127.0.0.1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var gotIP, gotRemoteAddr string
			handler := Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				gotIP = middleware.GetClientIP(r.Context())
				gotRemoteAddr = r.RemoteAddr
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = test.remoteAddr
			req.Header.Set("X-Forwarded-For", test.xff)
			req.Header.Set("X-Real-IP", "198.51.100.21")
			req.Header.Set("True-Client-IP", "198.51.100.22")
			req.Header.Set("Forwarded", "for=198.51.100.23")
			handler.ServeHTTP(httptest.NewRecorder(), req)

			if gotIP != test.wantIP {
				t.Fatalf("client IP = %q, want %q", gotIP, test.wantIP)
			}
			if gotRemoteAddr != test.remoteAddr {
				t.Fatalf("RemoteAddr = %q, want unchanged %q", gotRemoteAddr, test.remoteAddr)
			}
		})
	}
}

func TestRateLimitKeyCanonicalizesIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{
			name:       "IPv6 is grouped by prefix",
			remoteAddr: "[2001:db8:1:2:abcd::1]:1234",
			want:       "2001:db8:1:2::",
		},
		{
			name:       "IPv4-mapped IPv6 is unmapped",
			remoteAddr: "[::ffff:192.0.2.10]:1234",
			want:       "192.0.2.10",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = test.remoteAddr
			got, err := RateLimitKey(req)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("rate-limit key = %q, want %q", got, test.want)
			}
		})
	}
}
