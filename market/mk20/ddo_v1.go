package mk20

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	eabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/samber/lo"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin/v16/verifreg"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
)

var ErrUnknowContract = errors.New("provider does not work with this market")

// DDOV1 defines a structure for handling provider, client, and piece manager information with associated contract and notification details
// for a DDO deal handling.
type DDOV1 struct {

	// Provider specifies the address of the provider
	Provider address.Address `json:"provider"`

	// Actor providing AuthorizeMessage (like f1/f3 wallet) able to authorize actions such as managing ACLs
	PieceManager address.Address `json:"piece_manager"`

	// Duration represents the deal duration in epochs. This value is ignored for the deal with allocationID.
	// It must be at least 518400
	Duration abi.ChainEpoch `json:"duration"`

	// AllocationId represents an allocation identifier for the deal.
	AllocationId *verifreg.AllocationId `json:"allocation_id,omitempty"`

	// ContractAddress specifies the address of the contract governing the deal
	ContractAddress string `json:"contract_address"`

	// ContractDealIDMethod specifies the method name to verify the deal and retrieve the deal ID for a contract
	ContractVerifyMethod string `json:"contract_verify_method"`

	// ContractDealIDMethodParams represents encoded parameters for the contract verify method if required by the contract
	ContractVerifyMethodParams []byte `json:"contract_verify_method_Params,omitempty"`

	// NotificationAddress specifies the address to which notifications will be relayed to when sector is activated
	NotificationAddress string `json:"notification_address"`

	// NotificationPayload holds the notification data typically in a serialized byte array format.
	NotificationPayload []byte `json:"notification_payload,omitempty"`
}

func (d *DDOV1) Validate(db *harmonydb.DB, cfg *config.MK20Config) (DealCode, error) {
	code, err := IsProductEnabled(db, d.ProductName())
	if err != nil {
		return code, err
	}

	if d.Provider == address.Undef || d.Provider.Empty() {
		return ErrProductValidationFailed, xerrors.Errorf("provider address is not set")
	}

	var mk20disabledMiners []address.Address
	for _, m := range cfg.DisabledMiners {
		maddr, err := address.NewFromString(m)
		if err != nil {
			return ErrServerInternalError, xerrors.Errorf("failed to parse miner string: %s", err)
		}
		mk20disabledMiners = append(mk20disabledMiners, maddr)
	}

	if lo.Contains(mk20disabledMiners, d.Provider) {
		return ErrProductValidationFailed, xerrors.Errorf("provider is disabled")
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

	if d.ContractVerifyMethodParams == nil {
		return ErrProductValidationFailed, xerrors.Errorf("contract verify method params is not set")
	}

	if d.ContractVerifyMethod == "" {
		return ErrProductValidationFailed, xerrors.Errorf("contract verify method is not set")
	}

	return Ok, nil
}

func (d *DDOV1) GetDealID(ctx context.Context, db *harmonydb.DB, eth *ethclient.Client) (int64, DealCode, error) {
	if d.ContractAddress == "0xtest" {
		v, err := rand.Int(rand.Reader, big.NewInt(10000000))
		if err != nil {
			return -1, ErrServerInternalError, xerrors.Errorf("failed to generate random number: %w", err)
		}
		return v.Int64(), Ok, nil
	}

	var abiStr string
	err := db.QueryRow(ctx, `SELECT abi FROM ddo_contracts WHERE address = $1`, d.ContractAddress).Scan(&abiStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return -1, ErrMarketNotEnabled, ErrUnknowContract
		}
		return -1, ErrServerInternalError, xerrors.Errorf("getting abi: %w", err)
	}

	parsedABI, err := eabi.JSON(strings.NewReader(abiStr))
	if err != nil {
		return -1, ErrServerInternalError, xerrors.Errorf("parsing abi: %w", err)
	}

	to := common.HexToAddress(d.ContractAddress)

	// Get the method
	method, exists := parsedABI.Methods[d.ContractVerifyMethod]
	if !exists {
		return -1, ErrServerInternalError, fmt.Errorf("method %s not found in ABI", d.ContractVerifyMethod)
	}

	// Enforce method must take exactly one `bytes` parameter
	if len(method.Inputs) != 1 || method.Inputs[0].Type.String() != "bytes" {
		return -1, ErrServerInternalError, fmt.Errorf("method %q must take exactly one argument of type bytes", method.Name)
	}

	// ABI-encode method call with input
	callData, err := parsedABI.Pack(method.Name, d.ContractVerifyMethod)
	if err != nil {
		return -1, ErrServerInternalError, fmt.Errorf("failed to encode call data: %w", err)
	}

	// Build call message
	msg := ethereum.CallMsg{
		To:   &to,
		Data: callData,
	}

	// Call contract
	output, err := eth.CallContract(ctx, msg, nil)
	if err != nil {
		return -1, ErrServerInternalError, fmt.Errorf("eth_call failed: %w", err)
	}

	// Decode return value (assume string)
	var result int64
	if err := parsedABI.UnpackIntoInterface(&result, method.Name, output); err != nil {
		return -1, ErrServerInternalError, fmt.Errorf("decode result: %w", err)
	}

	if result == 0 {
		return -1, ErrDealRejectedByMarket, fmt.Errorf("empty result from contract")
	}

	return result, Ok, nil
}

func (d *DDOV1) ProductName() ProductName {
	return ProductNameDDOV1
}

var _ product = &DDOV1{}
