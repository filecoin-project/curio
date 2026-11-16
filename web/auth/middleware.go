package auth

import (
	"net/http"
	"strings"
)

// Middleware enforces sessions for every request except setup/login/status/identity.
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
		if wantsHTML(r) {
			if !has {
				http.Redirect(w, r, "/setup", http.StatusFound)
				return
			}
			http.Redirect(w, r, "/login", http.StatusFound)
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
func (a *Auth) isPublic(r *http.Request) bool {
	path := r.URL.Path
	switch {
	case path == "/setup" && r.Method == http.MethodGet:
		return true
	case path == "/login" && r.Method == http.MethodGet:
		return true
	case path == "/api/auth/status" && r.Method == http.MethodGet:
		return true
	case path == "/api/auth/identity" && r.Method == http.MethodGet:
		return true
	case path == "/api/auth/setup" && r.Method == http.MethodPost:
		return true
	case path == "/api/auth/login" && r.Method == http.MethodPost:
		return true
	case path == "/api/auth/logout" && r.Method == http.MethodPost:
		return true
	default:
		return false
	}
}

// wantsHTML prefers a redirect for browser navigations over a JSON 401.
func wantsHTML(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/debug/") {
		return false
	}
	accept := r.Header.Get("Accept")
	if accept == "" {
		return true
	}
	return strings.Contains(accept, "text/html") || strings.Contains(accept, "*/*")
}
