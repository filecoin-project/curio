package pdp

import (
	"fmt"
	"net/url"
)

const focGrantUser = "FOC Grant"

// WithFOCGrantUserinfo sets the URL's basic-auth user to "FOC Grant" and the
// password to token. A URL that already carries FOC grant userinfo is returned
// unchanged. The caller has a non-empty token that already decoded.
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

// SplitPullGrantURL separates FOC grant userinfo from a pull source URL.
// clean is the URL to validate and dial. user is nil when raw has no userinfo.
// The caller has not yet checked the host.
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

// validateFOCUserinfo allows a missing user, or basic auth whose user is the
// FOC grant user and whose password is non-empty. The caller has parsed raw.
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
