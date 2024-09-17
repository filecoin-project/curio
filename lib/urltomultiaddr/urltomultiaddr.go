package urltomultiaddr

import (
	"errors"
	"fmt"
	"net"
	"net/url"

	"github.com/multiformats/go-multiaddr"
)

const protocol = "tcp"

func UrlToMultiaddr(urlStr string) (multiaddr.Multiaddr, error) {
	// Parse the URL
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	// Extract the host and port
	host := parsedURL.Hostname()
	port := parsedURL.Port()
	if port == "" {
		// Default port based on the scheme
		switch parsedURL.Scheme {
		case "https":
			port = "443"
		case "http":
			port = "80"
		default:
			return nil, errors.New("invalid URL scheme")
		}
	}

	// Determine if the host is an IP address or a DNS name
	var addrStr string
	if ip := net.ParseIP(host); ip != nil {
		// It's an IP address, check if it's IPv4 or IPv6
		if ip.To4() != nil {
			// IPv4
			addrStr = fmt.Sprintf("/ip4/%s/%s/%s/%s", ip.String(), protocol, port, parsedURL.Scheme)
		} else {
			// IPv6
			addrStr = fmt.Sprintf("/ip6/%s/%s/%s/%s", ip.String(), protocol, port, parsedURL.Scheme)
		}
	} else {
		// It's a DNS name
		addrStr = fmt.Sprintf("/dns/%s/%s/%s/%s", host, protocol, port, parsedURL.Scheme)
	}

	return multiaddr.NewMultiaddr(addrStr)
}
