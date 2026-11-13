package pdp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBearerTokenReadsLaterAuthorization(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/pdp/ping", nil)
	req.Header.Add("Authorization", "FOC-Grant abc")
	req.Header.Add("Authorization", "Bearer jwt-token")
	got, err := bearerToken(req)
	if err != nil {
		t.Fatal(err)
	}
	if got != "jwt-token" {
		t.Fatalf("bearerToken() = %q, want jwt-token", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/pdp/ping", nil)
	req.Header.Add("Authorization", "Bearer ")
	req.Header.Add("Authorization", "Bearer later")
	got, err = bearerToken(req)
	if err != nil {
		t.Fatal(err)
	}
	if got != "later" {
		t.Fatalf("bearerToken() = %q, want later", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/pdp/ping", nil)
	_, err = bearerToken(req)
	if err == nil || err.Error() != "missing Authorization header" {
		t.Fatalf("bearerToken() err = %v, want missing Authorization header", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/pdp/ping", nil)
	req.Header.Set("Authorization", "FOC-Grant abc")
	_, err = bearerToken(req)
	if err == nil || err.Error() != "invalid Authorization header format" {
		t.Fatalf("bearerToken() err = %v, want invalid Authorization header format", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/pdp/ping", nil)
	req.Header.Set("Authorization", "Bearer ")
	_, err = bearerToken(req)
	if err == nil || err.Error() != "empty token" {
		t.Fatalf("bearerToken() err = %v, want empty token", err)
	}
}
