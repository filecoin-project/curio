package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/filecoin-project/curio/cuhttp"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/deps/config"
)

func testAuth(t *testing.T, store Store, cfg *config.CurioConfig) (*Auth, http.Handler) {
	t.Helper()
	if cfg == nil {
		cfg = config.DefaultCurioConfig()
		cfg.Addresses = config.NewDynamic([]config.CurioAddresses{{
			MinerAddresses:     []string{"f01234"},
			PreCommitControl:   []string{"f1control"},
			CommitControl:      []string{},
			DealPublishControl: []string{},
			TerminateControl:   []string{},
		}})
	}
	a := New(Options{
		Store: store,
		Deps:  &deps.Deps{Cfg: cfg},
	})
	r := mux.NewRouter()
	a.Register(r)
	r.HandleFunc("/api/webrpc/v0", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"ok","id":1}`))
	})
	r.HandleFunc("/api/config/layers", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	})
	r.HandleFunc("/debug/pprof/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pprof"))
	})
	r.HandleFunc("/pages/tasks/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("tasks"))
	})
	return a, a.Middleware(r)
}

func postJSON(t *testing.T, h http.Handler, path string, body any, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, json.NewEncoder(&buf).Encode(body))
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Host = "localhost:4701"
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func get(t *testing.T, h http.Handler, path string, cookies []*http.Cookie, accept string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "localhost:4701"
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func sessionCookies(rr *httptest.ResponseRecorder) []*http.Cookie {
	var out []*http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == sessionCookieName {
			out = append(out, c)
		}
	}
	return out
}

func TestUnauthenticatedDenied(t *testing.T) {
	store := NewMemStore()
	_, h := testAuth(t, store, nil)

	for _, path := range []string{"/api/webrpc/v0", "/api/config/layers", "/debug/pprof/"} {
		rr := get(t, h, path, nil, "application/json")
		require.Equal(t, http.StatusUnauthorized, rr.Code, path)
	}

	rr := get(t, h, "/pages/tasks/", nil, "text/html")
	require.Equal(t, http.StatusFound, rr.Code)
	require.Equal(t, "/setup", rr.Header().Get("Location"))
}

func TestSetupLoginAndAccess(t *testing.T) {
	store := NewMemStore()
	_, h := testAuth(t, store, nil)

	rr := postJSON(t, h, "/api/auth/setup", map[string]string{
		"username": "admin",
		"password": "password12345",
		"confirm":  "password12345",
	}, nil)
	require.Equal(t, http.StatusOK, rr.Code)
	cookies := sessionCookies(rr)
	require.NotEmpty(t, cookies)

	rr2 := postJSON(t, h, "/api/auth/setup", map[string]string{
		"username": "other",
		"password": "password12345",
		"confirm":  "password12345",
	}, nil)
	require.Equal(t, http.StatusConflict, rr2.Code)

	for _, path := range []string{"/api/webrpc/v0", "/api/config/layers", "/debug/pprof/", "/pages/tasks/"} {
		ok := get(t, h, path, cookies, "application/json")
		require.Equal(t, http.StatusOK, ok.Code, path)
	}
}

func TestBadPassword(t *testing.T) {
	store := NewMemStore()
	_, h := testAuth(t, store, nil)
	require.Equal(t, http.StatusOK, postJSON(t, h, "/api/auth/setup", map[string]string{
		"username": "admin",
		"password": "password12345",
		"confirm":  "password12345",
	}, nil).Code)

	// Wait out the 5s gate from setup's lack of claim — login claims on its own.
	// Setup does not claim; first login should succeed after claiming.
	store.lastPasswordAttemptAt = time.Time{}

	rr := postJSON(t, h, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "wrong-password!",
	}, nil)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestWebSocketUpgradeWithoutCookie(t *testing.T) {
	store := NewMemStore()
	_, h := testAuth(t, store, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/webrpc/v0", nil)
	req.Host = "localhost:4701"
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestIdentityPublic(t *testing.T) {
	store := NewMemStore()
	cfg := config.DefaultCurioConfig()
	cfg.Addresses = config.NewDynamic([]config.CurioAddresses{{
		MinerAddresses:   []string{"f01234"},
		PreCommitControl: []string{"f1abc"},
	}})
	_, h := testAuth(t, store, cfg)

	rr := get(t, h, "/api/auth/identity", nil, "application/json")
	require.Equal(t, http.StatusOK, rr.Code)
	var id Identity
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&id))
	require.Equal(t, "curio", id.Product)
	require.NotEmpty(t, id.Miners)
	require.Equal(t, []string{"f01234"}, id.Miners[0].MinerAddresses)
	require.Equal(t, []string{"f1abc"}, id.Miners[0].PreCommitControl)
	require.Nil(t, id.PDPWallet)
	body := rr.Body.String()
	require.NotContains(t, body, "private")
	require.NotContains(t, body, "private_key")
}

func TestChangePassword(t *testing.T) {
	store := NewMemStore()
	_, h := testAuth(t, store, nil)

	rr := postJSON(t, h, "/api/auth/setup", map[string]string{
		"username": "admin",
		"password": "password12345",
		"confirm":  "password12345",
	}, nil)
	require.Equal(t, http.StatusOK, rr.Code)
	cookies := sessionCookies(rr)
	require.NotEmpty(t, cookies)

	// Unauthenticated change is blocked by middleware.
	rrUnauth := postJSON(t, h, "/api/auth/password", map[string]string{
		"current":  "password12345",
		"password": "newpassword123",
		"confirm":  "newpassword123",
	}, nil)
	require.Equal(t, http.StatusUnauthorized, rrUnauth.Code)

	store.lastPasswordAttemptAt = time.Time{}
	rrWrong := postJSON(t, h, "/api/auth/password", map[string]string{
		"current":  "not-the-password",
		"password": "newpassword123",
		"confirm":  "newpassword123",
	}, cookies)
	require.Equal(t, http.StatusUnauthorized, rrWrong.Code)

	store.lastPasswordAttemptAt = time.Time{}
	rrOK := postJSON(t, h, "/api/auth/password", map[string]string{
		"current":  "password12345",
		"password": "newpassword123",
		"confirm":  "newpassword123",
	}, cookies)
	require.Equal(t, http.StatusOK, rrOK.Code)
	newCookies := sessionCookies(rrOK)
	require.NotEmpty(t, newCookies)

	// Old session cookie no longer works.
	rrOld := get(t, h, "/api/webrpc/v0", cookies, "application/json")
	require.Equal(t, http.StatusUnauthorized, rrOld.Code)

	store.lastPasswordAttemptAt = time.Time{}
	rrLoginOld := postJSON(t, h, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "password12345",
	}, nil)
	require.Equal(t, http.StatusUnauthorized, rrLoginOld.Code)

	store.lastPasswordAttemptAt = time.Time{}
	rrLoginNew := postJSON(t, h, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "newpassword123",
	}, nil)
	require.Equal(t, http.StatusOK, rrLoginNew.Code)
}

func TestPasswordAttemptRateLimit(t *testing.T) {
	store := NewMemStore()
	_, h := testAuth(t, store, nil)
	require.Equal(t, http.StatusOK, postJSON(t, h, "/api/auth/setup", map[string]string{
		"username": "admin",
		"password": "password12345",
		"confirm":  "password12345",
	}, nil).Code)

	store.lastPasswordAttemptAt = time.Time{}
	first := postJSON(t, h, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "password12345",
	}, nil)
	require.Equal(t, http.StatusOK, first.Code)

	second := postJSON(t, h, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "password12345",
	}, nil)
	require.Equal(t, http.StatusTooManyRequests, second.Code)
}

func TestGuiTLSConfigOnlyWhenPublicTLS(t *testing.T) {
	cfgOff := config.DefaultCurioConfig()
	cfgOff.HTTP.Enable = false
	require.False(t, cuhttp.TLSEnabled(&cfgOff.HTTP))

	cfgDelegated := config.DefaultCurioConfig()
	cfgDelegated.HTTP.Enable = true
	cfgDelegated.HTTP.DomainName = "example.com"
	cfgDelegated.HTTP.DelegateTLS = true
	require.False(t, cuhttp.TLSEnabled(&cfgDelegated.HTTP))

	cfgOn := config.DefaultCurioConfig()
	cfgOn.HTTP.Enable = true
	cfgOn.HTTP.DomainName = "example.com"
	cfgOn.HTTP.DelegateTLS = false
	require.True(t, cuhttp.TLSEnabled(&cfgOn.HTTP))
}

func TestBcryptCost(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte(strings.Repeat("x", 12)), bcryptCost)
	require.NoError(t, err)
	cost, err := bcrypt.Cost(hash)
	require.NoError(t, err)
	require.Equal(t, bcryptCost, cost)
}
