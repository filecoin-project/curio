package webrpc

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"github.com/yugabyte/pgx/v5"
	"strings"
)

// PDPService represents a PDP service
type PDPService struct {
	ID        int64  `db:"id" json:"id"`
	Name      string `db:"service_label" json:"name"`
	PubKey    []byte `db:"pubkey"`   // Stored as bytes in DB, converted to PEM string in JSON
	PubKeyStr string `json:"pubkey"` // PEM string for JSON response
}

// PDPServices retrieves the list of PDP services from the database
func (a *WebRPC) PDPServices(ctx context.Context) ([]PDPService, error) {
	services := []PDPService{}

	// Use w.deps.DB.Select to retrieve the services
	err := a.deps.DB.Select(ctx, &services, `SELECT id, service_label, pubkey FROM pdp_services ORDER BY id ASC`)
	if err != nil {
		log.Errorf("PDPServices: failed to select services: %v", err)
		return nil, fmt.Errorf("failed to retrieve services")
	}

	// Convert pubkey bytes to PEM format string in the JSON response
	for i, svc := range services {
		pubKeyBytes := svc.PubKey

		// Encode the public key to PEM format
		block := &pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: pubKeyBytes,
		}

		pemBytes := pem.EncodeToMemory(block)
		services[i].PubKeyStr = string(pemBytes)
	}

	return services, nil
}

// AddPDPService adds a new PDP service to the database
func (a *WebRPC) AddPDPService(ctx context.Context, name string, pubKey string) error {
	name = strings.TrimSpace(name)
	pubKey = strings.TrimSpace(pubKey)

	if name == "" {
		return fmt.Errorf("service name cannot be empty")
	}
	if pubKey == "" {
		return fmt.Errorf("public key cannot be empty")
	}

	// Decode the public key from PEM format
	block, _ := pem.Decode([]byte(pubKey))
	if block == nil || block.Type != "PUBLIC KEY" {
		return fmt.Errorf("failed to parse public key PEM")
	}
	pubKeyBytes := block.Bytes

	// Validate the public key
	_, err := x509.ParsePKIXPublicKey(pubKeyBytes)
	if err != nil {
		return fmt.Errorf("invalid public key: %v", err)
	}

	// Check if a service with the same name already exists
	var existingID int64
	err = a.deps.DB.QueryRow(ctx, `SELECT id FROM pdp_services WHERE service_label = $1`, name).Scan(&existingID)
	if err == nil {
		// Service with the same name exists
		return fmt.Errorf("a service with the same name already exists")
	} else if err != pgx.ErrNoRows {
		// Some other error occurred
		log.Errorf("AddPDPService: failed to check existing service: %v", err)
		return fmt.Errorf("failed to add service")
	}

	// Insert the new PDP service into the database
	_, err = a.deps.DB.Exec(ctx, `INSERT INTO pdp_services (service_label, pubkey) VALUES ($1, $2)`, name, pubKeyBytes)
	if err != nil {
		log.Errorf("AddPDPService: failed to insert service: %v", err)
		return fmt.Errorf("failed to add service")
	}

	return nil
}

// RemovePDPService removes a PDP service from the database
func (a *WebRPC) RemovePDPService(ctx context.Context, id int64) error {
	// Optional: Authentication and Authorization checks
	// For example, check if the user is an admin

	// Check if the service exists
	var existingID int64
	err := a.deps.DB.QueryRow(ctx, `SELECT id FROM pdp_services WHERE id = $1`, id).Scan(&existingID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("service with ID %d does not exist", id)
		}
		log.Errorf("RemovePDPService: failed to check existing service: %v", err)
		return fmt.Errorf("failed to remove service")
	}

	// Delete the service
	_, err = a.deps.DB.Exec(ctx, `DELETE FROM pdp_services WHERE id = $1`, id)
	if err != nil {
		log.Errorf("RemovePDPService: failed to delete service: %v", err)
		return fmt.Errorf("failed to remove service")
	}

	return nil
}
