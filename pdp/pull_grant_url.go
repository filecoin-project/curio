package pdp

import (
	"fmt"
	"net/url"
)

const focGrantUser = "FOC Grant"

// WithFOCGrantUserinfo sets user "FOC Grant" and password token.
// A URL that already has that userinfo is returned unchanged. token is non-empty and already decoded.
func WithFOCGrantUserinfo(sourceURL, token string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("FOC grant password is empty")
	}
	u, err := url.Parse(sourceURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if u.User != nil {
		if err := validateFOCUserinfo(u); err != nil {
			return "", err
		}
		return sourceURL, nil
	}
	u.User = url.UserPassword(focGrantUser, token)
	return u.String(), nil
}

// SplitPullGrantURL returns the URL without FOC grant userinfo, and that userinfo.
// user is nil when raw has none. The caller has not checked the host.
func SplitPullGrantURL(raw string) (clean string, user *url.Userinfo, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.User == nil {
		return raw, nil, nil
	}
	if err := validateFOCUserinfo(u); err != nil {
		return "", nil, err
	}
	user = u.User
	u.User = nil
	return u.String(), user, nil
}

// RedactPullSourceURL drops a password from a pull source URL before logging.
func RedactPullSourceURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = url.User(focGrantUser)
	return u.String()
}

// validateFOCUserinfo accepts no user, or user "FOC Grant" / "FOC%20Grant" with a password.
// The caller has parsed the URL.
func validateFOCUserinfo(u *url.URL) error {
	if u.User == nil {
		return nil
	}
	name := u.User.Username()
	if name != focGrantUser && name != "FOC%20Grant" {
		return fmt.Errorf("url userinfo is not an FOC grant")
	}
	pass, ok := u.User.Password()
	if !ok || pass == "" {
		return fmt.Errorf("FOC grant password is empty")
	}
	return nil
}
