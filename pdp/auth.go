package pdp

import (
	"crypto/ecdsa"
	"crypto/x509"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v4"
)

// verifyJWTToken extracts and verifies the JWT token from the request and returns the serviceID.
func (p *PDPService) verifyJWTToken(r *http.Request) (int64, error) {
	// Get the Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return 0, fmt.Errorf("missing Authorization header")
	}

	// Extract the token from the header
	bearerTokenPrefix := "Bearer "
	if !strings.HasPrefix(authHeader, bearerTokenPrefix) {
		return 0, fmt.Errorf("invalid Authorization header format")
	}
	tokenString := strings.TrimSpace(strings.TrimPrefix(authHeader, bearerTokenPrefix))
	if tokenString == "" {
		return 0, fmt.Errorf("empty token")
	}

	// Variable to capture serviceID
	var serviceID int64

	// Parse and verify the JWT token
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Ensure the signing method is ECDSA
		if _, ok := token.Method.(*jwt.SigningMethodECDSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// Extract the service ID from the token claims
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			return nil, fmt.Errorf("invalid token claims")
		}

		// Extract service_id from claims
		serviceID, ok := claims["service_name"]
		if !ok {
			return nil, fmt.Errorf("missing service_name claim")
		}

		// Query the database for the public key using serviceID
		var pubKeyBytes []byte
		ctx := r.Context()
		err := p.db.QueryRow(ctx, `
            SELECT pubkey FROM pdp_services WHERE service_label=$1
        `, serviceID).Scan(&pubKeyBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve public key for service_id %d: %v", serviceID, err)
		}

		// Parse the public key
		pubKeyInterface, err := x509.ParsePKIXPublicKey(pubKeyBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse public key: %v", err)
		}
		pubKey, ok := pubKeyInterface.(*ecdsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("public key is not ECDSA")
		}

		// Return the public key for signature verification
		return pubKey, nil
	})
	if err != nil {
		return 0, err
	}
	if !token.Valid {
		return 0, fmt.Errorf("invalid token")
	}

	return serviceID, nil
}
