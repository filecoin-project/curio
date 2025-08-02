package pdp

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/x509"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v4"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

type Auth interface {
	AuthService(r *http.Request) (string, error)
}

type NullAuth struct{}

var _ Auth = (*NullAuth)(nil)

func (a *NullAuth) AuthService(r *http.Request) (string, error) {
	return "public", nil
}

type JWTAuth struct {
	db *harmonydb.DB
}

var _ Auth = (*JWTAuth)(nil)

// JWTAuth extracts and verifies the JWT token from the request and returns the serviceID.
func (a *JWTAuth) AuthService(r *http.Request) (string, error) {
	// Get the Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", fmt.Errorf("missing Authorization header")
	}

	// Extract the token from the header
	bearerTokenPrefix := "Bearer "
	if !strings.HasPrefix(authHeader, bearerTokenPrefix) {
		return "", fmt.Errorf("invalid Authorization header format")
	}
	tokenString := strings.TrimSpace(strings.TrimPrefix(authHeader, bearerTokenPrefix))
	if tokenString == "" {
		return "", fmt.Errorf("empty token")
	}

	// Variable to capture serviceID
	var service string

	// Parse and verify the JWT token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		var isEd25519 bool
		// Ensure the signing method is ECDSA or EDDSA
		if _, ok := token.Method.(*jwt.SigningMethodECDSA); !ok {
			if _, ok := token.Method.(*jwt.SigningMethodEd25519); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			isEd25519 = true
		}

		// Extract the service ID from the token claims
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			return nil, fmt.Errorf("invalid token claims")
		}

		// Extract service_id from claims
		serviceIf, ok := claims["service_name"]
		if !ok {
			return nil, fmt.Errorf("missing service_name claim")
		}

		service, ok = serviceIf.(string)
		if !ok {
			return nil, fmt.Errorf("invalid service_name claim")
		}

		// Query the database for the public key using serviceID
		var pubKeyBytes []byte
		ctx := r.Context()
		err := a.db.QueryRow(ctx, `
            SELECT pubkey FROM pdp_services WHERE service_label=$1
        `, service).Scan(&pubKeyBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve public key for service_id %s: %v", service, err)
		}

		// Parse the public key
		pubKeyInterface, err := x509.ParsePKIXPublicKey(pubKeyBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse public key: %v", err)
		}

		if isEd25519 {
			pubKey, ok := pubKeyInterface.(ed25519.PublicKey)
			if !ok {
				return nil, fmt.Errorf("public key is not ED25519")
			}

			// Return the public key for signature verification
			return pubKey, nil
		}

		pubKey, ok := pubKeyInterface.(*ecdsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("public key is not ECDSA")
		}

		// Return the public key for signature verification
		return pubKey, nil
	})
	if err != nil {
		return "", err
	}
	if !token.Valid {
		return "", fmt.Errorf("invalid token")
	}

	return service, nil
}
