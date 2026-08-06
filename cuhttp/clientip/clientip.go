package clientip

import (
	"net"
	"net/http"
	"net/netip"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
)

// Middleware resolves the client IP from the TCP peer by default. A loopback
// peer is treated as one trusted reverse-proxy hop, matching Curio's documented
// same-host proxy setup. The proxy must overwrite or append X-Forwarded-For.
func Middleware(next http.Handler) http.Handler {
	direct := middleware.ClientIPFromRemoteAddr(next)
	loopbackProxy := middleware.ClientIPFromRemoteAddr(
		middleware.ClientIPFromXFFTrustedProxies(1)(next),
	)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ip, ok := remoteIP(r.RemoteAddr); ok && ip.IsLoopback() {
			loopbackProxy.ServeHTTP(w, r)
			return
		}
		direct.ServeHTTP(w, r)
	})
}

// FromRequest returns the client IP resolved by Middleware. The RemoteAddr
// fallback keeps independently-mounted handlers safe and usable in tests.
func FromRequest(r *http.Request) string {
	if ip := middleware.GetClientIP(r.Context()); ip != "" {
		return ip
	}

	if ip, ok := remoteIP(r.RemoteAddr); ok {
		return ip.String()
	}
	return remoteHost(r.RemoteAddr)
}

// RateLimitKey returns a normalized per-client key. IPv6 clients are grouped
// by /64 so rotating addresses within one delegated subnet cannot evade a
// limit.
func RateLimitKey(r *http.Request) (string, error) {
	return httprate.CanonicalizeIP(FromRequest(r)), nil
}

func remoteIP(remoteAddr string) (netip.Addr, bool) {
	ip, err := netip.ParseAddr(remoteHost(remoteAddr))
	if err != nil {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}

func remoteHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
