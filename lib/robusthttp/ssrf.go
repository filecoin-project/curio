package robusthttp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/xerrors"
)

// SSRFPolicy is for controlling the behavior of the downloader to prevent Server-Side Request Forgery
type SSRFPolicy struct {
	MaxURLLength          int
	MaxHeaderBytes        int
	MaxHeaderValues       int
	MaxRedirects          int
	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration

	AllowLoopbackIPs    bool
	AllowLocalHostnames bool
}

func DefaultSSRFPolicy() SSRFPolicy {
	return SSRFPolicy{
		MaxURLLength:          4096,
		MaxHeaderBytes:        16 << 10,
		MaxHeaderValues:       64,
		MaxRedirects:          3,
		DialTimeout:           20 * time.Second,
		TLSHandshakeTimeout:   20 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
}

// ValidateClientFetchURL is for early deal/proposal validation.
func ValidateClientFetchURL(raw string, headers http.Header, policy *SSRFPolicy) (*url.URL, error) {
	policy = withSSRFDefaults(policy)

	if raw == "" {
		return nil, errors.New("url is empty")
	}
	if len(raw) > policy.MaxURLLength {
		return nil, xerrors.Errorf("url exceeds %d bytes", policy.MaxURLLength)
	}
	if strings.ContainsAny(raw, "\x00\r\n\t") {
		return nil, errors.New("url contains control characters")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, xerrors.Errorf("parse url: %w", err)
	}
	if u.User != nil {
		return nil, errors.New("url userinfo is not allowed")
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, xerrors.Errorf("unsupported url scheme %q", u.Scheme)
	}
	u.Scheme = scheme

	host := u.Hostname()
	if err := validateFetchHostSyntax(host, policy); err != nil {
		return nil, err
	}

	if _, err := normalizedFetchPort(u); err != nil {
		return nil, err
	}

	// Catch unsafe IP literals early. DNS names are checked at dial time.
	if ip, err := netip.ParseAddr(host); err == nil {
		if err := validateFetchIP(ip, policy); err != nil {
			return nil, err
		}
	}

	if err := validateClientFetchHeaders(headers, policy); err != nil {
		return nil, err
	}

	return u, nil
}

// NewSSRFProtectedHTTPClient is for the actual downloader.
func NewSSRFProtectedHTTPClient(policy *SSRFPolicy, originalHeaders http.Header) (*http.Client, func() net.Conn) {
	policy = withSSRFDefaults(policy)

	var nc net.Conn
	getConn := func() net.Conn {
		return nc
	}

	tr := &http.Transport{
		Proxy:                 nil, // do not honor HTTP_PROXY/HTTPS_PROXY env
		DisableCompression:    true,
		TLSHandshakeTimeout:   policy.TLSHandshakeTimeout,
		ResponseHeaderTimeout: policy.ResponseHeaderTimeout,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			n, err := ssrfSafeDial(ctx, network, addr, policy)
			nc = n
			return n, err
		},
	}

	return &http.Client{
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= policy.MaxRedirects {
				return errors.New("too many redirects")
			}
			_, err := ValidateClientFetchURL(req.URL.String(), originalHeaders, policy)
			return err
		},
	}, getConn
}

func validateClientFetchHeaders(headers http.Header, policy *SSRFPolicy) error {
	var total int
	var values int

	for k, vals := range headers {
		if k == "" || strings.HasPrefix(k, ":") || strings.ContainsAny(k, "\r\n") {
			return xerrors.Errorf("invalid header name %q", k)
		}

		ck := http.CanonicalHeaderKey(k)
		if _, blocked := blockedClientFetchHeaders[ck]; blocked {
			return xerrors.Errorf("header %q is not allowed", ck)
		}

		values += len(vals)
		total += len(k)
		for _, v := range vals {
			if strings.ContainsAny(v, "\r\n") {
				return xerrors.Errorf("header %q contains CR/LF", ck)
			}
			total += len(v)
		}
	}

	if values > policy.MaxHeaderValues {
		return xerrors.Errorf("too many header values: %d > %d", values, policy.MaxHeaderValues)
	}
	if total > policy.MaxHeaderBytes {
		return xerrors.Errorf("headers exceed %d bytes", policy.MaxHeaderBytes)
	}

	return nil
}

var blockedClientFetchHeaders = map[string]struct{}{
	"Host":                {},
	"Connection":          {},
	"Proxy-Connection":    {},
	"Proxy-Authorization": {},
	"Forwarded":           {},
	"X-Forwarded-For":     {},
	"X-Forwarded-Host":    {},
	"X-Forwarded-Proto":   {},
	"X-Real-Ip":           {},
	"Transfer-Encoding":   {},
	"Te":                  {},
	"Trailer":             {},
	"Upgrade":             {},
	"Range":               {}, // downloader should own Range
}

func ssrfSafeDial(ctx context.Context, network, addr string, policy *SSRFPolicy) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, xerrors.Errorf("unsupported network %q", network)
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, xerrors.Errorf("split dial address: %w", err)
	}
	if err := validateFetchPort(port); err != nil {
		return nil, err
	}
	if err := validateFetchHostSyntax(host, policy); err != nil {
		return nil, err
	}

	ips, err := resolveFetchHost(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, xerrors.Errorf("host %q resolved to no addresses", host)
	}

	for _, ip := range ips {
		if err := validateFetchIP(ip, policy); err != nil {
			return nil, xerrors.Errorf("unsafe resolved address %s for %q: %w", ip, host, err)
		}
	}

	dialer := net.Dialer{Timeout: policy.DialTimeout, KeepAlive: 30 * time.Second}

	var lastErr error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, tcpNetworkForIP(ip), net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}

	return nil, lastErr
}

func resolveFetchHost(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip.Unmap()}, nil
	}

	resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, xerrors.Errorf("resolve host %q: %w", host, err)
	}

	ips := make([]netip.Addr, 0, len(resolved))
	for _, r := range resolved {
		ip, ok := netip.AddrFromSlice(r.IP)
		if !ok {
			continue
		}
		ips = append(ips, ip.Unmap())
	}
	return ips, nil
}

func validateFetchHostSyntax(host string, policy *SSRFPolicy) error {
	if host == "" {
		return errors.New("url host is empty")
	}
	if strings.Contains(host, "%") {
		return errors.New("scoped/zone identifiers are not allowed")
	}
	if strings.ContainsAny(host, " \t\r\n\x00") {
		return errors.New("host contains invalid characters")
	}
	if !isASCII(host) {
		return errors.New("non-ascii hostnames are not allowed")
	}

	h := strings.TrimSuffix(strings.ToLower(host), ".")
	if !policy.AllowLocalHostnames && (h == "localhost" || strings.HasSuffix(h, ".localhost") || h == "local" || strings.HasSuffix(h, ".local")) {
		return xerrors.Errorf("special-use hostname %q is not allowed", host)
	}

	return nil
}

func validateFetchIP(ip netip.Addr, policy *SSRFPolicy) error {
	ip = ip.Unmap()
	if !ip.IsValid() {
		return errors.New("invalid ip")
	}
	if ip.IsLoopback() {
		if policy.AllowLoopbackIPs {
			return nil
		}
		return xerrors.Errorf("non-public ip %s is not allowed", ip)
	}
	if ip.IsPrivate() {
		return xerrors.Errorf("non-public ip %s is not allowed", ip)
	}
	if ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
		return xerrors.Errorf("non-public ip %s is not allowed", ip)
	}

	for _, p := range forbiddenFetchPrefixes {
		if p.Contains(ip) {
			return xerrors.Errorf("forbidden ip range %s", p)
		}
	}

	return nil
}

var forbiddenFetchPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("255.255.255.255/32"),

	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

func normalizedFetchPort(u *url.URL) (string, error) {
	port := u.Port()
	if port == "" {
		if u.Scheme == "http" {
			return "80", nil
		}
		if u.Scheme == "https" {
			return "443", nil
		}
		return "", xerrors.Errorf("no default port for scheme %q", u.Scheme)
	}

	if err := validateFetchPort(port); err != nil {
		return "", err
	}
	return port, nil
}

func validateFetchPort(port string) error {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return xerrors.Errorf("invalid port %q", port)
	}
	return nil
}

func tcpNetworkForIP(ip netip.Addr) string {
	if ip.Is4() {
		return "tcp4"
	}
	return "tcp6"
}

func withSSRFDefaults(p *SSRFPolicy) *SSRFPolicy {
	d := DefaultSSRFPolicy()

	if p == nil {
		return &d
	}

	cfg := *p

	if cfg.MaxURLLength == 0 {
		cfg.MaxURLLength = d.MaxURLLength
	}
	if cfg.MaxHeaderBytes == 0 {
		cfg.MaxHeaderBytes = d.MaxHeaderBytes
	}
	if cfg.MaxHeaderValues == 0 {
		cfg.MaxHeaderValues = d.MaxHeaderValues
	}
	if cfg.MaxRedirects == 0 {
		cfg.MaxRedirects = d.MaxRedirects
	}
	if cfg.DialTimeout == 0 {
		cfg.DialTimeout = d.DialTimeout
	}
	if cfg.TLSHandshakeTimeout == 0 {
		cfg.TLSHandshakeTimeout = d.TLSHandshakeTimeout
	}
	if cfg.ResponseHeaderTimeout == 0 {
		cfg.ResponseHeaderTimeout = d.ResponseHeaderTimeout
	}

	return &cfg
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
}
