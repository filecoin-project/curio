package keysmith

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ErrNoGrant means the request carried none of the FOC grant sources.
var ErrNoGrant = errors.New("missing FOC grant")

// ErrInvalidGrant means a grant was present and did not verify.
var ErrInvalidGrant = errors.New("invalid FOC grant")

const (
	grantPrefix    = "FOC-Grant "
	grantUser      = "FOC Grant"
	grantUserEsc   = "FOC%20Grant"
	grantCookie    = "FOC Grant"
	grantCookieEsc = "FOC%20Grant"
	grantQuery     = "foc_grant"
)

// Credential returns the longest FOC grant on r, decoded.
// Each Authorization field is considered. Bearer is skipped.
// Basic auth contributes its password only when the user is "FOC Grant" or "FOC%20Grant".
// A cookie or foc_grant query is also a source. foc_grant is a base64url query value.
// A missing source is skipped.
func Credential(r *http.Request) ([]byte, error) {
	token, err := GrantToken(r)
	if err != nil {
		return nil, err
	}
	return decodeToken(token)
}

// GrantToken returns the longest FOC grant token on r, still base64url-encoded.
// Bearer field values are not grants. A request with none of the FOC sources is ErrNoGrant.
func GrantToken(r *http.Request) (string, error) {
	var candidates []string
	for _, auth := range r.Header.Values("Authorization") {
		if auth == "" {
			continue
		}
		scheme := strings.ToLower(auth)
		if strings.HasPrefix(scheme, "bearer ") {
			continue
		}
		if strings.HasPrefix(scheme, "basic ") {
			const prefix = "Basic "
			if len(auth) >= len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
				raw, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
				if err == nil {
					user, pass, ok := strings.Cut(string(raw), ":")
					if ok && pass != "" && (user == grantUser || user == grantUserEsc) {
						candidates = append(candidates, pass)
					}
				}
			}
			continue
		}
		candidates = append(candidates, auth)
	}
	// A space in the cookie name is not a token the standard parser always keeps.
	for _, c := range r.Cookies() {
		if c.Value != "" && (c.Name == grantCookie || c.Name == grantCookieEsc) {
			candidates = append(candidates, c.Value)
		}
	}
	for _, line := range r.Header.Values("Cookie") {
		for _, part := range strings.Split(line, ";") {
			name, val, ok := strings.Cut(strings.TrimSpace(part), "=")
			if !ok || val == "" {
				continue
			}
			if name == grantCookie || name == grantCookieEsc {
				candidates = append(candidates, val)
			}
		}
	}
	if r.URL != nil {
		for _, v := range r.URL.Query()[grantQuery] {
			if v != "" {
				candidates = append(candidates, v)
			}
		}
	}
	if len(candidates) == 0 {
		return "", ErrNoGrant
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if len(c) > len(best) {
			best = c
		}
	}
	best = strings.TrimSpace(best)
	if rest, ok := strings.CutPrefix(best, grantPrefix); ok {
		best = strings.TrimSpace(rest)
	}
	if _, err := decodeToken(best); err != nil {
		return "", err
	}
	return best, nil
}

// decodeToken accepts raw base64url or the "FOC-Grant" scheme from Keysmith.
func decodeToken(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(s, grantPrefix); ok {
		s = strings.TrimSpace(rest)
	}
	if s == "" {
		return nil, fmt.Errorf("%w: empty grant", ErrInvalidGrant)
	}
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return nil, fmt.Errorf("%w: grant is not base64url", ErrInvalidGrant)
}
