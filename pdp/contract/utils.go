package contract

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	mbig "math/big"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	etypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
)

var log = logging.Logger("pdp-contract")

// Standard capability keys for PDP product type (must match ServiceProviderRegistry.sol REQUIRED_PDP_KEYS Bloom filter)
const (
	CapServiceURL       = "serviceURL"
	CapMinPieceSize     = "minPieceSizeInBytes"
	CapMaxPieceSize     = "maxPieceSizeInBytes"
	CapIpniPiece        = "ipniPiece"  // Optional
	CapIpniIpfs         = "ipniIpfs"   // Optional
	CapIpniPeerID       = "ipniPeerId" // Optional, IPNI peer ID for discovery
	CapStoragePrice     = "storagePricePerTibPerDay"
	CapMinProvingPeriod = "minProvingPeriodInEpochs"
	CapLocation         = "location"
	CapPaymentToken     = "paymentTokenAddress"

	// CapIpniPeerIDDeprecated is the old key for the IPNI peer ID. It was incorrectly cased
	// and does not match the suggested key in the ServiceProviderRegistry contract. New
	// registrations and updates write both keys for compatibility. This key will be removed
	// in a future release.
	CapIpniPeerIDDeprecated = "IPNIPeerID"
)

// PDPOfferingData converts a PDPOffering-like struct to capability key-value pairs
type PDPOfferingData struct {
	ServiceURL               string
	MinPieceSizeInBytes      *mbig.Int
	MaxPieceSizeInBytes      *mbig.Int
	IpniPiece                bool
	IpniIpfs                 bool
	IpniPeerID               []byte
	StoragePricePerTibPerDay *mbig.Int
	MinProvingPeriodInEpochs *mbig.Int
	Location                 string
	PaymentTokenAddress      common.Address
}

func encodeBigIntCapability(i *mbig.Int) []byte {
	if i == nil {
		return nil
	}
	if i.Sign() == 0 {
		return []byte{0x00}
	}
	return i.Bytes()
}

func OfferingToCapabilities(offering PDPOfferingData, additionalCaps map[string]string) ([]string, [][]byte, error) {
	// Required PDP keys per REQUIRED_PDP_KEYS Bloom filter in ServiceProviderRegistry.sol
	keys := []string{
		CapServiceURL,
		CapMinPieceSize,
		CapMaxPieceSize,
		CapStoragePrice,
		CapMinProvingPeriod,
		CapLocation,
		CapPaymentToken,
	}

	values := [][]byte{
		[]byte(offering.ServiceURL),
		encodeBigIntCapability(offering.MinPieceSizeInBytes),
		encodeBigIntCapability(offering.MaxPieceSizeInBytes),
		encodeBigIntCapability(offering.StoragePricePerTibPerDay),
		encodeBigIntCapability(offering.MinProvingPeriodInEpochs),
		[]byte(offering.Location),
		offering.PaymentTokenAddress.Bytes(),
	}

	// Add optional PDP keys if enabled
	if offering.IpniPiece {
		keys = append(keys, CapIpniPiece)
		values = append(values, encodeBool(true))
	}
	if offering.IpniIpfs {
		keys = append(keys, CapIpniIpfs)
		values = append(values, encodeBool(true))
	}
	if offering.IpniIpfs || offering.IpniPiece {
		if len(offering.IpniPeerID) == 0 {
			return nil, nil, xerrors.Errorf("IpniPeerID is required if either IpniIpfs or IpniPiece is true")
		}
		// Write the correct key
		keys = append(keys, CapIpniPeerID)
		values = append(values, []byte(offering.IpniPeerID))
		// Also write the deprecated key for compatibility with older SDK versions
		keys = append(keys, CapIpniPeerIDDeprecated)
		values = append(values, []byte(offering.IpniPeerID))
	}

	// Add custom capabilities
	for k, v := range additionalCaps {
		keys = append(keys, k)
		// try hexadecimal
		if len(v)%2 == 0 && len(v) > 3 && strings.HasPrefix(v, "0x") {
			if decoded, err := hex.DecodeString(v[2:]); err == nil {
				values = append(values, decoded)
				continue
			}
		}
		values = append(values, []byte(v))
	}

	return keys, values, nil
}

func encodeBool(b bool) []byte {
	if b {
		return []byte{0x01}
	}
	return []byte{0x00}
}

// ResolveViewAddress resolves the view contract address for a service contract
// that implements viewContractAddress(). Service contracts (like FWSS) use
// separate view contracts for read-only operations that are not available on
// the service proxy itself.
// This function assumes that the service contract implements
// viewContractAddress() and therefore returns an error if the view address
// cannot be resolved.
func ResolveViewAddress(serviceAddr common.Address, ethClient *ethclient.Client) (common.Address, error) {
	svc, err := NewContractWithView(serviceAddr, ethClient)
	if err != nil {
		return common.Address{}, xerrors.Errorf("failed to bind to service at %s: %w", serviceAddr, err)
	}
	viewAddr, err := svc.ViewContractAddress(nil)
	if err != nil {
		return common.Address{}, xerrors.Errorf("failed to get view contract address: %w", err)
	}
	if viewAddr == (common.Address{}) {
		return common.Address{}, xerrors.Errorf("view contract address is zero")
	}
	return viewAddr, nil
}

// GetProvingScheduleFromListener checks if a listener has a view contract and returns
// an IPDPProvingSchedule instance bound to the appropriate address.
// It uses the view contract address if available, otherwise uses the listener address directly.
func GetProvingScheduleFromListener(listenerAddr common.Address, ethClient *ethclient.Client) (*IPDPProvingSchedule, error) {
	provingScheduleAddr := listenerAddr
	if viewAddr, err := ResolveViewAddress(listenerAddr, ethClient); err == nil {
		provingScheduleAddr = viewAddr
	} // else we'll assume that the listener contract itself implements IPDPProvingSchedule

	provingSchedule, err := NewIPDPProvingSchedule(provingScheduleAddr, ethClient)
	if err != nil {
		return nil, xerrors.Errorf("failed to create proving schedule binding: %w", err)
	}

	return provingSchedule, nil
}

func GetDataSetMetadataAtKey(listenerAddr common.Address, ethClient *ethclient.Client, dataSetId *mbig.Int, key string) (bool, string, error) {
	metadataAddr := listenerAddr
	if viewAddr, err := ResolveViewAddress(listenerAddr, ethClient); err == nil {
		metadataAddr = viewAddr
	} // else we'll still try from the listener contract just in case

	// Create a metadata service viewer.
	mDataService, err := NewListenerServiceWithMetaData(metadataAddr, ethClient)
	if err != nil {
		log.Debugw("Failed to create a meta data service from listener, returning metadata not found", "error", err)
		return false, "", nil
	}

	out, err := mDataService.GetDataSetMetadata(nil, dataSetId, key)
	if err != nil {
		return false, "", err
	}
	return out.Exists, out.Value, nil
}

func FSRegister(ctx context.Context, db *harmonydb.DB, full api.FullNode, ethClient *ethclient.Client, name, description string, pdpOffering PDPOfferingData, capabilities map[string]string) error {
	if len(name) > 128 {
		return xerrors.Errorf("name is too long, max 128 characters allowed")
	}

	if name == "" {
		return xerrors.Errorf("name is required")
	}

	if len(description) > 128 {
		return xerrors.Errorf("description is too long, max 128 characters allowed")
	}

	// Convert PDPOffering to capability keys/values
	keys, values, err := OfferingToCapabilities(pdpOffering, capabilities)
	if err != nil {
		return xerrors.Errorf("failed to convert offering to capabilities: %w", err)
	}

	// Validate capabilities
	for _, k := range keys {
		if len(k) > 32 {
			return xerrors.Errorf("capabilities key %s is too long, max 32 characters allowed", k)
		}
	}
	for _, v := range values {
		if len(v) > 128 {
			return xerrors.Errorf("capabilities value is too long, max 128 bytes allowed")
		}
	}
	if len(keys) > 32 {
		return xerrors.Errorf("too many capabilities, max 32 allowed")
	}

	sender, fSender, privateKey, err := getSender(ctx, db)
	if err != nil {
		return xerrors.Errorf("failed to get sender: %w", err)
	}

	ac, err := full.StateGetActor(ctx, fSender, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("failed to get actor: %w", err)
	}

	amount, err := types.ParseFIL("5 FIL")
	if err != nil {
		return fmt.Errorf("failed to parse 5 FIL: %w", err)
	}

	token := abi.NewTokenAmount(amount.Int64())

	if ac.Balance.LessThan(big.NewInt(token.Int64())) {
		return xerrors.Errorf("wallet balance is too low")
	}

	walletEvm, err := ethtypes.EthAddressFromFilecoinAddress(fSender)
	if err != nil {
		return xerrors.Errorf("failed to convert wallet address to Eth address: %w", err)
	}

	contractAddr, err := ServiceRegistryAddress()
	if err != nil {
		return xerrors.Errorf("failed to get service registry address: %w", err)
	}

	srAbi, err := ServiceProviderRegistryMetaData.GetAbi()
	if err != nil {
		return xerrors.Errorf("failed to get service registry ABI: %w", err)
	}

	// Prepare EVM calldata - registerProvider(address payee, string name, string description, ProductType productType, string[] capabilityKeys, bytes[] capabilityValues)
	calldata, err := srAbi.Pack("registerProvider", common.Address(walletEvm), name, description, uint8(0), keys, values)
	if err != nil {
		return fmt.Errorf("failed to serialize parameters for registerProvider: %w", err)
	}

	signedTx, err := createSignedTransaction(ctx, ethClient, privateKey, sender, contractAddr, amount.Int, calldata)
	if err != nil {
		return xerrors.Errorf("creating signed transaction: %w", err)
	}

	err = ethClient.SendTransaction(ctx, signedTx)
	if err != nil {
		return xerrors.Errorf("sending transaction: %w", err)
	}

	log.Infof("Sent Register Service Provider transaction %s at %s", signedTx.Hash().String(), time.Now().Format(time.RFC3339Nano))
	return nil
}

func getSender(ctx context.Context, db *harmonydb.DB) (common.Address, address.Address, *ecdsa.PrivateKey, error) {
	// Fetch the private key from the database
	var privateKeyData []byte
	err := db.QueryRow(ctx,
		`SELECT private_key FROM eth_keys WHERE role = 'pdp'`).Scan(&privateKeyData)
	if err != nil {
		return common.Address{}, address.Address{}, nil, xerrors.Errorf("fetching pdp private key from db: %w", err)
	}

	privateKey, err := crypto.ToECDSA(privateKeyData)
	if err != nil {
		return common.Address{}, address.Address{}, nil, xerrors.Errorf("converting private key: %w", err)
	}

	sender := crypto.PubkeyToAddress(privateKey.PublicKey)

	fSender, err := address.NewDelegatedAddress(builtin.EthereumAddressManagerActorID, sender.Bytes())
	if err != nil {
		return common.Address{}, address.Address{}, nil, xerrors.Errorf("failed to create delegated address: %w", err)
	}

	return sender, fSender, privateKey, nil
}

func createSignedTransaction(ctx context.Context, ethClient *ethclient.Client, privateKey *ecdsa.PrivateKey, from, to common.Address, amount *mbig.Int, data []byte) (*etypes.Transaction, error) {
	msg := ethereum.CallMsg{
		From:  from,
		To:    &to,
		Value: amount,
		Data:  data,
	}

	gasLimit, err := ethClient.EstimateGas(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("failed to estimate gas: %w", err)
	}
	if gasLimit == 0 {
		return nil, fmt.Errorf("estimated gas limit is zero")
	}

	// Fetch current base fee
	header, err := ethClient.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest block header: %w", err)
	}

	baseFee := header.BaseFee
	if baseFee == nil {
		return nil, fmt.Errorf("base fee not available; network might not support EIP-1559")
	}

	// Set GasTipCap (maxPriorityFeePerGas)
	gasTipCap, err := ethClient.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, xerrors.Errorf("estimating gas premium: %w", err)
	}

	// Calculate GasFeeCap (maxFeePerGas)
	gasFeeCap := big.NewInt(0).Add(baseFee, gasTipCap)

	chainID, err := ethClient.NetworkID(ctx)
	if err != nil {
		return nil, xerrors.Errorf("getting network ID: %w", err)
	}

	pendingNonce, err := ethClient.PendingNonceAt(ctx, from)
	if err != nil {
		return nil, xerrors.Errorf("getting pending nonce: %w", err)
	}

	// Create a new transaction with estimated gas limit and fee caps
	tx := etypes.NewTx(&etypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     pendingNonce,
		GasFeeCap: gasFeeCap,
		GasTipCap: gasTipCap,
		Gas:       gasLimit,
		To:        &to,
		Value:     amount,
		Data:      data,
	})

	// Sign the transaction
	signer := etypes.LatestSignerForChainID(chainID)
	signedTx, err := etypes.SignTx(tx, signer, privateKey)
	if err != nil {
		return nil, xerrors.Errorf("signing transaction: %w", err)
	}

	return signedTx, nil
}

func FSUpdateProvider(ctx context.Context, name, description string, db *harmonydb.DB, ethClient *ethclient.Client) (string, error) {
	if len(name) > 128 {
		return "", xerrors.Errorf("name is too long, max 128 characters allowed")
	}

	if name == "" {
		return "", xerrors.Errorf("name is required")
	}

	if len(description) > 128 {
		return "", xerrors.Errorf("description is too long, max 128 characters allowed")
	}

	sender, _, privateKey, err := getSender(ctx, db)
	if err != nil {
		return "", xerrors.Errorf("failed to get sender: %w", err)
	}

	contractAddr, err := ServiceRegistryAddress()
	if err != nil {
		return "", xerrors.Errorf("failed to get service registry address: %w", err)
	}

	srAbi, err := ServiceProviderRegistryMetaData.GetAbi()
	if err != nil {
		return "", xerrors.Errorf("failed to get service registry ABI: %w", err)
	}

	calldata, err := srAbi.Pack("updateProviderInfo", name, description)
	if err != nil {
		return "", xerrors.Errorf("failed to serialize parameters for updateProviderInfo: %w", err)
	}

	signedTx, err := createSignedTransaction(ctx, ethClient, privateKey, sender, contractAddr, mbig.NewInt(0), calldata)
	if err != nil {
		return "", xerrors.Errorf("creating signed transaction: %w", err)
	}

	err = ethClient.SendTransaction(ctx, signedTx)
	if err != nil {
		return "", xerrors.Errorf("sending transaction: %w", err)
	}

	return signedTx.Hash().String(), nil
}

func FSUpdatePDPService(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client, pdpOffering PDPOfferingData, capabilities map[string]string) (string, error) {
	// Convert PDPOffering to capability keys/values
	keys, values, err := OfferingToCapabilities(pdpOffering, capabilities)
	if err != nil {
		return "", xerrors.Errorf("failed to convert offering to capabilities: %w", err)
	}

	// Validate capabilities
	for _, k := range keys {
		if len(k) > 32 {
			return "", xerrors.Errorf("capabilities key %s is too long, max 32 characters allowed", k)
		}
	}
	for _, v := range values {
		if len(v) > 128 {
			return "", xerrors.Errorf("capabilities value is too long, max 128 bytes allowed")
		}
	}
	if len(keys) > 32 {
		return "", xerrors.Errorf("too many capabilities, max 32 allowed")
	}

	sender, _, privateKey, err := getSender(ctx, db)
	if err != nil {
		return "", xerrors.Errorf("failed to get sender: %w", err)
	}

	contractAddr, err := ServiceRegistryAddress()
	if err != nil {
		return "", xerrors.Errorf("failed to get service registry address: %w", err)
	}

	srAbi, err := ServiceProviderRegistryMetaData.GetAbi()
	if err != nil {
		return "", xerrors.Errorf("failed to get service registry ABI: %w", err)
	}

	// Call updateProduct instead of updatePDPServiceWithCapabilities
	calldata, err := srAbi.Pack("updateProduct", uint8(0), keys, values)
	if err != nil {
		return "", xerrors.Errorf("failed to serialize parameters for updateProduct: %w", err)
	}

	signedTx, err := createSignedTransaction(ctx, ethClient, privateKey, sender, contractAddr, mbig.NewInt(0), calldata)
	if err != nil {
		return "", xerrors.Errorf("creating signed transaction: %w", err)
	}

	err = ethClient.SendTransaction(ctx, signedTx)
	if err != nil {
		return "", xerrors.Errorf("sending transaction: %w", err)
	}

	return signedTx.Hash().String(), nil
}

func FSDeregisterProvider(ctx context.Context, db *harmonydb.DB, ethClient *ethclient.Client) (string, error) {
	sender, _, privateKey, err := getSender(ctx, db)
	if err != nil {
		return "", xerrors.Errorf("failed to get sender: %w", err)
	}

	contractAddr, err := ServiceRegistryAddress()
	if err != nil {
		return "", xerrors.Errorf("failed to get service registry address: %w", err)
	}

	srAbi, err := ServiceProviderRegistryMetaData.GetAbi()
	if err != nil {
		return "", xerrors.Errorf("failed to get service registry ABI: %w", err)
	}

	calldata, err := srAbi.Pack("removeProvider")
	if err != nil {
		return "", xerrors.Errorf("failed to serialize parameters for removeProvider: %w", err)
	}

	signedTx, err := createSignedTransaction(ctx, ethClient, privateKey, sender, contractAddr, mbig.NewInt(0), calldata)
	if err != nil {
		return "", xerrors.Errorf("creating signed transaction: %w", err)
	}

	err = ethClient.SendTransaction(ctx, signedTx)
	if err != nil {
		return "", xerrors.Errorf("sending transaction: %w", err)
	}

	return signedTx.Hash().String(), nil
}

func DecodeAddressCapability(input []byte) common.Address {
	// If input is longer than 32 bytes → return zero
	if len(input) > 32 {
		return common.Address{}
	}

	// 32-byte big-endian buffer
	var buf [32]byte

	if len(input) == 32 {
		// Exact fit
		copy(buf[:], input)
	} else {
		// Left pad if shorter
		copy(buf[32-len(input):], input)
	}

	// Lowest 20 bytes are the address
	return common.BytesToAddress(buf[12:])
}

// ShouldHexEncodeCapability reports whether a capability value needs hex-encoding
// to safely round-trip through JSON and browser input fields. Returns true for
// invalid UTF-8 or control characters; false for valid text (including CJK/emoji).
// See https://pkg.go.dev/unicode/utf8#DecodeRune for RuneError semantics.
func ShouldHexEncodeCapability(b []byte) bool {
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size == 1 { // invalid UTF-8
			return true
		}
		if unicode.IsControl(r) {
			return true
		}
		i += size
	}
	return false
}

// EncodeCapabilityForDisplay returns a display string for a capability value.
// Binary data gets "0x" hex prefix; valid UTF-8 text passes through as-is.
// Pairs with hex-decoding in OfferingToCapabilities.
func EncodeCapabilityForDisplay(b []byte) string {
	if ShouldHexEncodeCapability(b) {
		return "0x" + hex.EncodeToString(b)
	}
	return string(b)
}
