package urlhelper

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
)

// GetExternalURL returns the external URL for IPNI announcements based on HTTPConfig.
// If the DEV_CURIO_EXTERNAL_URL environment variable is set, it is used as the external URL.
// Otherwise, constructs URL from DomainName using default port based on build type.
func GetExternalURL(c *config.HTTPConfig) (*url.URL, error) {
	if devURL := os.Getenv("DEV_CURIO_EXTERNAL_URL"); devURL != "" {
		u, err := url.Parse(devURL)
		if err != nil {
			return nil, fmt.Errorf("parsing DEV_CURIO_EXTERNAL_URL: %w", err)
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

func FromURLWithPort(u *url.URL) (multiaddr.Multiaddr, error) {
	h := u.Hostname()
	var addr *multiaddr.Multiaddr
	if n := net.ParseIP(h); n != nil {
		ipAddr, err := manet.FromIP(n)
		if err != nil {
			return nil, err
		}
		addr = &ipAddr
	} else {
		// domain name
		ma, err := multiaddr.NewComponent(multiaddr.ProtocolWithCode(multiaddr.P_DNS).Name, h)
		if err != nil {
			return nil, err
		}
		mab := multiaddr.Cast(ma.Bytes())
		addr = &mab
	}
	pv := u.Port()
	if pv != "" {
		port, err := multiaddr.NewComponent(multiaddr.ProtocolWithCode(multiaddr.P_TCP).Name, pv)
		if err != nil {
			return nil, err
		}
		wport := multiaddr.Join(*addr, port)
		addr = &wport
	} else {
		// default ports for http and https
		var port *multiaddr.Component
		var err error
		switch u.Scheme {
		case "http":
			port, err = multiaddr.NewComponent(multiaddr.ProtocolWithCode(multiaddr.P_TCP).Name, "80")
			if err != nil {
				return nil, err
			}
		case "https":
			port, err = multiaddr.NewComponent(multiaddr.ProtocolWithCode(multiaddr.P_TCP).Name, "443")
		}
		if err != nil {
			return nil, err
		}
		wport := multiaddr.Join(*addr, port)
		addr = &wport
	}

	http, err := multiaddr.NewComponent(u.Scheme, "")
	if err != nil {
		return nil, err
	}

	joint := multiaddr.Join(*addr, http)
	if u.Path != "" {
		httppath, err := multiaddr.NewComponent(multiaddr.ProtocolWithCode(multiaddr.P_HTTP_PATH).Name, url.PathEscape(u.Path))
		if err != nil {
			return nil, err
		}
		joint = multiaddr.Join(joint, httppath)
	}

	return joint, nil
}
