package pdp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRefererIsExplorePage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		referer string
		want    bool
	}{
		{name: "empty", referer: "", want: false},
		{name: "wrong path", referer: "https://sp.example/piece/bafk", want: false},
		{name: "pdp path", referer: "https://sp.example/pdp/data-sets/1", want: false},
		{name: "explore root without id", referer: "https://sp.example/explore/data-sets", want: false},
		{name: "explore with id", referer: "https://sp.example/explore/data-sets/42", want: true},
		{name: "explore with trailing slash", referer: "https://sp.example/explore/data-sets/42/", want: true},
		{name: "explore with query", referer: "https://sp.example/explore/data-sets/7?limit=10", want: true},
		{name: "explore with hash", referer: "https://sp.example/explore/data-sets/7#row-3", want: true},
		{name: "relative path", referer: "/explore/data-sets/1", want: true},
		{name: "garbage", referer: "://bad", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := refererIsExplorePage(tc.referer)
			if got != tc.want {
				t.Fatalf("refererIsExplorePage(%q) = %v, want %v", tc.referer, got, tc.want)
			}
		})
	}
}

type stubAuth struct {
	service string
	err     error
}

func (a *stubAuth) AuthService(r *http.Request) (string, error) {
	return a.service, a.err
}

func TestAllowExploreList(t *testing.T) {
	t.Parallel()

	t.Run("jwt ok", func(t *testing.T) {
		t.Parallel()
		p := &PDPService{Auth: &stubAuth{service: "public"}}
		req := httptest.NewRequest(http.MethodGet, "/explore/data-sets/1/pieces", nil)
		if !p.allowExploreList(req) {
			t.Fatal("expected JWT auth to allow explore list")
		}
	})

	t.Run("jwt fail without referer", func(t *testing.T) {
		t.Parallel()
		p := &PDPService{Auth: &stubAuth{err: http.ErrNoCookie}}
		req := httptest.NewRequest(http.MethodGet, "/explore/data-sets/1/pieces", nil)
		if p.allowExploreList(req) {
			t.Fatal("expected deny without JWT or referer")
		}
	})

	t.Run("jwt fail with explore referer", func(t *testing.T) {
		t.Parallel()
		p := &PDPService{Auth: &stubAuth{err: http.ErrNoCookie}}
		req := httptest.NewRequest(http.MethodGet, "/explore/data-sets/1/pieces", nil)
		req.Header.Set("Referer", "https://sp.example/explore/data-sets/1")
		if !p.allowExploreList(req) {
			t.Fatal("expected explore referer to allow list")
		}
	})
}
