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
)

// Credential returns the longest FOC grant on r, decoded.
// A missing Authorization header, basic-auth password, or cookie is skipped.
// The Authorization value is used when it is not HTTP basic auth and not Bearer.
// Basic auth contributes its password only when the user is "FOC Grant" or "FOC%20Grant".
func Credential(r *http.Request) ([]byte, error) {
	token, err := GrantToken(r)
	if err != nil {
		return nil, err
	}
	return decodeToken(token)
}

// GrantToken returns the longest FOC grant token on r, still base64url-encoded.
// Bearer is not a grant. A request with none of the FOC sources is ErrNoGrant.
func GrantToken(r *http.Request) (string, error) {
	candidates := grantCandidates(r)
	if len(candidates) == 0 {
		return "", ErrNoGrant
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if len(c) > len(best) {
			best = c
		}
	}
	return normalizeGrantToken(best)
}

// grantCandidates lists FOC grant strings on r. Bearer is omitted.
// The caller picks the longest and decodes it.
func grantCandidates(r *http.Request) []string {
	var candidates []string
	if auth := r.Header.Get("Authorization"); auth != "" {
		scheme := strings.ToLower(auth)
		if !strings.HasPrefix(scheme, "basic ") && !strings.HasPrefix(scheme, "bearer ") {
			candidates = append(candidates, auth)
		}
	}
	if user, pass, ok := r.BasicAuth(); ok && pass != "" && (user == grantUser || user == grantUserEsc) {
		candidates = append(candidates, pass)
	}
	candidates = append(candidates, grantCookies(r)...)
	return candidates
}

// normalizeGrantToken strips the FOC-Grant scheme and checks the token decodes.
// The caller has already chosen this candidate.
func normalizeGrantToken(s string) (string, error) {
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(s, grantPrefix); ok {
		s = strings.TrimSpace(rest)
	}
	if _, err := decodeToken(s); err != nil {
		return "", err
	}
	return s, nil
}

// grantCookies reads an FOC Grant cookie from the parsed cookies and the raw
// Cookie header. A space in the name is not a token the standard parser always keeps.
func grantCookies(r *http.Request) []string {
	var out []string
	for _, c := range r.Cookies() {
		if c.Value != "" && (c.Name == grantCookie || c.Name == grantCookieEsc) {
			out = append(out, c.Value)
		}
	}
	for _, line := range r.Header.Values("Cookie") {
		for _, part := range strings.Split(line, ";") {
			name, val, ok := strings.Cut(strings.TrimSpace(part), "=")
			if !ok || val == "" {
				continue
			}
			if name == grantCookie || name == grantCookieEsc {
				out = append(out, val)
			}
		}
	}
	return out
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
