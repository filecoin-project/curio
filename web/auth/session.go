package auth

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gbrlsnchs/jwt/v3"
	"golang.org/x/xerrors"
)

const (
	sessionCookieName = "curio_session"
	sessionTTL        = 24 * time.Hour
)

// guiSessionClaims is the JWT payload for an admin GUI session cookie.
type guiSessionClaims struct {
	Salt string `json:"salt"`
	jwt.Payload
}

// mintSessionToken signs a JWT with the admin session key; salt must match for verify.
func mintSessionToken(admin Admin) (string, error) {
	if len(admin.SessionKey) == 0 || len(admin.SessionSalt) == 0 {
		return "", xerrors.Errorf("missing session signing material")
	}
	now := time.Now()
	claims := guiSessionClaims{
		Salt: hex.EncodeToString(admin.SessionSalt),
		Payload: jwt.Payload{
			IssuedAt:       jwt.NumericDate(now),
			ExpirationTime: jwt.NumericDate(now.Add(sessionTTL)),
		},
	}
	token, err := jwt.Sign(claims, jwt.NewHS256(admin.SessionKey))
	if err != nil {
		return "", err
	}
	return string(token), nil
}

// verifySessionToken reports whether raw is a valid unexpired JWT for admin's key+salt.
func verifySessionToken(admin Admin, raw string) bool {
	if raw == "" || len(admin.SessionKey) == 0 || len(admin.SessionSalt) == 0 {
		return false
	}
	var claims guiSessionClaims
	_, err := jwt.Verify([]byte(raw), jwt.NewHS256(admin.SessionKey), &claims,
		jwt.ValidatePayload(&claims.Payload, jwt.ExpirationTimeValidator(time.Now())))
	if err != nil {
		return false
	}
	want := hex.EncodeToString(admin.SessionSalt)
	return subtle.ConstantTimeCompare([]byte(claims.Salt), []byte(want)) == 1
}

// setSessionCookie writes the session cookie. secure is true when the GUI serves TLS.
func setSessionCookie(w http.ResponseWriter, raw string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    raw,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

// clearSessionCookie expires the session cookie.
func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   secure,
		MaxAge:   -1,
	})
}

// sessionRaw returns the raw cookie value, or empty when missing.
func sessionRaw(r *http.Request) string {
	c, err := r.Cookie(sessionCookieName)
	if err != nil || c == nil {
		return ""
	}
	return c.Value
}

// sessionAuthenticated loads admin secrets and verifies the request cookie.
func (a *Auth) sessionAuthenticated(ctx context.Context, r *http.Request) bool {
	raw := sessionRaw(r)
	if raw == "" {
		return false
	}
	admin, err := a.store.GetAdmin(ctx)
	if err != nil {
		return false
	}
	return verifySessionToken(admin, raw)
}
