// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package FWSS

import (
	"errors"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// CidsCid is an auto generated low-level Go binding around an user-defined struct.
type CidsCid struct {
	Data []byte
}

// FilecoinWarmStorageServicePlannedUpgrade is an auto generated low-level Go binding around an user-defined struct.
type FilecoinWarmStorageServicePlannedUpgrade struct {
	NextImplementation common.Address
	AfterEpoch         *big.Int
}

// FilecoinWarmStorageServiceServicePricing is an auto generated low-level Go binding around an user-defined struct.
type FilecoinWarmStorageServiceServicePricing struct {
	PricePerTiBPerMonthNoCDN   *big.Int
	PricePerTiBPerMonthWithCDN *big.Int
	TokenAddress               common.Address
	EpochsPerMonth             *big.Int
}

// IValidatorValidationResult is an auto generated low-level Go binding around an user-defined struct.
type IValidatorValidationResult struct {
	ModifiedAmount *big.Int
	SettleUpto     *big.Int
	Note           string
}

// FilecoinWarmStorageServiceMetaData contains all meta data concerning the FilecoinWarmStorageService contract.
var FilecoinWarmStorageServiceMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[{\"name\":\"_pdpVerifierAddress\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"_paymentsContractAddress\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"_usdfc\",\"type\":\"address\",\"internalType\":\"contractIERC20Metadata\"},{\"name\":\"_filBeamBeneficiaryAddress\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"_serviceProviderRegistry\",\"type\":\"address\",\"internalType\":\"contractServiceProviderRegistry\"},{\"name\":\"_sessionKeyRegistry\",\"type\":\"address\",\"internalType\":\"contractSessionKeyRegistry\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"UPGRADE_INTERFACE_VERSION\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"VERSION\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"addApprovedProvider\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"announcePlannedUpgrade\",\"inputs\":[{\"name\":\"plannedUpgrade\",\"type\":\"tuple\",\"internalType\":\"structFilecoinWarmStorageService.PlannedUpgrade\",\"components\":[{\"name\":\"nextImplementation\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"afterEpoch\",\"type\":\"uint96\",\"internalType\":\"uint96\"}]}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"calculateRatesPerEpoch\",\"inputs\":[{\"name\":\"totalBytes\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"storageRate\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"cacheMissRate\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"cdnRate\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"configureProvingPeriod\",\"inputs\":[{\"name\":\"_maxProvingPeriod\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"_challengeWindowSize\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"dataSetCreated\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"serviceProvider\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"extraData\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"dataSetDeleted\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"eip712Domain\",\"inputs\":[],\"outputs\":[{\"name\":\"fields\",\"type\":\"bytes1\",\"internalType\":\"bytes1\"},{\"name\":\"name\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"version\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"chainId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"verifyingContract\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"salt\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"extensions\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"extsload\",\"inputs\":[{\"name\":\"slot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"extsloadStruct\",\"inputs\":[{\"name\":\"slot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"size\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32[]\",\"internalType\":\"bytes32[]\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"filBeamBeneficiaryAddress\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getEffectiveRates\",\"inputs\":[],\"outputs\":[{\"name\":\"serviceFee\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"spPayment\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getProvingPeriodForEpoch\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"epoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getServicePrice\",\"inputs\":[],\"outputs\":[{\"name\":\"pricing\",\"type\":\"tuple\",\"internalType\":\"structFilecoinWarmStorageService.ServicePricing\",\"components\":[{\"name\":\"pricePerTiBPerMonthNoCDN\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"pricePerTiBPerMonthWithCDN\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"tokenAddress\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"epochsPerMonth\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"initialize\",\"inputs\":[{\"name\":\"_maxProvingPeriod\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"_challengeWindowSize\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"_filBeamControllerAddress\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"_name\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"_description\",\"type\":\"string\",\"internalType\":\"string\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"migrate\",\"inputs\":[{\"name\":\"_viewContract\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"nextProvingPeriod\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"challengeEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"leafCount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"owner\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"paymentsContractAddress\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"pdpVerifierAddress\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"piecesAdded\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"firstAdded\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"pieceData\",\"type\":\"tuple[]\",\"internalType\":\"structCids.Cid[]\",\"components\":[{\"name\":\"data\",\"type\":\"bytes\",\"internalType\":\"bytes\"}]},{\"name\":\"extraData\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"piecesScheduledRemove\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"pieceIds\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"},{\"name\":\"extraData\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"possessionProven\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"challengeCount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"proxiableUUID\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"railTerminated\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"terminator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"endEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"removeApprovedProvider\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"index\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"renounceOwnership\",\"inputs\":[],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"serviceProviderRegistry\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"contractServiceProviderRegistry\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"sessionKeyRegistry\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"contractSessionKeyRegistry\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"setViewContract\",\"inputs\":[{\"name\":\"_viewContract\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"settleFilBeamPaymentRails\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"cdnAmount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"cacheMissAmount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"storageProviderChanged\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"terminateCDNService\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"terminateService\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"topUpCDNPaymentRails\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"cdnAmountToAdd\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"cacheMissAmountToAdd\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"transferFilBeamController\",\"inputs\":[{\"name\":\"newController\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"transferOwnership\",\"inputs\":[{\"name\":\"newOwner\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"updateServiceCommission\",\"inputs\":[{\"name\":\"newCommissionBps\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"upgradeToAndCall\",\"inputs\":[{\"name\":\"newImplementation\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"data\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"payable\"},{\"type\":\"function\",\"name\":\"usdfcTokenAddress\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"contractIERC20Metadata\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"validatePayment\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"proposedAmount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"fromEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"toEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"result\",\"type\":\"tuple\",\"internalType\":\"structIValidator.ValidationResult\",\"components\":[{\"name\":\"modifiedAmount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"settleUpto\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"note\",\"type\":\"string\",\"internalType\":\"string\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"viewContractAddress\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"event\",\"name\":\"CDNPaymentRailsToppedUp\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"cdnAmountAdded\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"totalCdnLockup\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"cacheMissAmountAdded\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"totalCacheMissLockup\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"CDNPaymentTerminated\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"endEpoch\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"cacheMissRailId\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"cdnRailId\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"CDNServiceTerminated\",\"inputs\":[{\"name\":\"caller\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"dataSetId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"cacheMissRailId\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"cdnRailId\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ContractUpgraded\",\"inputs\":[{\"name\":\"version\",\"type\":\"string\",\"indexed\":false,\"internalType\":\"string\"},{\"name\":\"implementation\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"DataSetCreated\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"providerId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"pdpRailId\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"cacheMissRailId\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"cdnRailId\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"payer\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"},{\"name\":\"serviceProvider\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"},{\"name\":\"payee\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"},{\"name\":\"metadataKeys\",\"type\":\"string[]\",\"indexed\":false,\"internalType\":\"string[]\"},{\"name\":\"metadataValues\",\"type\":\"string[]\",\"indexed\":false,\"internalType\":\"string[]\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"DataSetServiceProviderChanged\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"oldServiceProvider\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"newServiceProvider\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"EIP712DomainChanged\",\"inputs\":[],\"anonymous\":false},{\"type\":\"event\",\"name\":\"FaultRecord\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"periodsFaulted\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"deadline\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"FilBeamControllerChanged\",\"inputs\":[{\"name\":\"oldController\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"},{\"name\":\"newController\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"FilecoinServiceDeployed\",\"inputs\":[{\"name\":\"name\",\"type\":\"string\",\"indexed\":false,\"internalType\":\"string\"},{\"name\":\"description\",\"type\":\"string\",\"indexed\":false,\"internalType\":\"string\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Initialized\",\"inputs\":[{\"name\":\"version\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"OwnershipTransferred\",\"inputs\":[{\"name\":\"previousOwner\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"newOwner\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"PDPPaymentTerminated\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"endEpoch\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"pdpRailId\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"PieceAdded\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"pieceId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"pieceCid\",\"type\":\"tuple\",\"indexed\":false,\"internalType\":\"structCids.Cid\",\"components\":[{\"name\":\"data\",\"type\":\"bytes\",\"internalType\":\"bytes\"}]},{\"name\":\"keys\",\"type\":\"string[]\",\"indexed\":false,\"internalType\":\"string[]\"},{\"name\":\"values\",\"type\":\"string[]\",\"indexed\":false,\"internalType\":\"string[]\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ProviderApproved\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ProviderUnapproved\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"RailRateUpdated\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"railId\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"newRate\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ServiceTerminated\",\"inputs\":[{\"name\":\"caller\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"dataSetId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"pdpRailId\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"cacheMissRailId\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"cdnRailId\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"UpgradeAnnounced\",\"inputs\":[{\"name\":\"plannedUpgrade\",\"type\":\"tuple\",\"indexed\":false,\"internalType\":\"structFilecoinWarmStorageService.PlannedUpgrade\",\"components\":[{\"name\":\"nextImplementation\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"afterEpoch\",\"type\":\"uint96\",\"internalType\":\"uint96\"}]}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Upgraded\",\"inputs\":[{\"name\":\"implementation\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ViewContractSet\",\"inputs\":[{\"name\":\"viewContract\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"error\",\"name\":\"AddressEmptyCode\",\"inputs\":[{\"name\":\"target\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"CDNPaymentAlreadyTerminated\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"CacheMissPaymentAlreadyTerminated\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"CallerNotPayer\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"expectedPayer\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"caller\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"CallerNotPayerOrPayee\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"expectedPayer\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"expectedPayee\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"caller\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"CallerNotPayments\",\"inputs\":[{\"name\":\"expected\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"actual\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"ChallengeWindowTooEarly\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"windowStart\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"nowBlock\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"ClientDataSetAlreadyRegistered\",\"inputs\":[{\"name\":\"clientDataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"CommissionExceedsMaximum\",\"inputs\":[{\"name\":\"commissionType\",\"type\":\"uint8\",\"internalType\":\"enumErrors.CommissionType\"},{\"name\":\"max\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"actual\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"DataSetNotFoundForRail\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"DataSetNotRegistered\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"DataSetPaymentAlreadyTerminated\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"DataSetPaymentBeyondEndEpoch\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"pdpEndEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"currentBlock\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"DivisionByZero\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"DuplicateMetadataKey\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"key\",\"type\":\"string\",\"internalType\":\"string\"}]},{\"type\":\"error\",\"name\":\"ERC1967InvalidImplementation\",\"inputs\":[{\"name\":\"implementation\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"ERC1967NonPayable\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"ExtraDataRequired\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"FailedCall\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"FilBeamServiceNotConfigured\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"InvalidChallengeCount\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"minExpected\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"actual\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"InvalidChallengeEpoch\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"minAllowed\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"maxAllowed\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"actual\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"InvalidChallengeWindowSize\",\"inputs\":[{\"name\":\"maxProvingPeriod\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"challengeWindowSize\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"InvalidDataSetId\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"InvalidEpochRange\",\"inputs\":[{\"name\":\"fromEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"toEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"InvalidInitialization\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidServiceDescriptionLength\",\"inputs\":[{\"name\":\"length\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"InvalidServiceNameLength\",\"inputs\":[{\"name\":\"length\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"InvalidTopUpAmount\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"MaxProvingPeriodZero\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"MetadataArrayCountMismatch\",\"inputs\":[{\"name\":\"metadataArrayCount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"pieceCount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"MetadataKeyAndValueLengthMismatch\",\"inputs\":[{\"name\":\"keysLength\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"valuesLength\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"MetadataKeyExceedsMaxLength\",\"inputs\":[{\"name\":\"index\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"maxAllowed\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"length\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"MetadataValueExceedsMaxLength\",\"inputs\":[{\"name\":\"index\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"maxAllowed\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"length\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"NextProvingPeriodAlreadyCalled\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"periodDeadline\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"nowBlock\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"NoPDPPaymentRail\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"NotInitializing\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"OnlyFilBeamControllerAllowed\",\"inputs\":[{\"name\":\"expected\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"actual\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"OnlyPDPVerifierAllowed\",\"inputs\":[{\"name\":\"expected\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"actual\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"OwnableInvalidOwner\",\"inputs\":[{\"name\":\"owner\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"OwnableUnauthorizedAccount\",\"inputs\":[{\"name\":\"account\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"PaymentRailsNotFinalized\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"pdpEndEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"ProofAlreadySubmitted\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"ProviderAlreadyApproved\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"ProviderNotInApprovedList\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"ProviderNotRegistered\",\"inputs\":[{\"name\":\"provider\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"ProvingNotStarted\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"ProvingPeriodPassed\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"deadline\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"nowBlock\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"RailNotAssociated\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"ServiceContractMustTerminateRail\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"TooManyMetadataKeys\",\"inputs\":[{\"name\":\"maxAllowed\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"keysLength\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"UUPSUnauthorizedCallContext\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"UUPSUnsupportedProxiableUUID\",\"inputs\":[{\"name\":\"slot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}]},{\"type\":\"error\",\"name\":\"ZeroAddress\",\"inputs\":[{\"name\":\"field\",\"type\":\"uint8\",\"internalType\":\"enumErrors.AddressField\"}]}]",
}

// FilecoinWarmStorageServiceABI is the input ABI used to generate the binding from.
// Deprecated: Use FilecoinWarmStorageServiceMetaData.ABI instead.
var FilecoinWarmStorageServiceABI = FilecoinWarmStorageServiceMetaData.ABI

// FilecoinWarmStorageService is an auto generated Go binding around an Ethereum contract.
type FilecoinWarmStorageService struct {
	FilecoinWarmStorageServiceCaller     // Read-only binding to the contract
	FilecoinWarmStorageServiceTransactor // Write-only binding to the contract
	FilecoinWarmStorageServiceFilterer   // Log filterer for contract events
}

// FilecoinWarmStorageServiceCaller is an auto generated read-only Go binding around an Ethereum contract.
type FilecoinWarmStorageServiceCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FilecoinWarmStorageServiceTransactor is an auto generated write-only Go binding around an Ethereum contract.
type FilecoinWarmStorageServiceTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FilecoinWarmStorageServiceFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type FilecoinWarmStorageServiceFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FilecoinWarmStorageServiceSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type FilecoinWarmStorageServiceSession struct {
	Contract     *FilecoinWarmStorageService // Generic contract binding to set the session for
	CallOpts     bind.CallOpts               // Call options to use throughout this session
	TransactOpts bind.TransactOpts           // Transaction auth options to use throughout this session
}

// FilecoinWarmStorageServiceCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type FilecoinWarmStorageServiceCallerSession struct {
	Contract *FilecoinWarmStorageServiceCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts                     // Call options to use throughout this session
}

// FilecoinWarmStorageServiceTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type FilecoinWarmStorageServiceTransactorSession struct {
	Contract     *FilecoinWarmStorageServiceTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts                     // Transaction auth options to use throughout this session
}

// FilecoinWarmStorageServiceRaw is an auto generated low-level Go binding around an Ethereum contract.
type FilecoinWarmStorageServiceRaw struct {
	Contract *FilecoinWarmStorageService // Generic contract binding to access the raw methods on
}

// FilecoinWarmStorageServiceCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type FilecoinWarmStorageServiceCallerRaw struct {
	Contract *FilecoinWarmStorageServiceCaller // Generic read-only contract binding to access the raw methods on
}

// FilecoinWarmStorageServiceTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type FilecoinWarmStorageServiceTransactorRaw struct {
	Contract *FilecoinWarmStorageServiceTransactor // Generic write-only contract binding to access the raw methods on
}

// NewFilecoinWarmStorageService creates a new instance of FilecoinWarmStorageService, bound to a specific deployed contract.
func NewFilecoinWarmStorageService(address common.Address, backend bind.ContractBackend) (*FilecoinWarmStorageService, error) {
	contract, err := bindFilecoinWarmStorageService(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageService{FilecoinWarmStorageServiceCaller: FilecoinWarmStorageServiceCaller{contract: contract}, FilecoinWarmStorageServiceTransactor: FilecoinWarmStorageServiceTransactor{contract: contract}, FilecoinWarmStorageServiceFilterer: FilecoinWarmStorageServiceFilterer{contract: contract}}, nil
}

// NewFilecoinWarmStorageServiceCaller creates a new read-only instance of FilecoinWarmStorageService, bound to a specific deployed contract.
func NewFilecoinWarmStorageServiceCaller(address common.Address, caller bind.ContractCaller) (*FilecoinWarmStorageServiceCaller, error) {
	contract, err := bindFilecoinWarmStorageService(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceCaller{contract: contract}, nil
}

// NewFilecoinWarmStorageServiceTransactor creates a new write-only instance of FilecoinWarmStorageService, bound to a specific deployed contract.
func NewFilecoinWarmStorageServiceTransactor(address common.Address, transactor bind.ContractTransactor) (*FilecoinWarmStorageServiceTransactor, error) {
	contract, err := bindFilecoinWarmStorageService(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceTransactor{contract: contract}, nil
}

// NewFilecoinWarmStorageServiceFilterer creates a new log filterer instance of FilecoinWarmStorageService, bound to a specific deployed contract.
func NewFilecoinWarmStorageServiceFilterer(address common.Address, filterer bind.ContractFilterer) (*FilecoinWarmStorageServiceFilterer, error) {
	contract, err := bindFilecoinWarmStorageService(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceFilterer{contract: contract}, nil
}

// bindFilecoinWarmStorageService binds a generic wrapper to an already deployed contract.
func bindFilecoinWarmStorageService(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := FilecoinWarmStorageServiceMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FilecoinWarmStorageService.Contract.FilecoinWarmStorageServiceCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.FilecoinWarmStorageServiceTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.FilecoinWarmStorageServiceTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FilecoinWarmStorageService.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.contract.Transact(opts, method, params...)
}

// UPGRADEINTERFACEVERSION is a free data retrieval call binding the contract method 0xad3cb1cc.
//
// Solidity: function UPGRADE_INTERFACE_VERSION() view returns(string)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) UPGRADEINTERFACEVERSION(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "UPGRADE_INTERFACE_VERSION")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// UPGRADEINTERFACEVERSION is a free data retrieval call binding the contract method 0xad3cb1cc.
//
// Solidity: function UPGRADE_INTERFACE_VERSION() view returns(string)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) UPGRADEINTERFACEVERSION() (string, error) {
	return _FilecoinWarmStorageService.Contract.UPGRADEINTERFACEVERSION(&_FilecoinWarmStorageService.CallOpts)
}

// UPGRADEINTERFACEVERSION is a free data retrieval call binding the contract method 0xad3cb1cc.
//
// Solidity: function UPGRADE_INTERFACE_VERSION() view returns(string)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) UPGRADEINTERFACEVERSION() (string, error) {
	return _FilecoinWarmStorageService.Contract.UPGRADEINTERFACEVERSION(&_FilecoinWarmStorageService.CallOpts)
}

// VERSION is a free data retrieval call binding the contract method 0xffa1ad74.
//
// Solidity: function VERSION() view returns(string)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) VERSION(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "VERSION")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// VERSION is a free data retrieval call binding the contract method 0xffa1ad74.
//
// Solidity: function VERSION() view returns(string)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) VERSION() (string, error) {
	return _FilecoinWarmStorageService.Contract.VERSION(&_FilecoinWarmStorageService.CallOpts)
}

// VERSION is a free data retrieval call binding the contract method 0xffa1ad74.
//
// Solidity: function VERSION() view returns(string)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) VERSION() (string, error) {
	return _FilecoinWarmStorageService.Contract.VERSION(&_FilecoinWarmStorageService.CallOpts)
}

// CalculateRatesPerEpoch is a free data retrieval call binding the contract method 0x4425b3a2.
//
// Solidity: function calculateRatesPerEpoch(uint256 totalBytes) view returns(uint256 storageRate, uint256 cacheMissRate, uint256 cdnRate)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) CalculateRatesPerEpoch(opts *bind.CallOpts, totalBytes *big.Int) (struct {
	StorageRate   *big.Int
	CacheMissRate *big.Int
	CdnRate       *big.Int
}, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "calculateRatesPerEpoch", totalBytes)

	outstruct := new(struct {
		StorageRate   *big.Int
		CacheMissRate *big.Int
		CdnRate       *big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.StorageRate = *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	outstruct.CacheMissRate = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	outstruct.CdnRate = *abi.ConvertType(out[2], new(*big.Int)).(**big.Int)

	return *outstruct, err

}

// CalculateRatesPerEpoch is a free data retrieval call binding the contract method 0x4425b3a2.
//
// Solidity: function calculateRatesPerEpoch(uint256 totalBytes) view returns(uint256 storageRate, uint256 cacheMissRate, uint256 cdnRate)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) CalculateRatesPerEpoch(totalBytes *big.Int) (struct {
	StorageRate   *big.Int
	CacheMissRate *big.Int
	CdnRate       *big.Int
}, error) {
	return _FilecoinWarmStorageService.Contract.CalculateRatesPerEpoch(&_FilecoinWarmStorageService.CallOpts, totalBytes)
}

// CalculateRatesPerEpoch is a free data retrieval call binding the contract method 0x4425b3a2.
//
// Solidity: function calculateRatesPerEpoch(uint256 totalBytes) view returns(uint256 storageRate, uint256 cacheMissRate, uint256 cdnRate)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) CalculateRatesPerEpoch(totalBytes *big.Int) (struct {
	StorageRate   *big.Int
	CacheMissRate *big.Int
	CdnRate       *big.Int
}, error) {
	return _FilecoinWarmStorageService.Contract.CalculateRatesPerEpoch(&_FilecoinWarmStorageService.CallOpts, totalBytes)
}

// Eip712Domain is a free data retrieval call binding the contract method 0x84b0196e.
//
// Solidity: function eip712Domain() view returns(bytes1 fields, string name, string version, uint256 chainId, address verifyingContract, bytes32 salt, uint256[] extensions)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) Eip712Domain(opts *bind.CallOpts) (struct {
	Fields            [1]byte
	Name              string
	Version           string
	ChainId           *big.Int
	VerifyingContract common.Address
	Salt              [32]byte
	Extensions        []*big.Int
}, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "eip712Domain")

	outstruct := new(struct {
		Fields            [1]byte
		Name              string
		Version           string
		ChainId           *big.Int
		VerifyingContract common.Address
		Salt              [32]byte
		Extensions        []*big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Fields = *abi.ConvertType(out[0], new([1]byte)).(*[1]byte)
	outstruct.Name = *abi.ConvertType(out[1], new(string)).(*string)
	outstruct.Version = *abi.ConvertType(out[2], new(string)).(*string)
	outstruct.ChainId = *abi.ConvertType(out[3], new(*big.Int)).(**big.Int)
	outstruct.VerifyingContract = *abi.ConvertType(out[4], new(common.Address)).(*common.Address)
	outstruct.Salt = *abi.ConvertType(out[5], new([32]byte)).(*[32]byte)
	outstruct.Extensions = *abi.ConvertType(out[6], new([]*big.Int)).(*[]*big.Int)

	return *outstruct, err

}

// Eip712Domain is a free data retrieval call binding the contract method 0x84b0196e.
//
// Solidity: function eip712Domain() view returns(bytes1 fields, string name, string version, uint256 chainId, address verifyingContract, bytes32 salt, uint256[] extensions)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) Eip712Domain() (struct {
	Fields            [1]byte
	Name              string
	Version           string
	ChainId           *big.Int
	VerifyingContract common.Address
	Salt              [32]byte
	Extensions        []*big.Int
}, error) {
	return _FilecoinWarmStorageService.Contract.Eip712Domain(&_FilecoinWarmStorageService.CallOpts)
}

// Eip712Domain is a free data retrieval call binding the contract method 0x84b0196e.
//
// Solidity: function eip712Domain() view returns(bytes1 fields, string name, string version, uint256 chainId, address verifyingContract, bytes32 salt, uint256[] extensions)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) Eip712Domain() (struct {
	Fields            [1]byte
	Name              string
	Version           string
	ChainId           *big.Int
	VerifyingContract common.Address
	Salt              [32]byte
	Extensions        []*big.Int
}, error) {
	return _FilecoinWarmStorageService.Contract.Eip712Domain(&_FilecoinWarmStorageService.CallOpts)
}

// Extsload is a free data retrieval call binding the contract method 0x1e2eaeaf.
//
// Solidity: function extsload(bytes32 slot) view returns(bytes32)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) Extsload(opts *bind.CallOpts, slot [32]byte) ([32]byte, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "extsload", slot)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// Extsload is a free data retrieval call binding the contract method 0x1e2eaeaf.
//
// Solidity: function extsload(bytes32 slot) view returns(bytes32)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) Extsload(slot [32]byte) ([32]byte, error) {
	return _FilecoinWarmStorageService.Contract.Extsload(&_FilecoinWarmStorageService.CallOpts, slot)
}

// Extsload is a free data retrieval call binding the contract method 0x1e2eaeaf.
//
// Solidity: function extsload(bytes32 slot) view returns(bytes32)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) Extsload(slot [32]byte) ([32]byte, error) {
	return _FilecoinWarmStorageService.Contract.Extsload(&_FilecoinWarmStorageService.CallOpts, slot)
}

// ExtsloadStruct is a free data retrieval call binding the contract method 0x5379a435.
//
// Solidity: function extsloadStruct(bytes32 slot, uint256 size) view returns(bytes32[])
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) ExtsloadStruct(opts *bind.CallOpts, slot [32]byte, size *big.Int) ([][32]byte, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "extsloadStruct", slot, size)

	if err != nil {
		return *new([][32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([][32]byte)).(*[][32]byte)

	return out0, err

}

// ExtsloadStruct is a free data retrieval call binding the contract method 0x5379a435.
//
// Solidity: function extsloadStruct(bytes32 slot, uint256 size) view returns(bytes32[])
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) ExtsloadStruct(slot [32]byte, size *big.Int) ([][32]byte, error) {
	return _FilecoinWarmStorageService.Contract.ExtsloadStruct(&_FilecoinWarmStorageService.CallOpts, slot, size)
}

// ExtsloadStruct is a free data retrieval call binding the contract method 0x5379a435.
//
// Solidity: function extsloadStruct(bytes32 slot, uint256 size) view returns(bytes32[])
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) ExtsloadStruct(slot [32]byte, size *big.Int) ([][32]byte, error) {
	return _FilecoinWarmStorageService.Contract.ExtsloadStruct(&_FilecoinWarmStorageService.CallOpts, slot, size)
}

// FilBeamBeneficiaryAddress is a free data retrieval call binding the contract method 0xdd6979bf.
//
// Solidity: function filBeamBeneficiaryAddress() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) FilBeamBeneficiaryAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "filBeamBeneficiaryAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// FilBeamBeneficiaryAddress is a free data retrieval call binding the contract method 0xdd6979bf.
//
// Solidity: function filBeamBeneficiaryAddress() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) FilBeamBeneficiaryAddress() (common.Address, error) {
	return _FilecoinWarmStorageService.Contract.FilBeamBeneficiaryAddress(&_FilecoinWarmStorageService.CallOpts)
}

// FilBeamBeneficiaryAddress is a free data retrieval call binding the contract method 0xdd6979bf.
//
// Solidity: function filBeamBeneficiaryAddress() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) FilBeamBeneficiaryAddress() (common.Address, error) {
	return _FilecoinWarmStorageService.Contract.FilBeamBeneficiaryAddress(&_FilecoinWarmStorageService.CallOpts)
}

// GetEffectiveRates is a free data retrieval call binding the contract method 0x93124a79.
//
// Solidity: function getEffectiveRates() view returns(uint256 serviceFee, uint256 spPayment)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) GetEffectiveRates(opts *bind.CallOpts) (struct {
	ServiceFee *big.Int
	SpPayment  *big.Int
}, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "getEffectiveRates")

	outstruct := new(struct {
		ServiceFee *big.Int
		SpPayment  *big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.ServiceFee = *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	outstruct.SpPayment = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)

	return *outstruct, err

}

// GetEffectiveRates is a free data retrieval call binding the contract method 0x93124a79.
//
// Solidity: function getEffectiveRates() view returns(uint256 serviceFee, uint256 spPayment)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) GetEffectiveRates() (struct {
	ServiceFee *big.Int
	SpPayment  *big.Int
}, error) {
	return _FilecoinWarmStorageService.Contract.GetEffectiveRates(&_FilecoinWarmStorageService.CallOpts)
}

// GetEffectiveRates is a free data retrieval call binding the contract method 0x93124a79.
//
// Solidity: function getEffectiveRates() view returns(uint256 serviceFee, uint256 spPayment)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) GetEffectiveRates() (struct {
	ServiceFee *big.Int
	SpPayment  *big.Int
}, error) {
	return _FilecoinWarmStorageService.Contract.GetEffectiveRates(&_FilecoinWarmStorageService.CallOpts)
}

// GetProvingPeriodForEpoch is a free data retrieval call binding the contract method 0x4a1fd7a3.
//
// Solidity: function getProvingPeriodForEpoch(uint256 dataSetId, uint256 epoch) view returns(uint256)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) GetProvingPeriodForEpoch(opts *bind.CallOpts, dataSetId *big.Int, epoch *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "getProvingPeriodForEpoch", dataSetId, epoch)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetProvingPeriodForEpoch is a free data retrieval call binding the contract method 0x4a1fd7a3.
//
// Solidity: function getProvingPeriodForEpoch(uint256 dataSetId, uint256 epoch) view returns(uint256)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) GetProvingPeriodForEpoch(dataSetId *big.Int, epoch *big.Int) (*big.Int, error) {
	return _FilecoinWarmStorageService.Contract.GetProvingPeriodForEpoch(&_FilecoinWarmStorageService.CallOpts, dataSetId, epoch)
}

// GetProvingPeriodForEpoch is a free data retrieval call binding the contract method 0x4a1fd7a3.
//
// Solidity: function getProvingPeriodForEpoch(uint256 dataSetId, uint256 epoch) view returns(uint256)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) GetProvingPeriodForEpoch(dataSetId *big.Int, epoch *big.Int) (*big.Int, error) {
	return _FilecoinWarmStorageService.Contract.GetProvingPeriodForEpoch(&_FilecoinWarmStorageService.CallOpts, dataSetId, epoch)
}

// GetServicePrice is a free data retrieval call binding the contract method 0x5482bdf9.
//
// Solidity: function getServicePrice() view returns((uint256,uint256,address,uint256) pricing)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) GetServicePrice(opts *bind.CallOpts) (FilecoinWarmStorageServiceServicePricing, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "getServicePrice")

	if err != nil {
		return *new(FilecoinWarmStorageServiceServicePricing), err
	}

	out0 := *abi.ConvertType(out[0], new(FilecoinWarmStorageServiceServicePricing)).(*FilecoinWarmStorageServiceServicePricing)

	return out0, err

}

// GetServicePrice is a free data retrieval call binding the contract method 0x5482bdf9.
//
// Solidity: function getServicePrice() view returns((uint256,uint256,address,uint256) pricing)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) GetServicePrice() (FilecoinWarmStorageServiceServicePricing, error) {
	return _FilecoinWarmStorageService.Contract.GetServicePrice(&_FilecoinWarmStorageService.CallOpts)
}

// GetServicePrice is a free data retrieval call binding the contract method 0x5482bdf9.
//
// Solidity: function getServicePrice() view returns((uint256,uint256,address,uint256) pricing)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) GetServicePrice() (FilecoinWarmStorageServiceServicePricing, error) {
	return _FilecoinWarmStorageService.Contract.GetServicePrice(&_FilecoinWarmStorageService.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) Owner() (common.Address, error) {
	return _FilecoinWarmStorageService.Contract.Owner(&_FilecoinWarmStorageService.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) Owner() (common.Address, error) {
	return _FilecoinWarmStorageService.Contract.Owner(&_FilecoinWarmStorageService.CallOpts)
}

// PaymentsContractAddress is a free data retrieval call binding the contract method 0xbc471469.
//
// Solidity: function paymentsContractAddress() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) PaymentsContractAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "paymentsContractAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// PaymentsContractAddress is a free data retrieval call binding the contract method 0xbc471469.
//
// Solidity: function paymentsContractAddress() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) PaymentsContractAddress() (common.Address, error) {
	return _FilecoinWarmStorageService.Contract.PaymentsContractAddress(&_FilecoinWarmStorageService.CallOpts)
}

// PaymentsContractAddress is a free data retrieval call binding the contract method 0xbc471469.
//
// Solidity: function paymentsContractAddress() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) PaymentsContractAddress() (common.Address, error) {
	return _FilecoinWarmStorageService.Contract.PaymentsContractAddress(&_FilecoinWarmStorageService.CallOpts)
}

// PdpVerifierAddress is a free data retrieval call binding the contract method 0xde4b6b71.
//
// Solidity: function pdpVerifierAddress() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) PdpVerifierAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "pdpVerifierAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// PdpVerifierAddress is a free data retrieval call binding the contract method 0xde4b6b71.
//
// Solidity: function pdpVerifierAddress() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) PdpVerifierAddress() (common.Address, error) {
	return _FilecoinWarmStorageService.Contract.PdpVerifierAddress(&_FilecoinWarmStorageService.CallOpts)
}

// PdpVerifierAddress is a free data retrieval call binding the contract method 0xde4b6b71.
//
// Solidity: function pdpVerifierAddress() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) PdpVerifierAddress() (common.Address, error) {
	return _FilecoinWarmStorageService.Contract.PdpVerifierAddress(&_FilecoinWarmStorageService.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) ProxiableUUID(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "proxiableUUID")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) ProxiableUUID() ([32]byte, error) {
	return _FilecoinWarmStorageService.Contract.ProxiableUUID(&_FilecoinWarmStorageService.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) ProxiableUUID() ([32]byte, error) {
	return _FilecoinWarmStorageService.Contract.ProxiableUUID(&_FilecoinWarmStorageService.CallOpts)
}

// ServiceProviderRegistry is a free data retrieval call binding the contract method 0x05f892ec.
//
// Solidity: function serviceProviderRegistry() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) ServiceProviderRegistry(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "serviceProviderRegistry")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// ServiceProviderRegistry is a free data retrieval call binding the contract method 0x05f892ec.
//
// Solidity: function serviceProviderRegistry() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) ServiceProviderRegistry() (common.Address, error) {
	return _FilecoinWarmStorageService.Contract.ServiceProviderRegistry(&_FilecoinWarmStorageService.CallOpts)
}

// ServiceProviderRegistry is a free data retrieval call binding the contract method 0x05f892ec.
//
// Solidity: function serviceProviderRegistry() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) ServiceProviderRegistry() (common.Address, error) {
	return _FilecoinWarmStorageService.Contract.ServiceProviderRegistry(&_FilecoinWarmStorageService.CallOpts)
}

// SessionKeyRegistry is a free data retrieval call binding the contract method 0x9f6aa572.
//
// Solidity: function sessionKeyRegistry() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) SessionKeyRegistry(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "sessionKeyRegistry")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// SessionKeyRegistry is a free data retrieval call binding the contract method 0x9f6aa572.
//
// Solidity: function sessionKeyRegistry() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) SessionKeyRegistry() (common.Address, error) {
	return _FilecoinWarmStorageService.Contract.SessionKeyRegistry(&_FilecoinWarmStorageService.CallOpts)
}

// SessionKeyRegistry is a free data retrieval call binding the contract method 0x9f6aa572.
//
// Solidity: function sessionKeyRegistry() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) SessionKeyRegistry() (common.Address, error) {
	return _FilecoinWarmStorageService.Contract.SessionKeyRegistry(&_FilecoinWarmStorageService.CallOpts)
}

// UsdfcTokenAddress is a free data retrieval call binding the contract method 0xd39b33ab.
//
// Solidity: function usdfcTokenAddress() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) UsdfcTokenAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "usdfcTokenAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// UsdfcTokenAddress is a free data retrieval call binding the contract method 0xd39b33ab.
//
// Solidity: function usdfcTokenAddress() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) UsdfcTokenAddress() (common.Address, error) {
	return _FilecoinWarmStorageService.Contract.UsdfcTokenAddress(&_FilecoinWarmStorageService.CallOpts)
}

// UsdfcTokenAddress is a free data retrieval call binding the contract method 0xd39b33ab.
//
// Solidity: function usdfcTokenAddress() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) UsdfcTokenAddress() (common.Address, error) {
	return _FilecoinWarmStorageService.Contract.UsdfcTokenAddress(&_FilecoinWarmStorageService.CallOpts)
}

// ValidatePayment is a free data retrieval call binding the contract method 0x1a7bf46f.
//
// Solidity: function validatePayment(uint256 railId, uint256 proposedAmount, uint256 fromEpoch, uint256 toEpoch, uint256 ) view returns((uint256,uint256,string) result)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) ValidatePayment(opts *bind.CallOpts, railId *big.Int, proposedAmount *big.Int, fromEpoch *big.Int, toEpoch *big.Int, arg4 *big.Int) (IValidatorValidationResult, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "validatePayment", railId, proposedAmount, fromEpoch, toEpoch, arg4)

	if err != nil {
		return *new(IValidatorValidationResult), err
	}

	out0 := *abi.ConvertType(out[0], new(IValidatorValidationResult)).(*IValidatorValidationResult)

	return out0, err

}

// ValidatePayment is a free data retrieval call binding the contract method 0x1a7bf46f.
//
// Solidity: function validatePayment(uint256 railId, uint256 proposedAmount, uint256 fromEpoch, uint256 toEpoch, uint256 ) view returns((uint256,uint256,string) result)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) ValidatePayment(railId *big.Int, proposedAmount *big.Int, fromEpoch *big.Int, toEpoch *big.Int, arg4 *big.Int) (IValidatorValidationResult, error) {
	return _FilecoinWarmStorageService.Contract.ValidatePayment(&_FilecoinWarmStorageService.CallOpts, railId, proposedAmount, fromEpoch, toEpoch, arg4)
}

// ValidatePayment is a free data retrieval call binding the contract method 0x1a7bf46f.
//
// Solidity: function validatePayment(uint256 railId, uint256 proposedAmount, uint256 fromEpoch, uint256 toEpoch, uint256 ) view returns((uint256,uint256,string) result)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) ValidatePayment(railId *big.Int, proposedAmount *big.Int, fromEpoch *big.Int, toEpoch *big.Int, arg4 *big.Int) (IValidatorValidationResult, error) {
	return _FilecoinWarmStorageService.Contract.ValidatePayment(&_FilecoinWarmStorageService.CallOpts, railId, proposedAmount, fromEpoch, toEpoch, arg4)
}

// ViewContractAddress is a free data retrieval call binding the contract method 0x7a9ebc15.
//
// Solidity: function viewContractAddress() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCaller) ViewContractAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FilecoinWarmStorageService.contract.Call(opts, &out, "viewContractAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// ViewContractAddress is a free data retrieval call binding the contract method 0x7a9ebc15.
//
// Solidity: function viewContractAddress() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) ViewContractAddress() (common.Address, error) {
	return _FilecoinWarmStorageService.Contract.ViewContractAddress(&_FilecoinWarmStorageService.CallOpts)
}

// ViewContractAddress is a free data retrieval call binding the contract method 0x7a9ebc15.
//
// Solidity: function viewContractAddress() view returns(address)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceCallerSession) ViewContractAddress() (common.Address, error) {
	return _FilecoinWarmStorageService.Contract.ViewContractAddress(&_FilecoinWarmStorageService.CallOpts)
}

// AddApprovedProvider is a paid mutator transaction binding the contract method 0xa71f9fec.
//
// Solidity: function addApprovedProvider(uint256 providerId) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) AddApprovedProvider(opts *bind.TransactOpts, providerId *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "addApprovedProvider", providerId)
}

// AddApprovedProvider is a paid mutator transaction binding the contract method 0xa71f9fec.
//
// Solidity: function addApprovedProvider(uint256 providerId) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) AddApprovedProvider(providerId *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.AddApprovedProvider(&_FilecoinWarmStorageService.TransactOpts, providerId)
}

// AddApprovedProvider is a paid mutator transaction binding the contract method 0xa71f9fec.
//
// Solidity: function addApprovedProvider(uint256 providerId) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) AddApprovedProvider(providerId *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.AddApprovedProvider(&_FilecoinWarmStorageService.TransactOpts, providerId)
}

// AnnouncePlannedUpgrade is a paid mutator transaction binding the contract method 0xbd003827.
//
// Solidity: function announcePlannedUpgrade((address,uint96) plannedUpgrade) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) AnnouncePlannedUpgrade(opts *bind.TransactOpts, plannedUpgrade FilecoinWarmStorageServicePlannedUpgrade) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "announcePlannedUpgrade", plannedUpgrade)
}

// AnnouncePlannedUpgrade is a paid mutator transaction binding the contract method 0xbd003827.
//
// Solidity: function announcePlannedUpgrade((address,uint96) plannedUpgrade) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) AnnouncePlannedUpgrade(plannedUpgrade FilecoinWarmStorageServicePlannedUpgrade) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.AnnouncePlannedUpgrade(&_FilecoinWarmStorageService.TransactOpts, plannedUpgrade)
}

// AnnouncePlannedUpgrade is a paid mutator transaction binding the contract method 0xbd003827.
//
// Solidity: function announcePlannedUpgrade((address,uint96) plannedUpgrade) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) AnnouncePlannedUpgrade(plannedUpgrade FilecoinWarmStorageServicePlannedUpgrade) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.AnnouncePlannedUpgrade(&_FilecoinWarmStorageService.TransactOpts, plannedUpgrade)
}

// ConfigureProvingPeriod is a paid mutator transaction binding the contract method 0xcee4f4c7.
//
// Solidity: function configureProvingPeriod(uint64 _maxProvingPeriod, uint256 _challengeWindowSize) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) ConfigureProvingPeriod(opts *bind.TransactOpts, _maxProvingPeriod uint64, _challengeWindowSize *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "configureProvingPeriod", _maxProvingPeriod, _challengeWindowSize)
}

// ConfigureProvingPeriod is a paid mutator transaction binding the contract method 0xcee4f4c7.
//
// Solidity: function configureProvingPeriod(uint64 _maxProvingPeriod, uint256 _challengeWindowSize) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) ConfigureProvingPeriod(_maxProvingPeriod uint64, _challengeWindowSize *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.ConfigureProvingPeriod(&_FilecoinWarmStorageService.TransactOpts, _maxProvingPeriod, _challengeWindowSize)
}

// ConfigureProvingPeriod is a paid mutator transaction binding the contract method 0xcee4f4c7.
//
// Solidity: function configureProvingPeriod(uint64 _maxProvingPeriod, uint256 _challengeWindowSize) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) ConfigureProvingPeriod(_maxProvingPeriod uint64, _challengeWindowSize *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.ConfigureProvingPeriod(&_FilecoinWarmStorageService.TransactOpts, _maxProvingPeriod, _challengeWindowSize)
}

// DataSetCreated is a paid mutator transaction binding the contract method 0x101c1eab.
//
// Solidity: function dataSetCreated(uint256 dataSetId, address serviceProvider, bytes extraData) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) DataSetCreated(opts *bind.TransactOpts, dataSetId *big.Int, serviceProvider common.Address, extraData []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "dataSetCreated", dataSetId, serviceProvider, extraData)
}

// DataSetCreated is a paid mutator transaction binding the contract method 0x101c1eab.
//
// Solidity: function dataSetCreated(uint256 dataSetId, address serviceProvider, bytes extraData) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) DataSetCreated(dataSetId *big.Int, serviceProvider common.Address, extraData []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.DataSetCreated(&_FilecoinWarmStorageService.TransactOpts, dataSetId, serviceProvider, extraData)
}

// DataSetCreated is a paid mutator transaction binding the contract method 0x101c1eab.
//
// Solidity: function dataSetCreated(uint256 dataSetId, address serviceProvider, bytes extraData) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) DataSetCreated(dataSetId *big.Int, serviceProvider common.Address, extraData []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.DataSetCreated(&_FilecoinWarmStorageService.TransactOpts, dataSetId, serviceProvider, extraData)
}

// DataSetDeleted is a paid mutator transaction binding the contract method 0x2abd465c.
//
// Solidity: function dataSetDeleted(uint256 dataSetId, uint256 , bytes ) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) DataSetDeleted(opts *bind.TransactOpts, dataSetId *big.Int, arg1 *big.Int, arg2 []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "dataSetDeleted", dataSetId, arg1, arg2)
}

// DataSetDeleted is a paid mutator transaction binding the contract method 0x2abd465c.
//
// Solidity: function dataSetDeleted(uint256 dataSetId, uint256 , bytes ) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) DataSetDeleted(dataSetId *big.Int, arg1 *big.Int, arg2 []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.DataSetDeleted(&_FilecoinWarmStorageService.TransactOpts, dataSetId, arg1, arg2)
}

// DataSetDeleted is a paid mutator transaction binding the contract method 0x2abd465c.
//
// Solidity: function dataSetDeleted(uint256 dataSetId, uint256 , bytes ) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) DataSetDeleted(dataSetId *big.Int, arg1 *big.Int, arg2 []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.DataSetDeleted(&_FilecoinWarmStorageService.TransactOpts, dataSetId, arg1, arg2)
}

// Initialize is a paid mutator transaction binding the contract method 0x46614302.
//
// Solidity: function initialize(uint64 _maxProvingPeriod, uint256 _challengeWindowSize, address _filBeamControllerAddress, string _name, string _description) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) Initialize(opts *bind.TransactOpts, _maxProvingPeriod uint64, _challengeWindowSize *big.Int, _filBeamControllerAddress common.Address, _name string, _description string) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "initialize", _maxProvingPeriod, _challengeWindowSize, _filBeamControllerAddress, _name, _description)
}

// Initialize is a paid mutator transaction binding the contract method 0x46614302.
//
// Solidity: function initialize(uint64 _maxProvingPeriod, uint256 _challengeWindowSize, address _filBeamControllerAddress, string _name, string _description) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) Initialize(_maxProvingPeriod uint64, _challengeWindowSize *big.Int, _filBeamControllerAddress common.Address, _name string, _description string) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.Initialize(&_FilecoinWarmStorageService.TransactOpts, _maxProvingPeriod, _challengeWindowSize, _filBeamControllerAddress, _name, _description)
}

// Initialize is a paid mutator transaction binding the contract method 0x46614302.
//
// Solidity: function initialize(uint64 _maxProvingPeriod, uint256 _challengeWindowSize, address _filBeamControllerAddress, string _name, string _description) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) Initialize(_maxProvingPeriod uint64, _challengeWindowSize *big.Int, _filBeamControllerAddress common.Address, _name string, _description string) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.Initialize(&_FilecoinWarmStorageService.TransactOpts, _maxProvingPeriod, _challengeWindowSize, _filBeamControllerAddress, _name, _description)
}

// Migrate is a paid mutator transaction binding the contract method 0xce5494bb.
//
// Solidity: function migrate(address _viewContract) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) Migrate(opts *bind.TransactOpts, _viewContract common.Address) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "migrate", _viewContract)
}

// Migrate is a paid mutator transaction binding the contract method 0xce5494bb.
//
// Solidity: function migrate(address _viewContract) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) Migrate(_viewContract common.Address) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.Migrate(&_FilecoinWarmStorageService.TransactOpts, _viewContract)
}

// Migrate is a paid mutator transaction binding the contract method 0xce5494bb.
//
// Solidity: function migrate(address _viewContract) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) Migrate(_viewContract common.Address) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.Migrate(&_FilecoinWarmStorageService.TransactOpts, _viewContract)
}

// NextProvingPeriod is a paid mutator transaction binding the contract method 0xaa27ebcc.
//
// Solidity: function nextProvingPeriod(uint256 dataSetId, uint256 challengeEpoch, uint256 leafCount, bytes ) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) NextProvingPeriod(opts *bind.TransactOpts, dataSetId *big.Int, challengeEpoch *big.Int, leafCount *big.Int, arg3 []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "nextProvingPeriod", dataSetId, challengeEpoch, leafCount, arg3)
}

// NextProvingPeriod is a paid mutator transaction binding the contract method 0xaa27ebcc.
//
// Solidity: function nextProvingPeriod(uint256 dataSetId, uint256 challengeEpoch, uint256 leafCount, bytes ) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) NextProvingPeriod(dataSetId *big.Int, challengeEpoch *big.Int, leafCount *big.Int, arg3 []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.NextProvingPeriod(&_FilecoinWarmStorageService.TransactOpts, dataSetId, challengeEpoch, leafCount, arg3)
}

// NextProvingPeriod is a paid mutator transaction binding the contract method 0xaa27ebcc.
//
// Solidity: function nextProvingPeriod(uint256 dataSetId, uint256 challengeEpoch, uint256 leafCount, bytes ) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) NextProvingPeriod(dataSetId *big.Int, challengeEpoch *big.Int, leafCount *big.Int, arg3 []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.NextProvingPeriod(&_FilecoinWarmStorageService.TransactOpts, dataSetId, challengeEpoch, leafCount, arg3)
}

// PiecesAdded is a paid mutator transaction binding the contract method 0xf6814d79.
//
// Solidity: function piecesAdded(uint256 dataSetId, uint256 firstAdded, (bytes)[] pieceData, bytes extraData) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) PiecesAdded(opts *bind.TransactOpts, dataSetId *big.Int, firstAdded *big.Int, pieceData []CidsCid, extraData []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "piecesAdded", dataSetId, firstAdded, pieceData, extraData)
}

// PiecesAdded is a paid mutator transaction binding the contract method 0xf6814d79.
//
// Solidity: function piecesAdded(uint256 dataSetId, uint256 firstAdded, (bytes)[] pieceData, bytes extraData) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) PiecesAdded(dataSetId *big.Int, firstAdded *big.Int, pieceData []CidsCid, extraData []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.PiecesAdded(&_FilecoinWarmStorageService.TransactOpts, dataSetId, firstAdded, pieceData, extraData)
}

// PiecesAdded is a paid mutator transaction binding the contract method 0xf6814d79.
//
// Solidity: function piecesAdded(uint256 dataSetId, uint256 firstAdded, (bytes)[] pieceData, bytes extraData) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) PiecesAdded(dataSetId *big.Int, firstAdded *big.Int, pieceData []CidsCid, extraData []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.PiecesAdded(&_FilecoinWarmStorageService.TransactOpts, dataSetId, firstAdded, pieceData, extraData)
}

// PiecesScheduledRemove is a paid mutator transaction binding the contract method 0xe7954aa7.
//
// Solidity: function piecesScheduledRemove(uint256 dataSetId, uint256[] pieceIds, bytes extraData) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) PiecesScheduledRemove(opts *bind.TransactOpts, dataSetId *big.Int, pieceIds []*big.Int, extraData []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "piecesScheduledRemove", dataSetId, pieceIds, extraData)
}

// PiecesScheduledRemove is a paid mutator transaction binding the contract method 0xe7954aa7.
//
// Solidity: function piecesScheduledRemove(uint256 dataSetId, uint256[] pieceIds, bytes extraData) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) PiecesScheduledRemove(dataSetId *big.Int, pieceIds []*big.Int, extraData []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.PiecesScheduledRemove(&_FilecoinWarmStorageService.TransactOpts, dataSetId, pieceIds, extraData)
}

// PiecesScheduledRemove is a paid mutator transaction binding the contract method 0xe7954aa7.
//
// Solidity: function piecesScheduledRemove(uint256 dataSetId, uint256[] pieceIds, bytes extraData) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) PiecesScheduledRemove(dataSetId *big.Int, pieceIds []*big.Int, extraData []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.PiecesScheduledRemove(&_FilecoinWarmStorageService.TransactOpts, dataSetId, pieceIds, extraData)
}

// PossessionProven is a paid mutator transaction binding the contract method 0x356de02b.
//
// Solidity: function possessionProven(uint256 dataSetId, uint256 , uint256 , uint256 challengeCount) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) PossessionProven(opts *bind.TransactOpts, dataSetId *big.Int, arg1 *big.Int, arg2 *big.Int, challengeCount *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "possessionProven", dataSetId, arg1, arg2, challengeCount)
}

// PossessionProven is a paid mutator transaction binding the contract method 0x356de02b.
//
// Solidity: function possessionProven(uint256 dataSetId, uint256 , uint256 , uint256 challengeCount) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) PossessionProven(dataSetId *big.Int, arg1 *big.Int, arg2 *big.Int, challengeCount *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.PossessionProven(&_FilecoinWarmStorageService.TransactOpts, dataSetId, arg1, arg2, challengeCount)
}

// PossessionProven is a paid mutator transaction binding the contract method 0x356de02b.
//
// Solidity: function possessionProven(uint256 dataSetId, uint256 , uint256 , uint256 challengeCount) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) PossessionProven(dataSetId *big.Int, arg1 *big.Int, arg2 *big.Int, challengeCount *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.PossessionProven(&_FilecoinWarmStorageService.TransactOpts, dataSetId, arg1, arg2, challengeCount)
}

// RailTerminated is a paid mutator transaction binding the contract method 0xc5153f70.
//
// Solidity: function railTerminated(uint256 railId, address terminator, uint256 endEpoch) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) RailTerminated(opts *bind.TransactOpts, railId *big.Int, terminator common.Address, endEpoch *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "railTerminated", railId, terminator, endEpoch)
}

// RailTerminated is a paid mutator transaction binding the contract method 0xc5153f70.
//
// Solidity: function railTerminated(uint256 railId, address terminator, uint256 endEpoch) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) RailTerminated(railId *big.Int, terminator common.Address, endEpoch *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.RailTerminated(&_FilecoinWarmStorageService.TransactOpts, railId, terminator, endEpoch)
}

// RailTerminated is a paid mutator transaction binding the contract method 0xc5153f70.
//
// Solidity: function railTerminated(uint256 railId, address terminator, uint256 endEpoch) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) RailTerminated(railId *big.Int, terminator common.Address, endEpoch *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.RailTerminated(&_FilecoinWarmStorageService.TransactOpts, railId, terminator, endEpoch)
}

// RemoveApprovedProvider is a paid mutator transaction binding the contract method 0x5840b83d.
//
// Solidity: function removeApprovedProvider(uint256 providerId, uint256 index) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) RemoveApprovedProvider(opts *bind.TransactOpts, providerId *big.Int, index *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "removeApprovedProvider", providerId, index)
}

// RemoveApprovedProvider is a paid mutator transaction binding the contract method 0x5840b83d.
//
// Solidity: function removeApprovedProvider(uint256 providerId, uint256 index) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) RemoveApprovedProvider(providerId *big.Int, index *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.RemoveApprovedProvider(&_FilecoinWarmStorageService.TransactOpts, providerId, index)
}

// RemoveApprovedProvider is a paid mutator transaction binding the contract method 0x5840b83d.
//
// Solidity: function removeApprovedProvider(uint256 providerId, uint256 index) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) RemoveApprovedProvider(providerId *big.Int, index *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.RemoveApprovedProvider(&_FilecoinWarmStorageService.TransactOpts, providerId, index)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) RenounceOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "renounceOwnership")
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) RenounceOwnership() (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.RenounceOwnership(&_FilecoinWarmStorageService.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) RenounceOwnership() (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.RenounceOwnership(&_FilecoinWarmStorageService.TransactOpts)
}

// SetViewContract is a paid mutator transaction binding the contract method 0x7f6330a1.
//
// Solidity: function setViewContract(address _viewContract) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) SetViewContract(opts *bind.TransactOpts, _viewContract common.Address) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "setViewContract", _viewContract)
}

// SetViewContract is a paid mutator transaction binding the contract method 0x7f6330a1.
//
// Solidity: function setViewContract(address _viewContract) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) SetViewContract(_viewContract common.Address) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.SetViewContract(&_FilecoinWarmStorageService.TransactOpts, _viewContract)
}

// SetViewContract is a paid mutator transaction binding the contract method 0x7f6330a1.
//
// Solidity: function setViewContract(address _viewContract) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) SetViewContract(_viewContract common.Address) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.SetViewContract(&_FilecoinWarmStorageService.TransactOpts, _viewContract)
}

// SettleFilBeamPaymentRails is a paid mutator transaction binding the contract method 0x3615edff.
//
// Solidity: function settleFilBeamPaymentRails(uint256 dataSetId, uint256 cdnAmount, uint256 cacheMissAmount) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) SettleFilBeamPaymentRails(opts *bind.TransactOpts, dataSetId *big.Int, cdnAmount *big.Int, cacheMissAmount *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "settleFilBeamPaymentRails", dataSetId, cdnAmount, cacheMissAmount)
}

// SettleFilBeamPaymentRails is a paid mutator transaction binding the contract method 0x3615edff.
//
// Solidity: function settleFilBeamPaymentRails(uint256 dataSetId, uint256 cdnAmount, uint256 cacheMissAmount) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) SettleFilBeamPaymentRails(dataSetId *big.Int, cdnAmount *big.Int, cacheMissAmount *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.SettleFilBeamPaymentRails(&_FilecoinWarmStorageService.TransactOpts, dataSetId, cdnAmount, cacheMissAmount)
}

// SettleFilBeamPaymentRails is a paid mutator transaction binding the contract method 0x3615edff.
//
// Solidity: function settleFilBeamPaymentRails(uint256 dataSetId, uint256 cdnAmount, uint256 cacheMissAmount) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) SettleFilBeamPaymentRails(dataSetId *big.Int, cdnAmount *big.Int, cacheMissAmount *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.SettleFilBeamPaymentRails(&_FilecoinWarmStorageService.TransactOpts, dataSetId, cdnAmount, cacheMissAmount)
}

// StorageProviderChanged is a paid mutator transaction binding the contract method 0x4059b6d7.
//
// Solidity: function storageProviderChanged(uint256 , address , address , bytes ) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) StorageProviderChanged(opts *bind.TransactOpts, arg0 *big.Int, arg1 common.Address, arg2 common.Address, arg3 []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "storageProviderChanged", arg0, arg1, arg2, arg3)
}

// StorageProviderChanged is a paid mutator transaction binding the contract method 0x4059b6d7.
//
// Solidity: function storageProviderChanged(uint256 , address , address , bytes ) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) StorageProviderChanged(arg0 *big.Int, arg1 common.Address, arg2 common.Address, arg3 []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.StorageProviderChanged(&_FilecoinWarmStorageService.TransactOpts, arg0, arg1, arg2, arg3)
}

// StorageProviderChanged is a paid mutator transaction binding the contract method 0x4059b6d7.
//
// Solidity: function storageProviderChanged(uint256 , address , address , bytes ) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) StorageProviderChanged(arg0 *big.Int, arg1 common.Address, arg2 common.Address, arg3 []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.StorageProviderChanged(&_FilecoinWarmStorageService.TransactOpts, arg0, arg1, arg2, arg3)
}

// TerminateCDNService is a paid mutator transaction binding the contract method 0x648564c0.
//
// Solidity: function terminateCDNService(uint256 dataSetId) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) TerminateCDNService(opts *bind.TransactOpts, dataSetId *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "terminateCDNService", dataSetId)
}

// TerminateCDNService is a paid mutator transaction binding the contract method 0x648564c0.
//
// Solidity: function terminateCDNService(uint256 dataSetId) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) TerminateCDNService(dataSetId *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.TerminateCDNService(&_FilecoinWarmStorageService.TransactOpts, dataSetId)
}

// TerminateCDNService is a paid mutator transaction binding the contract method 0x648564c0.
//
// Solidity: function terminateCDNService(uint256 dataSetId) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) TerminateCDNService(dataSetId *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.TerminateCDNService(&_FilecoinWarmStorageService.TransactOpts, dataSetId)
}

// TerminateService is a paid mutator transaction binding the contract method 0xb997a71e.
//
// Solidity: function terminateService(uint256 dataSetId) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) TerminateService(opts *bind.TransactOpts, dataSetId *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "terminateService", dataSetId)
}

// TerminateService is a paid mutator transaction binding the contract method 0xb997a71e.
//
// Solidity: function terminateService(uint256 dataSetId) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) TerminateService(dataSetId *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.TerminateService(&_FilecoinWarmStorageService.TransactOpts, dataSetId)
}

// TerminateService is a paid mutator transaction binding the contract method 0xb997a71e.
//
// Solidity: function terminateService(uint256 dataSetId) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) TerminateService(dataSetId *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.TerminateService(&_FilecoinWarmStorageService.TransactOpts, dataSetId)
}

// TopUpCDNPaymentRails is a paid mutator transaction binding the contract method 0xeb561d9c.
//
// Solidity: function topUpCDNPaymentRails(uint256 dataSetId, uint256 cdnAmountToAdd, uint256 cacheMissAmountToAdd) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) TopUpCDNPaymentRails(opts *bind.TransactOpts, dataSetId *big.Int, cdnAmountToAdd *big.Int, cacheMissAmountToAdd *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "topUpCDNPaymentRails", dataSetId, cdnAmountToAdd, cacheMissAmountToAdd)
}

// TopUpCDNPaymentRails is a paid mutator transaction binding the contract method 0xeb561d9c.
//
// Solidity: function topUpCDNPaymentRails(uint256 dataSetId, uint256 cdnAmountToAdd, uint256 cacheMissAmountToAdd) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) TopUpCDNPaymentRails(dataSetId *big.Int, cdnAmountToAdd *big.Int, cacheMissAmountToAdd *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.TopUpCDNPaymentRails(&_FilecoinWarmStorageService.TransactOpts, dataSetId, cdnAmountToAdd, cacheMissAmountToAdd)
}

// TopUpCDNPaymentRails is a paid mutator transaction binding the contract method 0xeb561d9c.
//
// Solidity: function topUpCDNPaymentRails(uint256 dataSetId, uint256 cdnAmountToAdd, uint256 cacheMissAmountToAdd) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) TopUpCDNPaymentRails(dataSetId *big.Int, cdnAmountToAdd *big.Int, cacheMissAmountToAdd *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.TopUpCDNPaymentRails(&_FilecoinWarmStorageService.TransactOpts, dataSetId, cdnAmountToAdd, cacheMissAmountToAdd)
}

// TransferFilBeamController is a paid mutator transaction binding the contract method 0x5e786446.
//
// Solidity: function transferFilBeamController(address newController) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) TransferFilBeamController(opts *bind.TransactOpts, newController common.Address) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "transferFilBeamController", newController)
}

// TransferFilBeamController is a paid mutator transaction binding the contract method 0x5e786446.
//
// Solidity: function transferFilBeamController(address newController) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) TransferFilBeamController(newController common.Address) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.TransferFilBeamController(&_FilecoinWarmStorageService.TransactOpts, newController)
}

// TransferFilBeamController is a paid mutator transaction binding the contract method 0x5e786446.
//
// Solidity: function transferFilBeamController(address newController) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) TransferFilBeamController(newController common.Address) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.TransferFilBeamController(&_FilecoinWarmStorageService.TransactOpts, newController)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.TransferOwnership(&_FilecoinWarmStorageService.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.TransferOwnership(&_FilecoinWarmStorageService.TransactOpts, newOwner)
}

// UpdateServiceCommission is a paid mutator transaction binding the contract method 0x662ed4b6.
//
// Solidity: function updateServiceCommission(uint256 newCommissionBps) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) UpdateServiceCommission(opts *bind.TransactOpts, newCommissionBps *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "updateServiceCommission", newCommissionBps)
}

// UpdateServiceCommission is a paid mutator transaction binding the contract method 0x662ed4b6.
//
// Solidity: function updateServiceCommission(uint256 newCommissionBps) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) UpdateServiceCommission(newCommissionBps *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.UpdateServiceCommission(&_FilecoinWarmStorageService.TransactOpts, newCommissionBps)
}

// UpdateServiceCommission is a paid mutator transaction binding the contract method 0x662ed4b6.
//
// Solidity: function updateServiceCommission(uint256 newCommissionBps) returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) UpdateServiceCommission(newCommissionBps *big.Int) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.UpdateServiceCommission(&_FilecoinWarmStorageService.TransactOpts, newCommissionBps)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactor) UpgradeToAndCall(opts *bind.TransactOpts, newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.contract.Transact(opts, "upgradeToAndCall", newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.UpgradeToAndCall(&_FilecoinWarmStorageService.TransactOpts, newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceTransactorSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _FilecoinWarmStorageService.Contract.UpgradeToAndCall(&_FilecoinWarmStorageService.TransactOpts, newImplementation, data)
}

// FilecoinWarmStorageServiceCDNPaymentRailsToppedUpIterator is returned from FilterCDNPaymentRailsToppedUp and is used to iterate over the raw logs and unpacked data for CDNPaymentRailsToppedUp events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceCDNPaymentRailsToppedUpIterator struct {
	Event *FilecoinWarmStorageServiceCDNPaymentRailsToppedUp // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceCDNPaymentRailsToppedUpIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceCDNPaymentRailsToppedUp)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceCDNPaymentRailsToppedUp)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceCDNPaymentRailsToppedUpIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceCDNPaymentRailsToppedUpIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceCDNPaymentRailsToppedUp represents a CDNPaymentRailsToppedUp event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceCDNPaymentRailsToppedUp struct {
	DataSetId            *big.Int
	CdnAmountAdded       *big.Int
	TotalCdnLockup       *big.Int
	CacheMissAmountAdded *big.Int
	TotalCacheMissLockup *big.Int
	Raw                  types.Log // Blockchain specific contextual infos
}

// FilterCDNPaymentRailsToppedUp is a free log retrieval operation binding the contract event 0x6b6e3adced39b19ee0a9f68ef785f7275ed75801e5f126964678fdf0f0552711.
//
// Solidity: event CDNPaymentRailsToppedUp(uint256 indexed dataSetId, uint256 cdnAmountAdded, uint256 totalCdnLockup, uint256 cacheMissAmountAdded, uint256 totalCacheMissLockup)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterCDNPaymentRailsToppedUp(opts *bind.FilterOpts, dataSetId []*big.Int) (*FilecoinWarmStorageServiceCDNPaymentRailsToppedUpIterator, error) {

	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "CDNPaymentRailsToppedUp", dataSetIdRule)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceCDNPaymentRailsToppedUpIterator{contract: _FilecoinWarmStorageService.contract, event: "CDNPaymentRailsToppedUp", logs: logs, sub: sub}, nil
}

// WatchCDNPaymentRailsToppedUp is a free log subscription operation binding the contract event 0x6b6e3adced39b19ee0a9f68ef785f7275ed75801e5f126964678fdf0f0552711.
//
// Solidity: event CDNPaymentRailsToppedUp(uint256 indexed dataSetId, uint256 cdnAmountAdded, uint256 totalCdnLockup, uint256 cacheMissAmountAdded, uint256 totalCacheMissLockup)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchCDNPaymentRailsToppedUp(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceCDNPaymentRailsToppedUp, dataSetId []*big.Int) (event.Subscription, error) {

	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "CDNPaymentRailsToppedUp", dataSetIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceCDNPaymentRailsToppedUp)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "CDNPaymentRailsToppedUp", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseCDNPaymentRailsToppedUp is a log parse operation binding the contract event 0x6b6e3adced39b19ee0a9f68ef785f7275ed75801e5f126964678fdf0f0552711.
//
// Solidity: event CDNPaymentRailsToppedUp(uint256 indexed dataSetId, uint256 cdnAmountAdded, uint256 totalCdnLockup, uint256 cacheMissAmountAdded, uint256 totalCacheMissLockup)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseCDNPaymentRailsToppedUp(log types.Log) (*FilecoinWarmStorageServiceCDNPaymentRailsToppedUp, error) {
	event := new(FilecoinWarmStorageServiceCDNPaymentRailsToppedUp)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "CDNPaymentRailsToppedUp", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceCDNPaymentTerminatedIterator is returned from FilterCDNPaymentTerminated and is used to iterate over the raw logs and unpacked data for CDNPaymentTerminated events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceCDNPaymentTerminatedIterator struct {
	Event *FilecoinWarmStorageServiceCDNPaymentTerminated // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceCDNPaymentTerminatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceCDNPaymentTerminated)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceCDNPaymentTerminated)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceCDNPaymentTerminatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceCDNPaymentTerminatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceCDNPaymentTerminated represents a CDNPaymentTerminated event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceCDNPaymentTerminated struct {
	DataSetId       *big.Int
	EndEpoch        *big.Int
	CacheMissRailId *big.Int
	CdnRailId       *big.Int
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterCDNPaymentTerminated is a free log retrieval operation binding the contract event 0xe8ae13ddeff1f075e7621cd59b2672919372cc6a0f69198a5eb5af0e42294a80.
//
// Solidity: event CDNPaymentTerminated(uint256 indexed dataSetId, uint256 endEpoch, uint256 cacheMissRailId, uint256 cdnRailId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterCDNPaymentTerminated(opts *bind.FilterOpts, dataSetId []*big.Int) (*FilecoinWarmStorageServiceCDNPaymentTerminatedIterator, error) {

	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "CDNPaymentTerminated", dataSetIdRule)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceCDNPaymentTerminatedIterator{contract: _FilecoinWarmStorageService.contract, event: "CDNPaymentTerminated", logs: logs, sub: sub}, nil
}

// WatchCDNPaymentTerminated is a free log subscription operation binding the contract event 0xe8ae13ddeff1f075e7621cd59b2672919372cc6a0f69198a5eb5af0e42294a80.
//
// Solidity: event CDNPaymentTerminated(uint256 indexed dataSetId, uint256 endEpoch, uint256 cacheMissRailId, uint256 cdnRailId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchCDNPaymentTerminated(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceCDNPaymentTerminated, dataSetId []*big.Int) (event.Subscription, error) {

	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "CDNPaymentTerminated", dataSetIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceCDNPaymentTerminated)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "CDNPaymentTerminated", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseCDNPaymentTerminated is a log parse operation binding the contract event 0xe8ae13ddeff1f075e7621cd59b2672919372cc6a0f69198a5eb5af0e42294a80.
//
// Solidity: event CDNPaymentTerminated(uint256 indexed dataSetId, uint256 endEpoch, uint256 cacheMissRailId, uint256 cdnRailId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseCDNPaymentTerminated(log types.Log) (*FilecoinWarmStorageServiceCDNPaymentTerminated, error) {
	event := new(FilecoinWarmStorageServiceCDNPaymentTerminated)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "CDNPaymentTerminated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceCDNServiceTerminatedIterator is returned from FilterCDNServiceTerminated and is used to iterate over the raw logs and unpacked data for CDNServiceTerminated events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceCDNServiceTerminatedIterator struct {
	Event *FilecoinWarmStorageServiceCDNServiceTerminated // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceCDNServiceTerminatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceCDNServiceTerminated)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceCDNServiceTerminated)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceCDNServiceTerminatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceCDNServiceTerminatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceCDNServiceTerminated represents a CDNServiceTerminated event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceCDNServiceTerminated struct {
	Caller          common.Address
	DataSetId       *big.Int
	CacheMissRailId *big.Int
	CdnRailId       *big.Int
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterCDNServiceTerminated is a free log retrieval operation binding the contract event 0xe050575f2f51273412c3b1a9a74ce3a2abc98172b48f6d19442de80a3744367d.
//
// Solidity: event CDNServiceTerminated(address indexed caller, uint256 indexed dataSetId, uint256 cacheMissRailId, uint256 cdnRailId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterCDNServiceTerminated(opts *bind.FilterOpts, caller []common.Address, dataSetId []*big.Int) (*FilecoinWarmStorageServiceCDNServiceTerminatedIterator, error) {

	var callerRule []interface{}
	for _, callerItem := range caller {
		callerRule = append(callerRule, callerItem)
	}
	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "CDNServiceTerminated", callerRule, dataSetIdRule)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceCDNServiceTerminatedIterator{contract: _FilecoinWarmStorageService.contract, event: "CDNServiceTerminated", logs: logs, sub: sub}, nil
}

// WatchCDNServiceTerminated is a free log subscription operation binding the contract event 0xe050575f2f51273412c3b1a9a74ce3a2abc98172b48f6d19442de80a3744367d.
//
// Solidity: event CDNServiceTerminated(address indexed caller, uint256 indexed dataSetId, uint256 cacheMissRailId, uint256 cdnRailId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchCDNServiceTerminated(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceCDNServiceTerminated, caller []common.Address, dataSetId []*big.Int) (event.Subscription, error) {

	var callerRule []interface{}
	for _, callerItem := range caller {
		callerRule = append(callerRule, callerItem)
	}
	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "CDNServiceTerminated", callerRule, dataSetIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceCDNServiceTerminated)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "CDNServiceTerminated", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseCDNServiceTerminated is a log parse operation binding the contract event 0xe050575f2f51273412c3b1a9a74ce3a2abc98172b48f6d19442de80a3744367d.
//
// Solidity: event CDNServiceTerminated(address indexed caller, uint256 indexed dataSetId, uint256 cacheMissRailId, uint256 cdnRailId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseCDNServiceTerminated(log types.Log) (*FilecoinWarmStorageServiceCDNServiceTerminated, error) {
	event := new(FilecoinWarmStorageServiceCDNServiceTerminated)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "CDNServiceTerminated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceContractUpgradedIterator is returned from FilterContractUpgraded and is used to iterate over the raw logs and unpacked data for ContractUpgraded events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceContractUpgradedIterator struct {
	Event *FilecoinWarmStorageServiceContractUpgraded // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceContractUpgradedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceContractUpgraded)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceContractUpgraded)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceContractUpgradedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceContractUpgradedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceContractUpgraded represents a ContractUpgraded event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceContractUpgraded struct {
	Version        string
	Implementation common.Address
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterContractUpgraded is a free log retrieval operation binding the contract event 0x2b51ff7c4cc8e6fe1c72e9d9685b7d2a88a5d82ad3a644afbdceb0272c89c1c3.
//
// Solidity: event ContractUpgraded(string version, address implementation)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterContractUpgraded(opts *bind.FilterOpts) (*FilecoinWarmStorageServiceContractUpgradedIterator, error) {

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "ContractUpgraded")
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceContractUpgradedIterator{contract: _FilecoinWarmStorageService.contract, event: "ContractUpgraded", logs: logs, sub: sub}, nil
}

// WatchContractUpgraded is a free log subscription operation binding the contract event 0x2b51ff7c4cc8e6fe1c72e9d9685b7d2a88a5d82ad3a644afbdceb0272c89c1c3.
//
// Solidity: event ContractUpgraded(string version, address implementation)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchContractUpgraded(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceContractUpgraded) (event.Subscription, error) {

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "ContractUpgraded")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceContractUpgraded)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "ContractUpgraded", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseContractUpgraded is a log parse operation binding the contract event 0x2b51ff7c4cc8e6fe1c72e9d9685b7d2a88a5d82ad3a644afbdceb0272c89c1c3.
//
// Solidity: event ContractUpgraded(string version, address implementation)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseContractUpgraded(log types.Log) (*FilecoinWarmStorageServiceContractUpgraded, error) {
	event := new(FilecoinWarmStorageServiceContractUpgraded)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "ContractUpgraded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceDataSetCreatedIterator is returned from FilterDataSetCreated and is used to iterate over the raw logs and unpacked data for DataSetCreated events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceDataSetCreatedIterator struct {
	Event *FilecoinWarmStorageServiceDataSetCreated // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceDataSetCreatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceDataSetCreated)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceDataSetCreated)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceDataSetCreatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceDataSetCreatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceDataSetCreated represents a DataSetCreated event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceDataSetCreated struct {
	DataSetId       *big.Int
	ProviderId      *big.Int
	PdpRailId       *big.Int
	CacheMissRailId *big.Int
	CdnRailId       *big.Int
	Payer           common.Address
	ServiceProvider common.Address
	Payee           common.Address
	MetadataKeys    []string
	MetadataValues  []string
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterDataSetCreated is a free log retrieval operation binding the contract event 0xc90cb3863281dc6e2e16e74064ed2e0ab91144ccfe5c3492b8c33f58fe90d0db.
//
// Solidity: event DataSetCreated(uint256 indexed dataSetId, uint256 indexed providerId, uint256 pdpRailId, uint256 cacheMissRailId, uint256 cdnRailId, address payer, address serviceProvider, address payee, string[] metadataKeys, string[] metadataValues)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterDataSetCreated(opts *bind.FilterOpts, dataSetId []*big.Int, providerId []*big.Int) (*FilecoinWarmStorageServiceDataSetCreatedIterator, error) {

	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}
	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "DataSetCreated", dataSetIdRule, providerIdRule)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceDataSetCreatedIterator{contract: _FilecoinWarmStorageService.contract, event: "DataSetCreated", logs: logs, sub: sub}, nil
}

// WatchDataSetCreated is a free log subscription operation binding the contract event 0xc90cb3863281dc6e2e16e74064ed2e0ab91144ccfe5c3492b8c33f58fe90d0db.
//
// Solidity: event DataSetCreated(uint256 indexed dataSetId, uint256 indexed providerId, uint256 pdpRailId, uint256 cacheMissRailId, uint256 cdnRailId, address payer, address serviceProvider, address payee, string[] metadataKeys, string[] metadataValues)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchDataSetCreated(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceDataSetCreated, dataSetId []*big.Int, providerId []*big.Int) (event.Subscription, error) {

	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}
	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "DataSetCreated", dataSetIdRule, providerIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceDataSetCreated)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "DataSetCreated", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseDataSetCreated is a log parse operation binding the contract event 0xc90cb3863281dc6e2e16e74064ed2e0ab91144ccfe5c3492b8c33f58fe90d0db.
//
// Solidity: event DataSetCreated(uint256 indexed dataSetId, uint256 indexed providerId, uint256 pdpRailId, uint256 cacheMissRailId, uint256 cdnRailId, address payer, address serviceProvider, address payee, string[] metadataKeys, string[] metadataValues)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseDataSetCreated(log types.Log) (*FilecoinWarmStorageServiceDataSetCreated, error) {
	event := new(FilecoinWarmStorageServiceDataSetCreated)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "DataSetCreated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceDataSetServiceProviderChangedIterator is returned from FilterDataSetServiceProviderChanged and is used to iterate over the raw logs and unpacked data for DataSetServiceProviderChanged events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceDataSetServiceProviderChangedIterator struct {
	Event *FilecoinWarmStorageServiceDataSetServiceProviderChanged // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceDataSetServiceProviderChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceDataSetServiceProviderChanged)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceDataSetServiceProviderChanged)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceDataSetServiceProviderChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceDataSetServiceProviderChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceDataSetServiceProviderChanged represents a DataSetServiceProviderChanged event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceDataSetServiceProviderChanged struct {
	DataSetId          *big.Int
	OldServiceProvider common.Address
	NewServiceProvider common.Address
	Raw                types.Log // Blockchain specific contextual infos
}

// FilterDataSetServiceProviderChanged is a free log retrieval operation binding the contract event 0x6bf4c2a87885bf6d2d69480d1835a60db52c95621e8b958542cfcdc1350ea991.
//
// Solidity: event DataSetServiceProviderChanged(uint256 indexed dataSetId, address indexed oldServiceProvider, address indexed newServiceProvider)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterDataSetServiceProviderChanged(opts *bind.FilterOpts, dataSetId []*big.Int, oldServiceProvider []common.Address, newServiceProvider []common.Address) (*FilecoinWarmStorageServiceDataSetServiceProviderChangedIterator, error) {

	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}
	var oldServiceProviderRule []interface{}
	for _, oldServiceProviderItem := range oldServiceProvider {
		oldServiceProviderRule = append(oldServiceProviderRule, oldServiceProviderItem)
	}
	var newServiceProviderRule []interface{}
	for _, newServiceProviderItem := range newServiceProvider {
		newServiceProviderRule = append(newServiceProviderRule, newServiceProviderItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "DataSetServiceProviderChanged", dataSetIdRule, oldServiceProviderRule, newServiceProviderRule)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceDataSetServiceProviderChangedIterator{contract: _FilecoinWarmStorageService.contract, event: "DataSetServiceProviderChanged", logs: logs, sub: sub}, nil
}

// WatchDataSetServiceProviderChanged is a free log subscription operation binding the contract event 0x6bf4c2a87885bf6d2d69480d1835a60db52c95621e8b958542cfcdc1350ea991.
//
// Solidity: event DataSetServiceProviderChanged(uint256 indexed dataSetId, address indexed oldServiceProvider, address indexed newServiceProvider)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchDataSetServiceProviderChanged(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceDataSetServiceProviderChanged, dataSetId []*big.Int, oldServiceProvider []common.Address, newServiceProvider []common.Address) (event.Subscription, error) {

	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}
	var oldServiceProviderRule []interface{}
	for _, oldServiceProviderItem := range oldServiceProvider {
		oldServiceProviderRule = append(oldServiceProviderRule, oldServiceProviderItem)
	}
	var newServiceProviderRule []interface{}
	for _, newServiceProviderItem := range newServiceProvider {
		newServiceProviderRule = append(newServiceProviderRule, newServiceProviderItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "DataSetServiceProviderChanged", dataSetIdRule, oldServiceProviderRule, newServiceProviderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceDataSetServiceProviderChanged)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "DataSetServiceProviderChanged", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseDataSetServiceProviderChanged is a log parse operation binding the contract event 0x6bf4c2a87885bf6d2d69480d1835a60db52c95621e8b958542cfcdc1350ea991.
//
// Solidity: event DataSetServiceProviderChanged(uint256 indexed dataSetId, address indexed oldServiceProvider, address indexed newServiceProvider)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseDataSetServiceProviderChanged(log types.Log) (*FilecoinWarmStorageServiceDataSetServiceProviderChanged, error) {
	event := new(FilecoinWarmStorageServiceDataSetServiceProviderChanged)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "DataSetServiceProviderChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceEIP712DomainChangedIterator is returned from FilterEIP712DomainChanged and is used to iterate over the raw logs and unpacked data for EIP712DomainChanged events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceEIP712DomainChangedIterator struct {
	Event *FilecoinWarmStorageServiceEIP712DomainChanged // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceEIP712DomainChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceEIP712DomainChanged)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceEIP712DomainChanged)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceEIP712DomainChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceEIP712DomainChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceEIP712DomainChanged represents a EIP712DomainChanged event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceEIP712DomainChanged struct {
	Raw types.Log // Blockchain specific contextual infos
}

// FilterEIP712DomainChanged is a free log retrieval operation binding the contract event 0x0a6387c9ea3628b88a633bb4f3b151770f70085117a15f9bf3787cda53f13d31.
//
// Solidity: event EIP712DomainChanged()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterEIP712DomainChanged(opts *bind.FilterOpts) (*FilecoinWarmStorageServiceEIP712DomainChangedIterator, error) {

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "EIP712DomainChanged")
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceEIP712DomainChangedIterator{contract: _FilecoinWarmStorageService.contract, event: "EIP712DomainChanged", logs: logs, sub: sub}, nil
}

// WatchEIP712DomainChanged is a free log subscription operation binding the contract event 0x0a6387c9ea3628b88a633bb4f3b151770f70085117a15f9bf3787cda53f13d31.
//
// Solidity: event EIP712DomainChanged()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchEIP712DomainChanged(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceEIP712DomainChanged) (event.Subscription, error) {

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "EIP712DomainChanged")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceEIP712DomainChanged)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "EIP712DomainChanged", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseEIP712DomainChanged is a log parse operation binding the contract event 0x0a6387c9ea3628b88a633bb4f3b151770f70085117a15f9bf3787cda53f13d31.
//
// Solidity: event EIP712DomainChanged()
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseEIP712DomainChanged(log types.Log) (*FilecoinWarmStorageServiceEIP712DomainChanged, error) {
	event := new(FilecoinWarmStorageServiceEIP712DomainChanged)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "EIP712DomainChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceFaultRecordIterator is returned from FilterFaultRecord and is used to iterate over the raw logs and unpacked data for FaultRecord events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceFaultRecordIterator struct {
	Event *FilecoinWarmStorageServiceFaultRecord // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceFaultRecordIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceFaultRecord)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceFaultRecord)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceFaultRecordIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceFaultRecordIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceFaultRecord represents a FaultRecord event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceFaultRecord struct {
	DataSetId      *big.Int
	PeriodsFaulted *big.Int
	Deadline       *big.Int
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterFaultRecord is a free log retrieval operation binding the contract event 0xff5f076c63706be9f7eaafa8329db4a9ce9b9e3cd6e53470f05491e2043e1a81.
//
// Solidity: event FaultRecord(uint256 indexed dataSetId, uint256 periodsFaulted, uint256 deadline)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterFaultRecord(opts *bind.FilterOpts, dataSetId []*big.Int) (*FilecoinWarmStorageServiceFaultRecordIterator, error) {

	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "FaultRecord", dataSetIdRule)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceFaultRecordIterator{contract: _FilecoinWarmStorageService.contract, event: "FaultRecord", logs: logs, sub: sub}, nil
}

// WatchFaultRecord is a free log subscription operation binding the contract event 0xff5f076c63706be9f7eaafa8329db4a9ce9b9e3cd6e53470f05491e2043e1a81.
//
// Solidity: event FaultRecord(uint256 indexed dataSetId, uint256 periodsFaulted, uint256 deadline)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchFaultRecord(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceFaultRecord, dataSetId []*big.Int) (event.Subscription, error) {

	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "FaultRecord", dataSetIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceFaultRecord)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "FaultRecord", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFaultRecord is a log parse operation binding the contract event 0xff5f076c63706be9f7eaafa8329db4a9ce9b9e3cd6e53470f05491e2043e1a81.
//
// Solidity: event FaultRecord(uint256 indexed dataSetId, uint256 periodsFaulted, uint256 deadline)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseFaultRecord(log types.Log) (*FilecoinWarmStorageServiceFaultRecord, error) {
	event := new(FilecoinWarmStorageServiceFaultRecord)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "FaultRecord", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceFilBeamControllerChangedIterator is returned from FilterFilBeamControllerChanged and is used to iterate over the raw logs and unpacked data for FilBeamControllerChanged events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceFilBeamControllerChangedIterator struct {
	Event *FilecoinWarmStorageServiceFilBeamControllerChanged // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceFilBeamControllerChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceFilBeamControllerChanged)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceFilBeamControllerChanged)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceFilBeamControllerChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceFilBeamControllerChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceFilBeamControllerChanged represents a FilBeamControllerChanged event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceFilBeamControllerChanged struct {
	OldController common.Address
	NewController common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterFilBeamControllerChanged is a free log retrieval operation binding the contract event 0x08d1f43979b2dfd11b4a8873e1df33bb20726f776c16863b31c775ef2a0bf488.
//
// Solidity: event FilBeamControllerChanged(address oldController, address newController)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterFilBeamControllerChanged(opts *bind.FilterOpts) (*FilecoinWarmStorageServiceFilBeamControllerChangedIterator, error) {

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "FilBeamControllerChanged")
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceFilBeamControllerChangedIterator{contract: _FilecoinWarmStorageService.contract, event: "FilBeamControllerChanged", logs: logs, sub: sub}, nil
}

// WatchFilBeamControllerChanged is a free log subscription operation binding the contract event 0x08d1f43979b2dfd11b4a8873e1df33bb20726f776c16863b31c775ef2a0bf488.
//
// Solidity: event FilBeamControllerChanged(address oldController, address newController)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchFilBeamControllerChanged(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceFilBeamControllerChanged) (event.Subscription, error) {

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "FilBeamControllerChanged")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceFilBeamControllerChanged)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "FilBeamControllerChanged", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFilBeamControllerChanged is a log parse operation binding the contract event 0x08d1f43979b2dfd11b4a8873e1df33bb20726f776c16863b31c775ef2a0bf488.
//
// Solidity: event FilBeamControllerChanged(address oldController, address newController)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseFilBeamControllerChanged(log types.Log) (*FilecoinWarmStorageServiceFilBeamControllerChanged, error) {
	event := new(FilecoinWarmStorageServiceFilBeamControllerChanged)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "FilBeamControllerChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceFilecoinServiceDeployedIterator is returned from FilterFilecoinServiceDeployed and is used to iterate over the raw logs and unpacked data for FilecoinServiceDeployed events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceFilecoinServiceDeployedIterator struct {
	Event *FilecoinWarmStorageServiceFilecoinServiceDeployed // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceFilecoinServiceDeployedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceFilecoinServiceDeployed)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceFilecoinServiceDeployed)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceFilecoinServiceDeployedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceFilecoinServiceDeployedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceFilecoinServiceDeployed represents a FilecoinServiceDeployed event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceFilecoinServiceDeployed struct {
	Name        string
	Description string
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterFilecoinServiceDeployed is a free log retrieval operation binding the contract event 0x139babbfe1492fc231f36f2d6e0e2ca503f8c9ebb0c641cffa70facd2ec2e2df.
//
// Solidity: event FilecoinServiceDeployed(string name, string description)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterFilecoinServiceDeployed(opts *bind.FilterOpts) (*FilecoinWarmStorageServiceFilecoinServiceDeployedIterator, error) {

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "FilecoinServiceDeployed")
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceFilecoinServiceDeployedIterator{contract: _FilecoinWarmStorageService.contract, event: "FilecoinServiceDeployed", logs: logs, sub: sub}, nil
}

// WatchFilecoinServiceDeployed is a free log subscription operation binding the contract event 0x139babbfe1492fc231f36f2d6e0e2ca503f8c9ebb0c641cffa70facd2ec2e2df.
//
// Solidity: event FilecoinServiceDeployed(string name, string description)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchFilecoinServiceDeployed(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceFilecoinServiceDeployed) (event.Subscription, error) {

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "FilecoinServiceDeployed")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceFilecoinServiceDeployed)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "FilecoinServiceDeployed", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseFilecoinServiceDeployed is a log parse operation binding the contract event 0x139babbfe1492fc231f36f2d6e0e2ca503f8c9ebb0c641cffa70facd2ec2e2df.
//
// Solidity: event FilecoinServiceDeployed(string name, string description)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseFilecoinServiceDeployed(log types.Log) (*FilecoinWarmStorageServiceFilecoinServiceDeployed, error) {
	event := new(FilecoinWarmStorageServiceFilecoinServiceDeployed)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "FilecoinServiceDeployed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceInitializedIterator is returned from FilterInitialized and is used to iterate over the raw logs and unpacked data for Initialized events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceInitializedIterator struct {
	Event *FilecoinWarmStorageServiceInitialized // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceInitializedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceInitialized)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceInitialized)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceInitializedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceInitializedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceInitialized represents a Initialized event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceInitialized struct {
	Version uint64
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterInitialized is a free log retrieval operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterInitialized(opts *bind.FilterOpts) (*FilecoinWarmStorageServiceInitializedIterator, error) {

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceInitializedIterator{contract: _FilecoinWarmStorageService.contract, event: "Initialized", logs: logs, sub: sub}, nil
}

// WatchInitialized is a free log subscription operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchInitialized(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceInitialized) (event.Subscription, error) {

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceInitialized)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "Initialized", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseInitialized is a log parse operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseInitialized(log types.Log) (*FilecoinWarmStorageServiceInitialized, error) {
	event := new(FilecoinWarmStorageServiceInitialized)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "Initialized", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceOwnershipTransferredIterator struct {
	Event *FilecoinWarmStorageServiceOwnershipTransferred // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceOwnershipTransferred)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceOwnershipTransferred)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceOwnershipTransferred represents a OwnershipTransferred event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*FilecoinWarmStorageServiceOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceOwnershipTransferredIterator{contract: _FilecoinWarmStorageService.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceOwnershipTransferred)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseOwnershipTransferred is a log parse operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseOwnershipTransferred(log types.Log) (*FilecoinWarmStorageServiceOwnershipTransferred, error) {
	event := new(FilecoinWarmStorageServiceOwnershipTransferred)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServicePDPPaymentTerminatedIterator is returned from FilterPDPPaymentTerminated and is used to iterate over the raw logs and unpacked data for PDPPaymentTerminated events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServicePDPPaymentTerminatedIterator struct {
	Event *FilecoinWarmStorageServicePDPPaymentTerminated // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServicePDPPaymentTerminatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServicePDPPaymentTerminated)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServicePDPPaymentTerminated)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServicePDPPaymentTerminatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServicePDPPaymentTerminatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServicePDPPaymentTerminated represents a PDPPaymentTerminated event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServicePDPPaymentTerminated struct {
	DataSetId *big.Int
	EndEpoch  *big.Int
	PdpRailId *big.Int
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterPDPPaymentTerminated is a free log retrieval operation binding the contract event 0x15371708a8f4745aad266e85741738fc10741627fcc63fd79f29843c59bb3eaf.
//
// Solidity: event PDPPaymentTerminated(uint256 indexed dataSetId, uint256 endEpoch, uint256 pdpRailId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterPDPPaymentTerminated(opts *bind.FilterOpts, dataSetId []*big.Int) (*FilecoinWarmStorageServicePDPPaymentTerminatedIterator, error) {

	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "PDPPaymentTerminated", dataSetIdRule)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServicePDPPaymentTerminatedIterator{contract: _FilecoinWarmStorageService.contract, event: "PDPPaymentTerminated", logs: logs, sub: sub}, nil
}

// WatchPDPPaymentTerminated is a free log subscription operation binding the contract event 0x15371708a8f4745aad266e85741738fc10741627fcc63fd79f29843c59bb3eaf.
//
// Solidity: event PDPPaymentTerminated(uint256 indexed dataSetId, uint256 endEpoch, uint256 pdpRailId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchPDPPaymentTerminated(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServicePDPPaymentTerminated, dataSetId []*big.Int) (event.Subscription, error) {

	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "PDPPaymentTerminated", dataSetIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServicePDPPaymentTerminated)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "PDPPaymentTerminated", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParsePDPPaymentTerminated is a log parse operation binding the contract event 0x15371708a8f4745aad266e85741738fc10741627fcc63fd79f29843c59bb3eaf.
//
// Solidity: event PDPPaymentTerminated(uint256 indexed dataSetId, uint256 endEpoch, uint256 pdpRailId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParsePDPPaymentTerminated(log types.Log) (*FilecoinWarmStorageServicePDPPaymentTerminated, error) {
	event := new(FilecoinWarmStorageServicePDPPaymentTerminated)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "PDPPaymentTerminated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServicePieceAddedIterator is returned from FilterPieceAdded and is used to iterate over the raw logs and unpacked data for PieceAdded events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServicePieceAddedIterator struct {
	Event *FilecoinWarmStorageServicePieceAdded // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServicePieceAddedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServicePieceAdded)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServicePieceAdded)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServicePieceAddedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServicePieceAddedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServicePieceAdded represents a PieceAdded event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServicePieceAdded struct {
	DataSetId *big.Int
	PieceId   *big.Int
	PieceCid  CidsCid
	Keys      []string
	Values    []string
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterPieceAdded is a free log retrieval operation binding the contract event 0xe919e037e2ba38e953115496aafcfc43555ef39f79c2f5f996608a78628eabd7.
//
// Solidity: event PieceAdded(uint256 indexed dataSetId, uint256 indexed pieceId, (bytes) pieceCid, string[] keys, string[] values)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterPieceAdded(opts *bind.FilterOpts, dataSetId []*big.Int, pieceId []*big.Int) (*FilecoinWarmStorageServicePieceAddedIterator, error) {

	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}
	var pieceIdRule []interface{}
	for _, pieceIdItem := range pieceId {
		pieceIdRule = append(pieceIdRule, pieceIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "PieceAdded", dataSetIdRule, pieceIdRule)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServicePieceAddedIterator{contract: _FilecoinWarmStorageService.contract, event: "PieceAdded", logs: logs, sub: sub}, nil
}

// WatchPieceAdded is a free log subscription operation binding the contract event 0xe919e037e2ba38e953115496aafcfc43555ef39f79c2f5f996608a78628eabd7.
//
// Solidity: event PieceAdded(uint256 indexed dataSetId, uint256 indexed pieceId, (bytes) pieceCid, string[] keys, string[] values)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchPieceAdded(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServicePieceAdded, dataSetId []*big.Int, pieceId []*big.Int) (event.Subscription, error) {

	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}
	var pieceIdRule []interface{}
	for _, pieceIdItem := range pieceId {
		pieceIdRule = append(pieceIdRule, pieceIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "PieceAdded", dataSetIdRule, pieceIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServicePieceAdded)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "PieceAdded", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParsePieceAdded is a log parse operation binding the contract event 0xe919e037e2ba38e953115496aafcfc43555ef39f79c2f5f996608a78628eabd7.
//
// Solidity: event PieceAdded(uint256 indexed dataSetId, uint256 indexed pieceId, (bytes) pieceCid, string[] keys, string[] values)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParsePieceAdded(log types.Log) (*FilecoinWarmStorageServicePieceAdded, error) {
	event := new(FilecoinWarmStorageServicePieceAdded)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "PieceAdded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceProviderApprovedIterator is returned from FilterProviderApproved and is used to iterate over the raw logs and unpacked data for ProviderApproved events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceProviderApprovedIterator struct {
	Event *FilecoinWarmStorageServiceProviderApproved // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceProviderApprovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceProviderApproved)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceProviderApproved)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceProviderApprovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceProviderApprovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceProviderApproved represents a ProviderApproved event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceProviderApproved struct {
	ProviderId *big.Int
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterProviderApproved is a free log retrieval operation binding the contract event 0xa58a9113199b8ca6ab27dcb19489338356a3870ca0467736c7dff7769d9d0e4b.
//
// Solidity: event ProviderApproved(uint256 indexed providerId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterProviderApproved(opts *bind.FilterOpts, providerId []*big.Int) (*FilecoinWarmStorageServiceProviderApprovedIterator, error) {

	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "ProviderApproved", providerIdRule)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceProviderApprovedIterator{contract: _FilecoinWarmStorageService.contract, event: "ProviderApproved", logs: logs, sub: sub}, nil
}

// WatchProviderApproved is a free log subscription operation binding the contract event 0xa58a9113199b8ca6ab27dcb19489338356a3870ca0467736c7dff7769d9d0e4b.
//
// Solidity: event ProviderApproved(uint256 indexed providerId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchProviderApproved(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceProviderApproved, providerId []*big.Int) (event.Subscription, error) {

	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "ProviderApproved", providerIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceProviderApproved)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "ProviderApproved", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseProviderApproved is a log parse operation binding the contract event 0xa58a9113199b8ca6ab27dcb19489338356a3870ca0467736c7dff7769d9d0e4b.
//
// Solidity: event ProviderApproved(uint256 indexed providerId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseProviderApproved(log types.Log) (*FilecoinWarmStorageServiceProviderApproved, error) {
	event := new(FilecoinWarmStorageServiceProviderApproved)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "ProviderApproved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceProviderUnapprovedIterator is returned from FilterProviderUnapproved and is used to iterate over the raw logs and unpacked data for ProviderUnapproved events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceProviderUnapprovedIterator struct {
	Event *FilecoinWarmStorageServiceProviderUnapproved // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceProviderUnapprovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceProviderUnapproved)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceProviderUnapproved)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceProviderUnapprovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceProviderUnapprovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceProviderUnapproved represents a ProviderUnapproved event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceProviderUnapproved struct {
	ProviderId *big.Int
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterProviderUnapproved is a free log retrieval operation binding the contract event 0xba4e32ee0678ec258ee0a93a97d502407f44c84993025385cd10a7f565c82b24.
//
// Solidity: event ProviderUnapproved(uint256 indexed providerId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterProviderUnapproved(opts *bind.FilterOpts, providerId []*big.Int) (*FilecoinWarmStorageServiceProviderUnapprovedIterator, error) {

	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "ProviderUnapproved", providerIdRule)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceProviderUnapprovedIterator{contract: _FilecoinWarmStorageService.contract, event: "ProviderUnapproved", logs: logs, sub: sub}, nil
}

// WatchProviderUnapproved is a free log subscription operation binding the contract event 0xba4e32ee0678ec258ee0a93a97d502407f44c84993025385cd10a7f565c82b24.
//
// Solidity: event ProviderUnapproved(uint256 indexed providerId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchProviderUnapproved(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceProviderUnapproved, providerId []*big.Int) (event.Subscription, error) {

	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "ProviderUnapproved", providerIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceProviderUnapproved)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "ProviderUnapproved", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseProviderUnapproved is a log parse operation binding the contract event 0xba4e32ee0678ec258ee0a93a97d502407f44c84993025385cd10a7f565c82b24.
//
// Solidity: event ProviderUnapproved(uint256 indexed providerId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseProviderUnapproved(log types.Log) (*FilecoinWarmStorageServiceProviderUnapproved, error) {
	event := new(FilecoinWarmStorageServiceProviderUnapproved)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "ProviderUnapproved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceRailRateUpdatedIterator is returned from FilterRailRateUpdated and is used to iterate over the raw logs and unpacked data for RailRateUpdated events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceRailRateUpdatedIterator struct {
	Event *FilecoinWarmStorageServiceRailRateUpdated // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceRailRateUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceRailRateUpdated)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceRailRateUpdated)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceRailRateUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceRailRateUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceRailRateUpdated represents a RailRateUpdated event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceRailRateUpdated struct {
	DataSetId *big.Int
	RailId    *big.Int
	NewRate   *big.Int
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterRailRateUpdated is a free log retrieval operation binding the contract event 0xe48d2ac923afa407ac53fd133176c8ba21d06ab27a0a79391ce837609fe19a63.
//
// Solidity: event RailRateUpdated(uint256 indexed dataSetId, uint256 railId, uint256 newRate)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterRailRateUpdated(opts *bind.FilterOpts, dataSetId []*big.Int) (*FilecoinWarmStorageServiceRailRateUpdatedIterator, error) {

	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "RailRateUpdated", dataSetIdRule)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceRailRateUpdatedIterator{contract: _FilecoinWarmStorageService.contract, event: "RailRateUpdated", logs: logs, sub: sub}, nil
}

// WatchRailRateUpdated is a free log subscription operation binding the contract event 0xe48d2ac923afa407ac53fd133176c8ba21d06ab27a0a79391ce837609fe19a63.
//
// Solidity: event RailRateUpdated(uint256 indexed dataSetId, uint256 railId, uint256 newRate)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchRailRateUpdated(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceRailRateUpdated, dataSetId []*big.Int) (event.Subscription, error) {

	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "RailRateUpdated", dataSetIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceRailRateUpdated)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "RailRateUpdated", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseRailRateUpdated is a log parse operation binding the contract event 0xe48d2ac923afa407ac53fd133176c8ba21d06ab27a0a79391ce837609fe19a63.
//
// Solidity: event RailRateUpdated(uint256 indexed dataSetId, uint256 railId, uint256 newRate)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseRailRateUpdated(log types.Log) (*FilecoinWarmStorageServiceRailRateUpdated, error) {
	event := new(FilecoinWarmStorageServiceRailRateUpdated)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "RailRateUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceServiceTerminatedIterator is returned from FilterServiceTerminated and is used to iterate over the raw logs and unpacked data for ServiceTerminated events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceServiceTerminatedIterator struct {
	Event *FilecoinWarmStorageServiceServiceTerminated // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceServiceTerminatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceServiceTerminated)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceServiceTerminated)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceServiceTerminatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceServiceTerminatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceServiceTerminated represents a ServiceTerminated event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceServiceTerminated struct {
	Caller          common.Address
	DataSetId       *big.Int
	PdpRailId       *big.Int
	CacheMissRailId *big.Int
	CdnRailId       *big.Int
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterServiceTerminated is a free log retrieval operation binding the contract event 0x10c867634d8e51bbfd5ddd2e06b4f4a97a91274488ee3afbe1e146aa79e85293.
//
// Solidity: event ServiceTerminated(address indexed caller, uint256 indexed dataSetId, uint256 pdpRailId, uint256 cacheMissRailId, uint256 cdnRailId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterServiceTerminated(opts *bind.FilterOpts, caller []common.Address, dataSetId []*big.Int) (*FilecoinWarmStorageServiceServiceTerminatedIterator, error) {

	var callerRule []interface{}
	for _, callerItem := range caller {
		callerRule = append(callerRule, callerItem)
	}
	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "ServiceTerminated", callerRule, dataSetIdRule)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceServiceTerminatedIterator{contract: _FilecoinWarmStorageService.contract, event: "ServiceTerminated", logs: logs, sub: sub}, nil
}

// WatchServiceTerminated is a free log subscription operation binding the contract event 0x10c867634d8e51bbfd5ddd2e06b4f4a97a91274488ee3afbe1e146aa79e85293.
//
// Solidity: event ServiceTerminated(address indexed caller, uint256 indexed dataSetId, uint256 pdpRailId, uint256 cacheMissRailId, uint256 cdnRailId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchServiceTerminated(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceServiceTerminated, caller []common.Address, dataSetId []*big.Int) (event.Subscription, error) {

	var callerRule []interface{}
	for _, callerItem := range caller {
		callerRule = append(callerRule, callerItem)
	}
	var dataSetIdRule []interface{}
	for _, dataSetIdItem := range dataSetId {
		dataSetIdRule = append(dataSetIdRule, dataSetIdItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "ServiceTerminated", callerRule, dataSetIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceServiceTerminated)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "ServiceTerminated", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseServiceTerminated is a log parse operation binding the contract event 0x10c867634d8e51bbfd5ddd2e06b4f4a97a91274488ee3afbe1e146aa79e85293.
//
// Solidity: event ServiceTerminated(address indexed caller, uint256 indexed dataSetId, uint256 pdpRailId, uint256 cacheMissRailId, uint256 cdnRailId)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseServiceTerminated(log types.Log) (*FilecoinWarmStorageServiceServiceTerminated, error) {
	event := new(FilecoinWarmStorageServiceServiceTerminated)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "ServiceTerminated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceUpgradeAnnouncedIterator is returned from FilterUpgradeAnnounced and is used to iterate over the raw logs and unpacked data for UpgradeAnnounced events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceUpgradeAnnouncedIterator struct {
	Event *FilecoinWarmStorageServiceUpgradeAnnounced // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceUpgradeAnnouncedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceUpgradeAnnounced)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceUpgradeAnnounced)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceUpgradeAnnouncedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceUpgradeAnnouncedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceUpgradeAnnounced represents a UpgradeAnnounced event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceUpgradeAnnounced struct {
	PlannedUpgrade FilecoinWarmStorageServicePlannedUpgrade
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterUpgradeAnnounced is a free log retrieval operation binding the contract event 0xbcf8666408d712c75c2cbd790925afbec6495ca9e04186b1182902260a1d53cd.
//
// Solidity: event UpgradeAnnounced((address,uint96) plannedUpgrade)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterUpgradeAnnounced(opts *bind.FilterOpts) (*FilecoinWarmStorageServiceUpgradeAnnouncedIterator, error) {

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "UpgradeAnnounced")
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceUpgradeAnnouncedIterator{contract: _FilecoinWarmStorageService.contract, event: "UpgradeAnnounced", logs: logs, sub: sub}, nil
}

// WatchUpgradeAnnounced is a free log subscription operation binding the contract event 0xbcf8666408d712c75c2cbd790925afbec6495ca9e04186b1182902260a1d53cd.
//
// Solidity: event UpgradeAnnounced((address,uint96) plannedUpgrade)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchUpgradeAnnounced(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceUpgradeAnnounced) (event.Subscription, error) {

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "UpgradeAnnounced")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceUpgradeAnnounced)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "UpgradeAnnounced", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseUpgradeAnnounced is a log parse operation binding the contract event 0xbcf8666408d712c75c2cbd790925afbec6495ca9e04186b1182902260a1d53cd.
//
// Solidity: event UpgradeAnnounced((address,uint96) plannedUpgrade)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseUpgradeAnnounced(log types.Log) (*FilecoinWarmStorageServiceUpgradeAnnounced, error) {
	event := new(FilecoinWarmStorageServiceUpgradeAnnounced)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "UpgradeAnnounced", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceUpgradedIterator is returned from FilterUpgraded and is used to iterate over the raw logs and unpacked data for Upgraded events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceUpgradedIterator struct {
	Event *FilecoinWarmStorageServiceUpgraded // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceUpgradedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceUpgraded)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceUpgraded)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceUpgradedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceUpgradedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceUpgraded represents a Upgraded event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceUpgraded struct {
	Implementation common.Address
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterUpgraded is a free log retrieval operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterUpgraded(opts *bind.FilterOpts, implementation []common.Address) (*FilecoinWarmStorageServiceUpgradedIterator, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceUpgradedIterator{contract: _FilecoinWarmStorageService.contract, event: "Upgraded", logs: logs, sub: sub}, nil
}

// WatchUpgraded is a free log subscription operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchUpgraded(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceUpgraded, implementation []common.Address) (event.Subscription, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceUpgraded)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "Upgraded", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseUpgraded is a log parse operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseUpgraded(log types.Log) (*FilecoinWarmStorageServiceUpgraded, error) {
	event := new(FilecoinWarmStorageServiceUpgraded)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "Upgraded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FilecoinWarmStorageServiceViewContractSetIterator is returned from FilterViewContractSet and is used to iterate over the raw logs and unpacked data for ViewContractSet events raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceViewContractSetIterator struct {
	Event *FilecoinWarmStorageServiceViewContractSet // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *FilecoinWarmStorageServiceViewContractSetIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FilecoinWarmStorageServiceViewContractSet)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(FilecoinWarmStorageServiceViewContractSet)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *FilecoinWarmStorageServiceViewContractSetIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FilecoinWarmStorageServiceViewContractSetIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FilecoinWarmStorageServiceViewContractSet represents a ViewContractSet event raised by the FilecoinWarmStorageService contract.
type FilecoinWarmStorageServiceViewContractSet struct {
	ViewContract common.Address
	Raw          types.Log // Blockchain specific contextual infos
}

// FilterViewContractSet is a free log retrieval operation binding the contract event 0xe25384d89f44dc828e27dcd324f63dae28a4b9e5bb164e04a9c7ecfacf01fd36.
//
// Solidity: event ViewContractSet(address indexed viewContract)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) FilterViewContractSet(opts *bind.FilterOpts, viewContract []common.Address) (*FilecoinWarmStorageServiceViewContractSetIterator, error) {

	var viewContractRule []interface{}
	for _, viewContractItem := range viewContract {
		viewContractRule = append(viewContractRule, viewContractItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.FilterLogs(opts, "ViewContractSet", viewContractRule)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceViewContractSetIterator{contract: _FilecoinWarmStorageService.contract, event: "ViewContractSet", logs: logs, sub: sub}, nil
}

// WatchViewContractSet is a free log subscription operation binding the contract event 0xe25384d89f44dc828e27dcd324f63dae28a4b9e5bb164e04a9c7ecfacf01fd36.
//
// Solidity: event ViewContractSet(address indexed viewContract)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) WatchViewContractSet(opts *bind.WatchOpts, sink chan<- *FilecoinWarmStorageServiceViewContractSet, viewContract []common.Address) (event.Subscription, error) {

	var viewContractRule []interface{}
	for _, viewContractItem := range viewContract {
		viewContractRule = append(viewContractRule, viewContractItem)
	}

	logs, sub, err := _FilecoinWarmStorageService.contract.WatchLogs(opts, "ViewContractSet", viewContractRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FilecoinWarmStorageServiceViewContractSet)
				if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "ViewContractSet", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseViewContractSet is a log parse operation binding the contract event 0xe25384d89f44dc828e27dcd324f63dae28a4b9e5bb164e04a9c7ecfacf01fd36.
//
// Solidity: event ViewContractSet(address indexed viewContract)
func (_FilecoinWarmStorageService *FilecoinWarmStorageServiceFilterer) ParseViewContractSet(log types.Log) (*FilecoinWarmStorageServiceViewContractSet, error) {
	event := new(FilecoinWarmStorageServiceViewContractSet)
	if err := _FilecoinWarmStorageService.contract.UnpackLog(event, "ViewContractSet", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
