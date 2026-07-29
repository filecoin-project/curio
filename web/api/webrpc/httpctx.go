package webrpc

import (
	"context"
	"net/http"
)

type httpRequestKey struct{}

// withHTTPRequest stores the inbound HTTP request on the context so RPC
// methods can inspect headers (e.g. Referer) from WebSocket upgrades or POSTs.
func withHTTPRequest(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, httpRequestKey{}, r)
}

// httpRequestFromContext returns the request injected by injectHTTPRequest.
func httpRequestFromContext(ctx context.Context) *http.Request {
	r, _ := ctx.Value(httpRequestKey{}).(*http.Request)
	return r
}

// injectHTTPRequest wraps an RPC handler so each call's context carries the
// original *http.Request (including WebSocket upgrade headers).
func injectHTTPRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := withHTTPRequest(r.Context(), r)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
