package mk20

import (
	"bytes"
	"context"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ipfs/go-cid"
	"github.com/samber/lo"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin/v16/verifreg"

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	mk20contract "github.com/filecoin-project/curio/market/mk20/contract"
)

var ErrUnknowContract = errors.New("provider does not work with this market")

// DDOV1 defines a structure for handling provider, client, and piece manager information with associated contract and notification details
// for a DDO deal handling.
type DDOV1 struct {

	// Provider specifies the address of the provider
	Provider address.Address `json:"provider"`

	// Duration represents the deal duration in epochs. This value is ignored for the deal with allocationID.
	// It must be at least 518400
	Duration abi.ChainEpoch `json:"duration"`

	// AllocationId represents an allocation identifier for the deal.
	AllocationId *verifreg.AllocationId `json:"allocation_id,omitempty"`

	// MarketAddress specifies the address of the market governing the deal
	MarketAddress string `json:"market_address"`

	// MarketDealID specifies the deal ID for the market actor at MarketAddress
	MarketDealID *uint64 `json:"market_deal_id"`

	// NotificationAddress specifies the address to which notifications will be relayed to when sector is activated
	NotificationAddress address.Address `json:"notification_address"`

	// NotificationPayload holds the notification data, typically in a serialized byte array format.
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

	if d.MarketAddress != "" {
		if len(d.MarketAddress) < 2 {
			return ErrProductValidationFailed, xerrors.Errorf("market address too short")
		}
		if d.MarketAddress[0:2] != "0x" {
			return ErrProductValidationFailed, xerrors.Errorf("market address must start with 0x")
		}
		if !common.IsHexAddress(d.MarketAddress) {
			return ErrProductValidationFailed, xerrors.Errorf("invalid market address")
		}
	}

	if d.NotificationAddress == address.Undef || d.NotificationAddress.Empty() {
		if d.NotificationPayload != nil {
			return ErrProductValidationFailed, xerrors.Errorf("notification payload cannot be set without notification address")
		}
	} else {
		if d.NotificationPayload == nil {
			return ErrProductValidationFailed, xerrors.Errorf("notification payload is not set")
		}
	}

	return Ok, nil
}

func (d *DDOV1) VerifyMarketDeal(ctx context.Context, db *harmonydb.DB, eth *ethclient.Client, deal *Deal) (DealCode, error) {
	if d.MarketAddress == "" {
		return Ok, nil
	}

	if d.MarketDealID == nil {
		return ErrProductValidationFailed, xerrors.Errorf("market deal id is not set")
	}

	if deal == nil {
		return ErrBadProposal, xerrors.Errorf("deal is nil")
	}

	if deal.Data == nil {
		return ErrBadProposal, xerrors.Errorf("deal data is required for market verification")
	}

	var allowed bool
	err := db.QueryRow(ctx, `SELECT allowed FROM ddo_contracts WHERE address = $1`, d.MarketAddress).Scan(&allowed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrBadProposal, ErrUnknowContract
		}
		return ErrServerInternalError, xerrors.Errorf("getting abi: %w", err)
	}

	if !allowed {
		return ErrBadProposal, xerrors.Errorf("market contract is not allowed by storage provider")
	}

	market, err := mk20contract.NewCurioDealViewV1Caller(common.HexToAddress(d.MarketAddress), eth)
	if err != nil {
		return ErrServerInternalError, xerrors.Errorf("creating CurioDealViewV1 caller: %w", err)
	}

	version, err := market.Version(&bind.CallOpts{Context: ctx})
	if err != nil {
		return ErrServerInternalError, xerrors.Errorf("calling market version: %w", err)
	}
	if version == nil || version.Uint64() != 1 {
		return ErrMarketNotEnabled, xerrors.Errorf("unsupported market interface version: %v", version)
	}

	view, err := market.GetDeal(&bind.CallOpts{Context: ctx}, new(big.Int).SetUint64(*d.MarketDealID))
	if err != nil {
		if isDealNotFoundRevert(err) {
			return ErrDealRejectedByMarket, xerrors.Errorf("deal %d not found in market", *d.MarketDealID)
		}
		return ErrServerInternalError, xerrors.Errorf("calling market getDeal: %w", err)
	}

	// Match on-chain values with local deal values.
	localProviderID, err := address.IDFromAddress(d.Provider)
	if err != nil {
		return ErrProductValidationFailed, xerrors.Errorf("invalid provider for market verification: %w", err)
	}

	if view.ProviderActorId == nil || view.ProviderActorId.Uint64() != localProviderID {
		return ErrDealRejectedByMarket, xerrors.Errorf("provider mismatch: market=%v local=%d", view.ProviderActorId, localProviderID)
	}

	// TODO: Review if care about client at all
	localClient := localClientIDBytes(deal.Client)
	if !bytes.Equal(view.ClientId, localClient) {
		return ErrDealRejectedByMarket, xerrors.Errorf("client mismatch between market deal and local deal")
	}

	pcid, err := cid.Cast(view.PieceCidV2)
	if err != nil {
		return ErrDealRejectedByMarket, xerrors.Errorf("failed to cast market deal piece cid: %w", err)
	}

	if !deal.Data.PieceCID.Equals(pcid) {
		return ErrDealRejectedByMarket, xerrors.Errorf("piece cid mismatch between market deal and local deal")
	}

	if d.AllocationId != nil {
		if view.AllocationId == nil || view.AllocationId.Uint64() != uint64(*d.AllocationId) {
			return ErrDealRejectedByMarket, xerrors.Errorf("allocation mismatch between market deal and local deal")
		}
	}
	if d.AllocationId == nil {
		if view.AllocationId != nil && view.AllocationId.Sign() != 0 {
			return ErrDealRejectedByMarket, xerrors.Errorf("allocation mismatch between market deal and local deal")
		}
	}

	if view.Duration == nil || view.Duration.Cmp(big.NewInt(int64(d.Duration))) != 0 {
		return ErrDealRejectedByMarket, xerrors.Errorf("duration mismatch between market deal and local deal")
	}

	// Finalized deal is terminal and cannot be accepted for onboarding.
	if view.State == 2 {
		return ErrDealRejectedByMarket, xerrors.Errorf("market deal %d is already finalized", *d.MarketDealID)
	}

	// TODO: Guard against start duration here. Maybe use a config to allow minimum now+2days to max now+7days

	return Ok, nil
}

func isDealNotFoundRevert(err error) bool {
	var dataErr rpc.DataError
	if !errors.As(err, &dataErr) {
		return false
	}

	revertDataHex, ok := dataErr.ErrorData().(string)
	if !ok {
		return false
	}

	revertData, err := hexutil.Decode(revertDataHex)
	if err != nil {
		return false
	}
	if len(revertData) < 4 {
		return false
	}

	parsedABI, err := mk20contract.CurioDealViewV1MetaData.GetAbi()
	if err != nil {
		return false
	}
	dealNotFound, ok := parsedABI.Errors["DealNotFound"]
	if !ok {
		return false
	}

	return bytes.Equal(revertData[:4], dealNotFound.ID[:4])
}

func localClientIDBytes(client string) []byte {
	a, err := address.NewFromString(client)
	if err != nil {
		return []byte(client)
	}
	return a.Bytes()
}

func (d *DDOV1) ProductName() ProductName {
	return ProductNameDDOV1
}

var _ product = &DDOV1{}
