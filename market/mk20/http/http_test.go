package http

import (
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestRegisterRateLimitedRouteUsesIndependentLimiters(t *testing.T) {
	router := chi.NewRouter()
	authMiddleware := func(next stdhttp.Handler) stdhttp.Handler {
		return next
	}
	okHandler := stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusOK)
	})

	registerRateLimitedRoute(router, authMiddleware, 1, stdhttp.MethodGet, "/one", okHandler)
	registerRateLimitedRoute(router, authMiddleware, 1, stdhttp.MethodGet, "/two", okHandler)

	request := func(path string) *stdhttp.Request {
		req := httptest.NewRequest(stdhttp.MethodGet, path, nil)
		req.RemoteAddr = "203.0.113.10:1234"
		return req
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request("/one"))
	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("first /one request status = %d, want %d", recorder.Code, stdhttp.StatusOK)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request("/one"))
	if recorder.Code != stdhttp.StatusTooManyRequests {
		t.Fatalf("second /one request status = %d, want %d", recorder.Code, stdhttp.StatusTooManyRequests)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request("/two"))
	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("first /two request status = %d, want %d", recorder.Code, stdhttp.StatusOK)
	}
}
