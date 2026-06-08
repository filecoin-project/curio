package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// Signer abstracts the signature operation (secp256k1, bls, delegated).
type Signer interface {
	// Sign signs the supplied digest and returns raw signature bytes.
	Sign(digest []byte) ([]byte, error)
	// PublicKeyBytes returns the raw public‑key bytes (no multibase / address).
	PublicKeyBytes() []byte
	// Type returns a short string identifying the key algorithm.
	Type() string
}

// CurioAuthHeader returns a HTTPClient Option that injects "CurioAuth ..."
// on every request using the algorithm defined in the OpenAPI spec.
func CurioAuthHeader(s Signer) Option {
	return WithAuth(func(_ context.Context, requestMethod string, requestPath string) (string, string, error) {
		digest := AuthDigest(s.PublicKeyBytes(), requestMethod, requestPath, time.Now())

		sig, err := s.Sign(digest[:])
		if err != nil {
			return "", "", err
		}

		header := fmt.Sprintf("CurioAuth %s:%s:%s",
			s.Type(),
			base64.StdEncoding.EncodeToString(s.PublicKeyBytes()),
			base64.StdEncoding.EncodeToString(sig),
		)
		return "Authorization", header, nil
	})
}

// AuthDigest returns the SHA-256 digest that must be signed for CurioAuth.
//
// The message format must match the mk20 server-side verifier:
// address bytes + uppercase HTTP method + escaped request path + RFC3339 minute timestamp.
func AuthDigest(pubKey []byte, requestMethod string, requestPath string, timestamp time.Time) [32]byte {
	requestMethod = strings.ToUpper(requestMethod)
	if requestPath == "" {
		requestPath = "/"
	}

	return sha256.Sum256(bytes.Join([][]byte{
		pubKey,
		[]byte(requestMethod),
		[]byte(requestPath),
		[]byte(timestamp.UTC().Truncate(time.Minute).Format(time.RFC3339)),
	}, []byte{}))
}
