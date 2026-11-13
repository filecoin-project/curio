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

// AuthService verifies the request's bearer JWT and returns its service_name claim.
func (a *JWTAuth) AuthService(r *http.Request) (string, error) {
	tokenString, err := bearerToken(r)
	if err != nil {
		return "", err
	}

	// Variable to capture serviceID
	var service string

	// Parse and verify the JWT token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
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

		// Extract the service label used to look up the registered public key.
		serviceIf, ok := claims["service_name"]
		if !ok {
			return nil, fmt.Errorf("missing service_name claim")
		}

		service, ok = serviceIf.(string)
		if !ok {
			return nil, fmt.Errorf("invalid service_name claim")
		}

		// Query the database for the public key using the service label.
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

// bearerToken returns the first non-empty Bearer token on r.
// Each Authorization field is checked. A field with another scheme is skipped.
func bearerToken(r *http.Request) (string, error) {
	const bearerTokenPrefix = "Bearer "
	sawAuth := false
	sawEmptyBearer := false
	for _, authHeader := range r.Header.Values("Authorization") {
		if authHeader == "" {
			continue
		}
		sawAuth = true
		if !strings.HasPrefix(authHeader, bearerTokenPrefix) {
			continue
		}
		tokenString := strings.TrimSpace(strings.TrimPrefix(authHeader, bearerTokenPrefix))
		if tokenString == "" {
			sawEmptyBearer = true
			continue
		}
		return tokenString, nil
	}
	if sawEmptyBearer {
		return "", fmt.Errorf("empty token")
	}
	if !sawAuth {
		return "", fmt.Errorf("missing Authorization header")
	}
	return "", fmt.Errorf("invalid Authorization header format")
}
