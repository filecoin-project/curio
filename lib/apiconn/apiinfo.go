// Package apiconn parses Lotus-compatible FULLNODE_API_INFO strings without
// importing github.com/filecoin-project/lotus.
package apiconn

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

// Info holds a parsed FULLNODE_API_INFO entry.
type Info struct {
	Addr  string
	Token []byte
}

// Parse splits "TOKEN:multiaddr" or bare multiaddr/URL forms.
func Parse(s string) Info {
	var tok []byte
	if !strings.HasPrefix(s, "/") {
		if idx := strings.Index(s, ":"); idx > 0 && !strings.HasPrefix(s[idx+1:], "//") {
			tok = []byte(s[:idx])
			s = s[idx+1:]
		}
	}
	return Info{Addr: s, Token: tok}
}

// DialArgs returns the HTTP JSON-RPC URL for API version v1.
func (a Info) DialArgs(version string) (string, error) {
	ma, err := multiaddr.NewMultiaddr(a.Addr)
	if err == nil {
		_, addr, err := manet.DialArgs(ma)
		if err != nil {
			return "", err
		}

		scheme := "ws"
		multiaddr.ForEach(ma, func(c multiaddr.Component) bool {
			switch c.Protocol().Code {
			case multiaddr.P_WSS, multiaddr.P_TLS:
				scheme = "wss"
			}
			return true
		})

		return url.JoinPath(scheme+"://"+addr, "rpc", version)
	}

	u, err := url.Parse(a.Addr)
	if err != nil {
		return "", err
	}
	return url.JoinPath(u.String(), "rpc", version)
}

// AuthHeader returns the Authorization header for the token, if any.
func (a Info) AuthHeader() http.Header {
	if len(a.Token) == 0 {
		return nil
	}
	return http.Header{"Authorization": []string{"Bearer " + string(a.Token)}}
}
