package auth

import (
	"net/http"
	"strings"
)

// Middleware enforces sessions for API and debug routes.
// Static UI is public; page JS checks /api/auth/status and routes to /login/ or /setup/.
func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.isPublic(r) {
			next.ServeHTTP(w, r)
			return
		}

		if a.sessionAuthenticated(r.Context(), r) {
			next.ServeHTTP(w, r)
			return
		}

		has, err := a.store.HasAdmin(r.Context())
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !has {
			writeJSONError(w, http.StatusUnauthorized, "setup required")
			return
		}
		writeJSONError(w, http.StatusUnauthorized, "authentication required")
	})
}

// isPublic reports routes that must work before authentication.
// Setup POST is public only until the singleton admin exists.
// Non-API GETs (static UI, including /login/) are public so auth can be JS-based.
func (a *Auth) isPublic(r *http.Request) bool {
	path := r.URL.Path
	switch {
	case path == "/api/auth/setup" && r.Method == http.MethodPost:
		return a.setupStillOpen(r)
	case path == "/api/auth/status" && r.Method == http.MethodGet:
		return true
	case path == "/api/auth/identity" && r.Method == http.MethodGet:
		return true
	case path == "/api/auth/login" && r.Method == http.MethodPost:
		return true
	case path == "/api/auth/logout" && r.Method == http.MethodPost:
		return true
	case (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
		!strings.HasPrefix(path, "/api/") &&
		!strings.HasPrefix(path, "/debug/"):
		return true
	default:
		return false
	}
}

// setupStillOpen is true when no admin row exists yet. Errors fail closed (not public).
func (a *Auth) setupStillOpen(r *http.Request) bool {
	has, err := a.store.HasAdmin(r.Context())
	if err != nil {
		return false
	}
	return !has
}
