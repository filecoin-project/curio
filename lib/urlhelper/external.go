package urlhelper

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/multiformats/go-multiaddr"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
)

// GetExternalURL returns the external URL for IPNI announcements based on HTTPConfig.
// If ExternalURL is set, it is returned after validation.
// Otherwise, constructs URL from DomainName using default port based on build type.
func GetExternalURL(c *config.HTTPConfig) (*url.URL, error) {
	if c.ExternalURL != "" {
		u, err := url.Parse(c.ExternalURL)
		if err != nil {
			return nil, fmt.Errorf("parsing ExternalURL: %w", err)
		}
		return u, nil
	}

	// Fallback to existing logic
	if build.BuildType == build.BuildMainnet || build.BuildType == build.BuildCalibnet {
		return url.Parse(fmt.Sprintf("https://%s", c.DomainName))
	}

	// Devnet: use ListenAddress port
	ls := strings.Split(c.ListenAddress, ":")
	if len(ls) != 2 {
		return nil, fmt.Errorf("invalid ListenAddress format: %s", c.ListenAddress)
	}
	return url.Parse(fmt.Sprintf("http://%s:%s", c.DomainName, ls[1]))
}

// GetExternalLibp2pAddr returns the multiaddr for libp2p WebSocket announcements.
func GetExternalLibp2pAddr(c *config.HTTPConfig) (multiaddr.Multiaddr, error) {
	u, err := GetExternalURL(c)
	if err != nil {
		return nil, err
	}

	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	protocol := "wss"
	if u.Scheme == "http" {
		protocol = "ws"
	}

	return multiaddr.NewMultiaddr(fmt.Sprintf("/dns/%s/tcp/%s/%s", u.Hostname(), port, protocol))
}

// GetExternalHTTPAddr returns the multiaddr for HTTP announcements.
func GetExternalHTTPAddr(c *config.HTTPConfig) (multiaddr.Multiaddr, error) {
	u, err := GetExternalURL(c)
	if err != nil {
		return nil, err
	}

	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	protocol := "https"
	if u.Scheme == "http" {
		protocol = "http"
	}

	return multiaddr.NewMultiaddr(fmt.Sprintf("/dns/%s/tcp/%s/%s", u.Hostname(), port, protocol))
}
