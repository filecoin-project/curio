package auth

import (
	"net/http"

	"github.com/gorilla/mux"

	"github.com/filecoin-project/curio/deps"
)

// Auth holds GUI authentication state for one admin HTTP server.
type Auth struct {
	store         Store
	deps          *deps.Deps
	fails         *failLimiter
	secureCookies bool
}

// Options configures Auth construction.
type Options struct {
	Store         Store
	Deps          *deps.Deps
	SecureCookies bool
}

// New builds Auth. Store and Deps must be non-nil for production use.
func New(opts Options) *Auth {
	return &Auth{
		store:         opts.Store,
		deps:          opts.Deps,
		fails:         newFailLimiter(),
		secureCookies: opts.SecureCookies,
	}
}

// Register mounts setup/login/password pages and auth API routes on r.
func (a *Auth) Register(r *mux.Router) {
	r.HandleFunc("/setup", a.handleSetupPage).Methods(http.MethodGet)
	r.HandleFunc("/login", a.handleLoginPage).Methods(http.MethodGet)
	r.HandleFunc("/password", a.handlePasswordPage).Methods(http.MethodGet)

	r.HandleFunc("/api/auth/status", a.handleStatus).Methods(http.MethodGet)
	r.HandleFunc("/api/auth/identity", a.handleIdentity).Methods(http.MethodGet)
	r.HandleFunc("/api/auth/setup", a.handleSetup).Methods(http.MethodPost)
	r.HandleFunc("/api/auth/login", a.handleLogin).Methods(http.MethodPost)
	r.HandleFunc("/api/auth/logout", a.handleLogout).Methods(http.MethodPost)
	r.HandleFunc("/api/auth/password", a.handleChangePassword).Methods(http.MethodPost)
}
