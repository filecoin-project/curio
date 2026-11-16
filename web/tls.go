package web

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/filecoin-project/curio/cuhttp"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var (
	selfSignedOnce sync.Once
	selfSignedCert tls.Certificate
	selfSignedErr  error
)

// guiTLSConfig builds a TLS config that always terminates TLS for the admin GUI.
// When autocert is available for HTTP.DomainName, that certificate is used for
// that SNI name; any other requested name (or missing cert) uses a self-signed cert.
func guiTLSConfig(cfg *config.CurioConfig, db *harmonydb.DB) (*tls.Config, error) {
	fallback, err := selfSignedGUICert()
	if err != nil {
		return nil, err
	}

	var manager *autocert.Manager
	domain := ""
	if cfg != nil {
		domain = strings.TrimSpace(cfg.HTTP.DomainName)
		manager = cuhttp.SharedCertManager(&cfg.HTTP, db)
	}

	tc := &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2", "http/1.1"},
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			if manager != nil && domain != "" && strings.EqualFold(hello.ServerName, domain) {
				cert, err := manager.GetCertificate(hello)
				if err == nil && cert != nil {
					return cert, nil
				}
				log.Warnf("autocert certificate for %q unavailable, using self-signed: %v", hello.ServerName, err)
			}
			return &fallback, nil
		},
	}
	return tc, nil
}

// selfSignedGUICert returns a process-wide self-signed certificate for local/IP GUI access.
func selfSignedGUICert() (tls.Certificate, error) {
	selfSignedOnce.Do(func() {
		selfSignedCert, selfSignedErr = generateSelfSignedGUICert()
	})
	return selfSignedCert, selfSignedErr
}

// generateSelfSignedGUICert creates a long-lived ECDSA cert for localhost and loopback.
func generateSelfSignedGUICert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"Curio Admin GUI"},
			CommonName:   "Curio Admin GUI",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return tls.X509KeyPair(certPEM, keyPEM)
}
