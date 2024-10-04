package webrpc

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	xerrors "golang.org/x/xerrors"
	"strings"

	"github.com/yugabyte/pgx/v5"
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

// PDPOwnerAddress represents an owner address entry in the pdp_owner_addresses table
type PDPOwnerAddress struct {
	OwnerAddress string `db:"owner_address" json:"owner_address"`
}

// ImportPDPKey imports an Ethereum private key into the pdp_owner_addresses table.
// It accepts the key in hex format, writes it to the table with the corresponding
// owner_address, and returns the address string.
func (a *WebRPC) ImportPDPKey(ctx context.Context, hexPrivateKey string) (string, error) {
	hexPrivateKey = strings.TrimSpace(hexPrivateKey)
	if hexPrivateKey == "" {
		return "", fmt.Errorf("private key cannot be empty")
	}

	// Remove any leading '0x' from the hex string
	hexPrivateKey = strings.TrimPrefix(hexPrivateKey, "0x")
	hexPrivateKey = strings.TrimPrefix(hexPrivateKey, "0X")

	// Decode the hex private key
	privateKeyBytes, err := hex.DecodeString(hexPrivateKey)
	if err != nil {
		return "", fmt.Errorf("failed to decode private key: %v", err)
	}

	// Parse the private key
	privateKey, err := crypto.ToECDSA(privateKeyBytes)
	if err != nil {
		return "", fmt.Errorf("invalid private key: %v", err)
	}

	// Get the public key
	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return "", xerrors.New("error casting public key to ECDSA")
	}

	// Derive the address
	address := crypto.PubkeyToAddress(*publicKeyECDSA).Hex()

	// Insert into the database within a transaction
	_, err = a.deps.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (bool, error) {
		// Check if the owner_address already exists
		var existingAddress string
		err := tx.QueryRow(`SELECT owner_address FROM pdp_owner_addresses WHERE owner_address = $1`, address).Scan(&existingAddress)
		if err == nil {
			return false, fmt.Errorf("owner address %s already exists", address)
		} else if err != pgx.ErrNoRows {
			return false, fmt.Errorf("failed to check existing owner address: %v", err)
		}

		// Insert the new owner address and private key
		_, err = tx.Exec(`INSERT INTO pdp_owner_addresses (owner_address, private_key) VALUES ($1, $2)`, address, privateKeyBytes)
		if err != nil {
			return false, fmt.Errorf("failed to insert owner address: %v", err)
		}
		return true, nil
	})
	if err != nil {
		log.Errorf("ImportPDPKey: failed to import key: %v", err)
		return "", fmt.Errorf("failed to import key")
	}

	return address, nil
}

// ListPDPKeys retrieves the list of owner addresses from the pdp_owner_addresses table
func (a *WebRPC) ListPDPKeys(ctx context.Context) ([]string, error) {
	addresses := []string{}

	// Use a.deps.DB.Select to retrieve the owner addresses
	err := a.deps.DB.Select(ctx, &addresses, `SELECT owner_address FROM pdp_owner_addresses ORDER BY owner_address ASC`)
	if err != nil {
		log.Errorf("ListPDPKeys: failed to select addresses: %v", err)
		return nil, fmt.Errorf("failed to retrieve addresses")
	}

	return addresses, nil
}

// RemovePDPKey removes an owner address and its associated private key from the pdp_owner_addresses table
func (a *WebRPC) RemovePDPKey(ctx context.Context, ownerAddress string) error {
	ownerAddress = strings.TrimSpace(ownerAddress)
	if ownerAddress == "" {
		return fmt.Errorf("owner address cannot be empty")
	}

	// Check if the owner address exists
	var existingAddress string
	err := a.deps.DB.QueryRow(ctx, `SELECT owner_address FROM pdp_owner_addresses WHERE owner_address = $1`, ownerAddress).Scan(&existingAddress)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("owner address %s does not exist", ownerAddress)
		}
		log.Errorf("RemovePDPKey: failed to check existing owner address: %v", err)
		return fmt.Errorf("failed to remove key")
	}

	// Delete the key
	_, err = a.deps.DB.Exec(ctx, `DELETE FROM pdp_owner_addresses WHERE owner_address = $1`, ownerAddress)
	if err != nil {
		log.Errorf("RemovePDPKey: failed to delete key: %v", err)
		return fmt.Errorf("failed to remove key")
	}

	return nil
}
