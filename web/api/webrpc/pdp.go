package webrpc

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/filecoin-project/curio/pdp/contract"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
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

type PDPOwnerAddress struct {
	Address string `db:"address" json:"address"`
}

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
		var existingAddress bool

		err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM eth_keys WHERE role = 'pdp')`).Scan(&existingAddress)
		if err != nil {
			return false, xerrors.Errorf("failed to check existing owner address: %v", err)
		}
		if existingAddress {
			return false, fmt.Errorf("owner address %s already exists", address)
		}

		// Insert the new owner address and private key
		_, err = tx.Exec(`INSERT INTO eth_keys (address, private_key, role) VALUES ($1, $2, 'pdp')`, address, privateKeyBytes)
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

func (a *WebRPC) ListPDPKeys(ctx context.Context) ([]string, error) {
	addresses := []string{}

	// Use a.deps.DB.Select to retrieve the owner addresses
	err := a.deps.DB.Select(ctx, &addresses, `SELECT address FROM eth_keys WHERE role = 'pdp' ORDER BY address ASC`)
	if err != nil {
		log.Errorf("ListPDPKeys: failed to select addresses: %v", err)
		return nil, fmt.Errorf("failed to retrieve addresses")
	}

	return addresses, nil
}

func (a *WebRPC) RemovePDPKey(ctx context.Context, ownerAddress string) error {
	ownerAddress = strings.TrimSpace(ownerAddress)
	if ownerAddress == "" {
		return fmt.Errorf("owner address cannot be empty")
	}

	// Check if the owner address exists
	var existingAddress string
	err := a.deps.DB.QueryRow(ctx, `SELECT address FROM eth_keys WHERE address = $1 AND role = 'pdp'`, ownerAddress).Scan(&existingAddress)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("owner address %s does not exist", ownerAddress)
		}
		log.Errorf("RemovePDPKey: failed to check existing owner address: %v", err)
		return fmt.Errorf("failed to remove key")
	}

	// Delete the key
	_, err = a.deps.DB.Exec(ctx, `DELETE FROM eth_keys WHERE address = $1 AND role = 'pdp'`, ownerAddress)
	if err != nil {
		log.Errorf("RemovePDPKey: failed to delete key: %v", err)
		return fmt.Errorf("failed to remove key")
	}

	return nil
}

type FSRegistryStatus struct {
	Address                    string            `json:"address"`
	ID                         int64             `json:"id"`
	Active                     bool              `json:"status"`
	Name                       string            `json:"name"`
	Description                string            `json:"description"`
	Payee                      string            `json:"payee"`
	ServiceURL                 string            `json:"service_url"`
	MinPieceSizeInBytes        int64             `json:"min_size"`
	MaxPieceSizeInBytes        int64             `json:"max_size"`
	IpniPiece                  bool              `json:"ipni_piece"`
	IpniIpfs                   bool              `json:"ipni_ipfs"`
	StoragePricePerTibPerMonth int64             `json:"price"`
	MinProvingPeriodInEpochs   int64             `json:"min_proving_period"`
	Location                   string            `json:"location"`
	Capabilities               map[string]string `json:"capabilities"`
}

func (a *WebRPC) FSRegistryStatus(ctx context.Context) (*FSRegistryStatus, error) {
	var existingAddress string
	err := a.deps.DB.QueryRow(ctx, `SELECT address FROM eth_keys WHERE role = 'pdp'`).Scan(&existingAddress)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("no PDP key found")
		}
		return nil, fmt.Errorf("failed to retrieve PDP key")
	}

	eclient, err := a.deps.EthClient.Val()
	if err != nil {
		return nil, fmt.Errorf("failed to get eth client: %w", err)
	}

	registryAddr, err := contract.ServiceRegistryAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to get service registry address: %w", err)
	}

	registry, err := contract.NewServiceProviderRegistry(registryAddr, eclient)
	if err != nil {
		return nil, fmt.Errorf("failed to create service registry: %w", err)
	}

	registered, err := registry.IsRegisteredProvider(&bind.CallOpts{Context: ctx}, common.HexToAddress(existingAddress))
	if err != nil {
		return nil, fmt.Errorf("failed to check if provider is registered: %w", err)
	}

	if !registered {
		return nil, nil
	}

	provider, err := registry.GetProviderByAddress(&bind.CallOpts{Context: ctx}, common.HexToAddress(existingAddress))
	if err != nil {
		return nil, fmt.Errorf("failed to get provider: %w", err)
	}

	pdpOffering, err := registry.GetPDPService(&bind.CallOpts{Context: ctx}, provider.ProviderId)
	if err != nil {
		return nil, fmt.Errorf("failed to get PDP offering: %w", err)
	}

	capabilityValues, err := registry.GetProductCapabilities(&bind.CallOpts{Context: ctx}, provider.ProviderId, uint8(0), pdpOffering.CapabilityKeys)
	if err != nil {
		return nil, fmt.Errorf("failed to get capability values: %w", err)
	}

	capabilities := make(map[string]string)
	for i := range pdpOffering.CapabilityKeys {
		if capabilityValues.Exists[i] {
			capabilities[pdpOffering.CapabilityKeys[i]] = capabilityValues.Values[i]
		} else {
			capabilities[pdpOffering.CapabilityKeys[i]] = ""
		}
	}

	return &FSRegistryStatus{
		Address:                    existingAddress,
		ID:                         provider.ProviderId.Int64(),
		Active:                     provider.IsActive,
		Payee:                      provider.Payee.String(),
		Name:                       provider.Name,
		Description:                provider.Description,
		ServiceURL:                 pdpOffering.PdpOffering.ServiceURL,
		MinPieceSizeInBytes:        pdpOffering.PdpOffering.MinPieceSizeInBytes.Int64(),
		MaxPieceSizeInBytes:        pdpOffering.PdpOffering.MaxPieceSizeInBytes.Int64(),
		IpniPiece:                  pdpOffering.PdpOffering.IpniPiece,
		IpniIpfs:                   pdpOffering.PdpOffering.IpniIpfs,
		StoragePricePerTibPerMonth: pdpOffering.PdpOffering.StoragePricePerTibPerMonth.Int64(),
		MinProvingPeriodInEpochs:   pdpOffering.PdpOffering.MinProvingPeriodInEpochs.Int64(),
		Location:                   pdpOffering.PdpOffering.Location,
		Capabilities:               capabilities,
	}, nil
}
