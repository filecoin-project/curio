package pdp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestHandleExploreDataSetPiecesAuthGate(t *testing.T) {
	t.Parallel()

	p := &PDPService{Auth: &stubAuth{err: http.ErrNoCookie}}
	r := chi.NewRouter()
	mountExploreRoutes(r, p)

	req := httptest.NewRequest(http.MethodGet, "/explore/data-sets/1/pieces", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestHandleExploreDataSetPage(t *testing.T) {
	t.Parallel()

	p := &PDPService{Auth: &NullAuth{}}
	r := chi.NewRouter()
	mountExploreRoutes(r, p)

	req := httptest.NewRequest(http.MethodGet, "/explore/data-sets/99", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Dataset Explorer",
		"/explore/static/explore.js",
		`id="pieces-table"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q; got: %s", want, body)
		}
	}

	reqBad := httptest.NewRequest(http.MethodGet, "/explore/data-sets/not-a-number", nil)
	recBad := httptest.NewRecorder()
	r.ServeHTTP(recBad, reqBad)
	if recBad.Code != http.StatusBadRequest {
		t.Fatalf("invalid id status = %d", recBad.Code)
	}
}

func TestExploreStaticAssets(t *testing.T) {
	t.Parallel()

	p := &PDPService{Auth: &NullAuth{}}
	r := chi.NewRouter()
	mountExploreRoutes(r, p)

	for _, path := range []string{"/explore/static/explore.js", "/explore/static/explore.css"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Fatalf("%s empty body", path)
		}
	}
}
