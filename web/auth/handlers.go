package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const (
	minPasswordLen = 12
	bcryptCost     = 12
	maxUsernameLen = 128
)

type credentialsBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Confirm  string `json:"confirm"`
}

type changePasswordBody struct {
	Current  string `json:"current"`
	Password string `json:"password"`
	Confirm  string `json:"confirm"`
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// sameOriginOK rejects cross-site POSTs when Origin is present and mismatched.
func sameOriginOK(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	expected := scheme + "://" + r.Host
	return strings.EqualFold(origin, expected)
}

func (a *Auth) handleStatus(w http.ResponseWriter, r *http.Request) {
	has, err := a.store.HasAdmin(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"configured":    has,
		"authenticated": a.sessionAuthenticated(r.Context(), r),
	})
}

func (a *Auth) handleSetup(w http.ResponseWriter, r *http.Request) {
	if !sameOriginOK(r) {
		writeJSONError(w, http.StatusForbidden, "origin not allowed")
		return
	}
	has, err := a.store.HasAdmin(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if has {
		writeJSONError(w, http.StatusConflict, "admin already configured")
		return
	}

	var body credentialsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	username := strings.TrimSpace(body.Username)
	if username == "" || utf8.RuneCountInString(username) > maxUsernameLen {
		writeJSONError(w, http.StatusBadRequest, "invalid username")
		return
	}
	if len(body.Password) < minPasswordLen {
		writeJSONError(w, http.StatusBadRequest, "password must be at least 12 characters")
		return
	}
	if body.Password != body.Confirm {
		writeJSONError(w, http.StatusBadRequest, "passwords do not match")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcryptCost)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := a.store.CreateAdmin(r.Context(), username, string(hash)); err != nil {
		if errors.Is(err, errAdminExists) {
			writeJSONError(w, http.StatusConflict, "admin already configured")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := a.issueSession(w, r); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
}

func (a *Auth) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !sameOriginOK(r) {
		writeJSONError(w, http.StatusForbidden, "origin not allowed")
		return
	}
	ip := clientIP(r)
	if a.fails.blocked(ip) {
		writeJSONError(w, http.StatusTooManyRequests, "too many failed attempts; try again later")
		return
	}

	var body credentialsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if err := a.store.ClaimPasswordAttempt(r.Context()); err != nil {
		if errors.Is(err, errAttemptTooSoon) {
			writeJSONError(w, http.StatusTooManyRequests, "wait 5 seconds before trying again")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	admin, err := a.store.GetAdmin(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Always bcrypt-compare so timing does not reveal a wrong username.
	errUser := subtleUsernameMatch(admin.Username, body.Username)
	errPass := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(body.Password))
	if errUser != nil || errPass != nil {
		a.fails.record(ip)
		writeJSONError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	a.fails.clear(ip)
	if err := a.issueSession(w, r); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
}

func (a *Auth) handleLogout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w, a.secureCookies)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
}

func (a *Auth) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if !sameOriginOK(r) {
		writeJSONError(w, http.StatusForbidden, "origin not allowed")
		return
	}
	ip := clientIP(r)
	if a.fails.blocked(ip) {
		writeJSONError(w, http.StatusTooManyRequests, "too many failed attempts; try again later")
		return
	}

	var body changePasswordBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(body.Password) < minPasswordLen {
		writeJSONError(w, http.StatusBadRequest, "password must be at least 12 characters")
		return
	}
	if body.Password != body.Confirm {
		writeJSONError(w, http.StatusBadRequest, "passwords do not match")
		return
	}

	if err := a.store.ClaimPasswordAttempt(r.Context()); err != nil {
		if errors.Is(err, errAttemptTooSoon) {
			writeJSONError(w, http.StatusTooManyRequests, "wait 5 seconds before trying again")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}

	admin, err := a.store.GetAdmin(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(body.Current)); err != nil {
		a.fails.record(ip)
		writeJSONError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcryptCost)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := a.store.UpdatePassword(r.Context(), string(newHash)); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	a.fails.clear(ip)
	if err := a.issueSession(w, r); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
}

// issueSession mints a signed JWT cookie. Caller already authenticated.
func (a *Auth) issueSession(w http.ResponseWriter, r *http.Request) error {
	admin, err := a.store.GetAdmin(r.Context())
	if err != nil {
		return err
	}
	raw, err := mintSessionToken(admin)
	if err != nil {
		return err
	}
	setSessionCookie(w, raw, a.secureCookies)
	return nil
}

// subtleUsernameMatch compares usernames without leaking length via bcrypt path only.
func subtleUsernameMatch(want, got string) error {
	if want == got {
		return nil
	}
	return errors.New("username mismatch")
}
