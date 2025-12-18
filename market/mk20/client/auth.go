package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"
)

// Signer abstracts the signature operation (ed25519, secp256k1, …).
type Signer interface {
	// Sign signs the supplied digest and returns raw signature bytes.
	Sign(digest []byte) ([]byte, error)
	// PublicKeyBytes returns the raw public‑key bytes (no multibase / address).
	PublicKeyBytes() []byte
	// Type returns a short string identifying the key algorithm ("ed25519", …).
	Type() string
}

// HourlyCurioAuthHeader returns a HTTPClient Option that injects “CurioAuth …”
// on every request using the algorithm defined in the OpenAPI spec.
func HourlyCurioAuthHeader(s Signer) Option {
	return WithAuth(func(_ context.Context) (string, string, error) {
		now := time.Now().UTC().Truncate(time.Hour)
		msg := bytes.Join([][]byte{s.PublicKeyBytes(), []byte(now.Format(time.RFC3339))}, []byte{})
		digest := sha256.Sum256(msg)

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
