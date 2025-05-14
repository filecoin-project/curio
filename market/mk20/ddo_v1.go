package mk20

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum"
	eabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin/v16/verifreg"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var UnknowContract = errors.New("provider does not work with this market")

// DDOV1 defines a structure for handling provider, client, and piece manager information with associated contract and notification details
// for a DDO deal handling.
type DDOV1 struct {

	// Provider specifies the address of the provider
	Provider address.Address `json:"provider"`

	// Client represents the address of the deal client
	Client address.Address `json:"client"`

	// Actor able to with AuthorizeMessage (like f1/f3 wallet) able to authorize actions such as managing ACLs
	PieceManager address.Address `json:"piecemanager"`

	// Duration represents the deal duration in epochs. This value is ignored for the deal with allocationID.
	// It must be at least 518400
	Duration abi.ChainEpoch `json:"duration"`

	// AllocationId represents an aggregated allocation identifier for the deal.
	AllocationId *verifreg.AllocationId `json:"aggregatedallocationid"`

	// ContractAddress specifies the address of the contract governing the deal
	ContractAddress string `json:"contractaddress"`

	// ContractDealIDMethod specifies the method name to retrieve the deal ID for a contract
	ContractDealIDMethod string `json:"contractdealidmethod"`

	// ContractDealIDMethodParams represents encoded parameters for the contract deal ID method if required by the contract
	ContractDealIDMethodParams []byte `json:"contractdealidmethodparams"`

	// NotificationAddress specifies the address to which notifications will be relayed to when sector is activated
	NotificationAddress string `json:"notificationaddress"`

	// NotificationPayload holds the notification data typically in a serialized byte array format.
	NotificationPayload []byte `json:"notificationpayload"`

	// Indexing indicates if the deal is to be indexed in the provider's system to support CIDs based retrieval
	Indexing bool `json:"indexing"`

	// AnnounceToIPNI indicates whether the deal should be announced to the Interplanetary Network Indexer (IPNI).
	AnnounceToIPNI bool `json:"announcetoinpni"`
}

func (d *DDOV1) Validate(dbProducts []dbProduct) (int, error) {
	code, err := d.IsEnabled(dbProducts)
	if err != nil {
		return code, err
	}

	if d.Provider == address.Undef || d.Provider.Empty() {
		return ErrProductValidationFailed, xerrors.Errorf("provider address is not set")
	}

	if d.Client == address.Undef || d.Client.Empty() {
		return ErrProductValidationFailed, xerrors.Errorf("client address is not set")
	}

	if d.PieceManager == address.Undef || d.PieceManager.Empty() {
		return ErrProductValidationFailed, xerrors.Errorf("piece manager address is not set")
	}

	if d.AllocationId != nil {
		if *d.AllocationId == verifreg.NoAllocationID {
			return ErrProductValidationFailed, xerrors.Errorf("incorrect allocation id")
		}
	}

	if d.AllocationId == nil {
		if d.Duration < 518400 {
			return ErrDurationTooShort, xerrors.Errorf("duration must be at least 518400")
		}
	}

	if d.ContractAddress == "" {
		return ErrProductValidationFailed, xerrors.Errorf("contract address is not set")
	}

	if d.ContractAddress[0:2] != "0x" {
		return ErrProductValidationFailed, xerrors.Errorf("contract address must start with 0x")
	}

	if d.ContractDealIDMethodParams == nil {
		return ErrProductValidationFailed, xerrors.Errorf("contract deal id method params is not set")
	}

	if d.ContractDealIDMethod == "" {
		return ErrProductValidationFailed, xerrors.Errorf("contract deal id method is not set")
	}

	return Ok, nil
}

func (d *DDOV1) GetDealID(ctx context.Context, db *harmonydb.DB, eth *ethclient.Client) (string, int, error) {
	var abiStr string
	err := db.QueryRow(ctx, `SELECT abi FROM ddo_contracts WHERE address = $1`, d.ContractAddress).Scan(&abiStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrMarketNotEnabled, UnknowContract
		}
		return "", http.StatusInternalServerError, xerrors.Errorf("getting abi: %w", err)
	}

	parsedABI, err := eabi.JSON(strings.NewReader(abiStr))
	if err != nil {
		return "", http.StatusInternalServerError, xerrors.Errorf("parsing abi: %w", err)
	}

	to := common.HexToAddress(d.ContractAddress)

	// Get the method
	method, exists := parsedABI.Methods[d.ContractDealIDMethod]
	if !exists {
		return "", http.StatusInternalServerError, fmt.Errorf("method %s not found in ABI", d.ContractDealIDMethod)
	}

	// Enforce method must take exactly one `bytes` parameter
	if len(method.Inputs) != 1 || method.Inputs[0].Type.String() != "bytes" {
		return "", http.StatusInternalServerError, fmt.Errorf("method %q must take exactly one argument of type bytes", method.Name)
	}

	// ABI-encode method call with input
	callData, err := parsedABI.Pack(method.Name, d.ContractDealIDMethodParams)
	if err != nil {
		return "", http.StatusInternalServerError, fmt.Errorf("failed to encode call data: %w", err)
	}

	// Build call message
	msg := ethereum.CallMsg{
		To:   &to,
		Data: callData,
	}

	// Call contract
	output, err := eth.CallContract(ctx, msg, nil)
	if err != nil {
		return "", http.StatusInternalServerError, fmt.Errorf("eth_call failed: %w", err)
	}

	// Decode return value (assume string)
	var result string
	if err := parsedABI.UnpackIntoInterface(&result, method.Name, output); err != nil {
		return "", http.StatusInternalServerError, fmt.Errorf("decode result: %w", err)
	}

	if result == "" {
		return "", ErrDealRejectedByMarket, fmt.Errorf("empty result from contract")
	}

	return result, Ok, nil
}

func (d *DDOV1) ProductName() ProductName {
	return ProductNameDDOV1
}

func (d *DDOV1) IsEnabled(dbProducts []dbProduct) (int, error) {
	name := string(d.ProductName())
	for _, p := range dbProducts {
		if p.Name == name {
			if p.Enabled {
				return Ok, nil
			}
			return ErrProductNotEnabled, xerrors.Errorf("product %s is not enabled on the provider", name)
		}
	}
	return ErrUnsupportedProduct, xerrors.Errorf("product %s is not supported on the provider", name)
}
