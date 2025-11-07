// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package filecoinpayment

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
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

// PaymentsRailInfo is an auto generated low-level Go binding around an user-defined struct.
type PaymentsRailInfo struct {
	RailId       *big.Int
	IsTerminated bool
	EndEpoch     *big.Int
}

// PaymentsRailView is an auto generated low-level Go binding around an user-defined struct.
type PaymentsRailView struct {
	Token               common.Address
	From                common.Address
	To                  common.Address
	Operator            common.Address
	Validator           common.Address
	PaymentRate         *big.Int
	LockupPeriod        *big.Int
	LockupFixed         *big.Int
	SettledUpTo         *big.Int
	EndEpoch            *big.Int
	CommissionRateBps   *big.Int
	ServiceFeeRecipient common.Address
}

// PaymentsMetaData contains all meta data concerning the Payments contract.
var PaymentsMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"COMMISSION_MAX_BPS\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"NETWORK_FEE_DENOMINATOR\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"NETWORK_FEE_NUMERATOR\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"accounts\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"owner\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"funds\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"lockupCurrent\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"lockupRate\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"lockupLastSettledAt\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"auctionInfo\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"}],\"outputs\":[{\"name\":\"startPrice\",\"type\":\"uint88\",\"internalType\":\"uint88\"},{\"name\":\"startTime\",\"type\":\"uint168\",\"internalType\":\"uint168\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"burnForFees\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"recipient\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"requested\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"payable\"},{\"type\":\"function\",\"name\":\"createRail\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"from\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"validator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"commissionRateBps\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"serviceFeeRecipient\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"deposit\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"payable\"},{\"type\":\"function\",\"name\":\"depositWithAuthorization\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC3009\"},{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"validAfter\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"validBefore\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"nonce\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"v\",\"type\":\"uint8\",\"internalType\":\"uint8\"},{\"name\":\"r\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"s\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"depositWithAuthorizationAndApproveOperator\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC3009\"},{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"validAfter\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"validBefore\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"nonce\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"v\",\"type\":\"uint8\",\"internalType\":\"uint8\"},{\"name\":\"r\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"s\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"operator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"rateAllowance\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"lockupAllowance\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"maxLockupPeriod\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"depositWithAuthorizationAndIncreaseOperatorApproval\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC3009\"},{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"validAfter\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"validBefore\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"nonce\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"v\",\"type\":\"uint8\",\"internalType\":\"uint8\"},{\"name\":\"r\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"s\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"operator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"rateAllowanceIncrease\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"lockupAllowanceIncrease\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"depositWithPermit\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"deadline\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"v\",\"type\":\"uint8\",\"internalType\":\"uint8\"},{\"name\":\"r\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"s\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"depositWithPermitAndApproveOperator\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"deadline\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"v\",\"type\":\"uint8\",\"internalType\":\"uint8\"},{\"name\":\"r\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"s\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"operator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"rateAllowance\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"lockupAllowance\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"maxLockupPeriod\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"depositWithPermitAndIncreaseOperatorApproval\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"deadline\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"v\",\"type\":\"uint8\",\"internalType\":\"uint8\"},{\"name\":\"r\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"s\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"operator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"rateAllowanceIncrease\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"lockupAllowanceIncrease\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"getAccountInfoIfSettled\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"owner\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"fundedUntilEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"currentFunds\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"availableFunds\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"currentLockupRate\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getRail\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"tuple\",\"internalType\":\"structPayments.RailView\",\"components\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"from\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"operator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"validator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"paymentRate\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"lockupPeriod\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"lockupFixed\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"settledUpTo\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"endEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"commissionRateBps\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"serviceFeeRecipient\",\"type\":\"address\",\"internalType\":\"address\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getRailsForPayeeAndToken\",\"inputs\":[{\"name\":\"payee\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"offset\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"limit\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"results\",\"type\":\"tuple[]\",\"internalType\":\"structPayments.RailInfo[]\",\"components\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"isTerminated\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"endEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"name\":\"nextOffset\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"total\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getRailsForPayerAndToken\",\"inputs\":[{\"name\":\"payer\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"offset\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"limit\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"results\",\"type\":\"tuple[]\",\"internalType\":\"structPayments.RailInfo[]\",\"components\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"isTerminated\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"endEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"name\":\"nextOffset\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"total\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getRateChangeQueueSize\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"increaseOperatorApproval\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"operator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"rateAllowanceIncrease\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"lockupAllowanceIncrease\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"modifyRailLockup\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"period\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"lockupFixed\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"modifyRailPayment\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"newRate\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"oneTimePayment\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"operatorApprovals\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"client\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"operator\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"isApproved\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"rateAllowance\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"lockupAllowance\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"rateUsage\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"lockupUsage\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"maxLockupPeriod\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"setOperatorApproval\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"operator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"approved\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"rateAllowance\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"lockupAllowance\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"maxLockupPeriod\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"settleRail\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"untilEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"totalSettledAmount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"totalNetPayeeAmount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"totalOperatorCommission\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"totalNetworkFee\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"finalSettledEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"note\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"settleTerminatedRailWithoutValidation\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"totalSettledAmount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"totalNetPayeeAmount\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"totalOperatorCommission\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"totalNetworkFee\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"finalSettledEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"note\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"terminateRail\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"withdraw\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"withdrawTo\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"event\",\"name\":\"AccountLockupSettled\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"contractIERC20\"},{\"name\":\"owner\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"lockupCurrent\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"lockupRate\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"lockupLastSettledAt\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"DepositRecorded\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"contractIERC20\"},{\"name\":\"from\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"to\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"OperatorApprovalUpdated\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"contractIERC20\"},{\"name\":\"client\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"operator\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"approved\",\"type\":\"bool\",\"indexed\":false,\"internalType\":\"bool\"},{\"name\":\"rateAllowance\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"lockupAllowance\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"maxLockupPeriod\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"RailCreated\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"payer\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"payee\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"token\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"contractIERC20\"},{\"name\":\"operator\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"},{\"name\":\"validator\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"},{\"name\":\"serviceFeeRecipient\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"},{\"name\":\"commissionRateBps\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"RailFinalized\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"RailLockupModified\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"oldLockupPeriod\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"newLockupPeriod\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"oldLockupFixed\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"newLockupFixed\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"RailOneTimePaymentProcessed\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"netPayeeAmount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"operatorCommission\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"networkFee\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"RailRateModified\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"oldRate\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"newRate\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"RailSettled\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"totalSettledAmount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"totalNetPayeeAmount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"operatorCommission\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"networkFee\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"settledUpTo\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"RailTerminated\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"by\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"endEpoch\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"WithdrawRecorded\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"contractIERC20\"},{\"name\":\"from\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"to\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"error\",\"name\":\"CannotModifyTerminatedRailBeyondEndEpoch\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"maxSettlementEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"blockNumber\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"CannotSettleFutureEpochs\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"maxAllowedEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"attemptedEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"CannotSettleTerminatedRailBeforeMaxEpoch\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"requiredBlock\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"currentBlock\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"CommissionRateTooHigh\",\"inputs\":[{\"name\":\"maxAllowed\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"actual\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"CurrentLockupLessThanOldLockup\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"from\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"oldLockup\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"currentLockup\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"InsufficientCurrentLockup\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"from\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"currentLockup\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"lockupReduction\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"InsufficientFundsForOneTimePayment\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"from\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"required\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"actual\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"InsufficientFundsForSettlement\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"from\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"available\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"required\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"InsufficientLockupForSettlement\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"from\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"available\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"required\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"InsufficientNativeTokenForBurn\",\"inputs\":[{\"name\":\"required\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"sent\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"InsufficientUnlockedFunds\",\"inputs\":[{\"name\":\"available\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"requested\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"InvalidRateChangeQueueState\",\"inputs\":[{\"name\":\"nextRateChangeUntilEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"processedEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"InvalidTerminatedRailModification\",\"inputs\":[{\"name\":\"actualPeriod\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"actualLockupFixed\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"attemptedPeriod\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"attemptedLockupFixed\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"LockupExceedsFundsInvariant\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"account\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"lockupCurrent\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"fundsCurrent\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"LockupFixedIncreaseNotAllowedDueToInsufficientFunds\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"from\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"actualLockupFixed\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"attemptedLockupFixed\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"LockupInconsistencyDuringRailFinalization\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"from\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"expectedLockup\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"actualLockup\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"LockupNotSettledRateChangeNotAllowed\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"from\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"isSettled\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"currentRate\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"attemptedRate\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"LockupPeriodChangeNotAllowedDueToInsufficientFunds\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"from\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"actualLockupPeriod\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"attemptedLockupPeriod\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"LockupPeriodExceedsOperatorMaximum\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"operator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"maxAllowedPeriod\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"requestedPeriod\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"LockupRateInconsistent\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"from\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"paymentRate\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"lockupRate\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"LockupRateLessThanOldRate\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"from\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"lockupRate\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"oldRate\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"MissingServiceFeeRecipient\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"MustSendExactNativeAmount\",\"inputs\":[{\"name\":\"required\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"sent\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"NativeTokenNotAccepted\",\"inputs\":[{\"name\":\"sent\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"NativeTokenNotSupported\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"NativeTransferFailed\",\"inputs\":[{\"name\":\"to\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"amount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"NoProgressInSettlement\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"expectedSettledUpTo\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"actualSettledUpTo\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"NotAuthorizedToTerminateRail\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"allowedClient\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"allowedOperator\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"caller\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"OneTimePaymentExceedsLockup\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"available\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"required\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"OnlyRailClientAllowed\",\"inputs\":[{\"name\":\"expected\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"caller\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"OnlyRailOperatorAllowed\",\"inputs\":[{\"name\":\"expected\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"caller\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"OperatorLockupAllowanceExceeded\",\"inputs\":[{\"name\":\"allowed\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"attemptedUsage\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"OperatorNotApproved\",\"inputs\":[{\"name\":\"from\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"operator\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"OperatorRateAllowanceExceeded\",\"inputs\":[{\"name\":\"allowed\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"attemptedUsage\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"PRBMath_MulDiv_Overflow\",\"inputs\":[{\"name\":\"x\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"y\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"denominator\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"PRBMath_UD60x18_Exp2_InputTooBig\",\"inputs\":[{\"name\":\"x\",\"type\":\"uint256\",\"internalType\":\"UD60x18\"}]},{\"type\":\"error\",\"name\":\"RailAlreadyTerminated\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"RailInactiveOrSettled\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"RailNotTerminated\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"RateChangeNotAllowedOnTerminatedRail\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"RateChangeQueueNotEmpty\",\"inputs\":[{\"name\":\"nextUntilEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"ReentrancyGuardReentrantCall\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"SafeERC20FailedOperation\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"SignerMustBeMsgSender\",\"inputs\":[{\"name\":\"expected\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"actual\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"ValidatorModifiedAmountExceedsMaximum\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"maxAllowed\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"attempted\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"ValidatorSettledBeforeSegmentStart\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"allowedStart\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"attemptedStart\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"ValidatorSettledBeyondSegmentEnd\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"allowedEnd\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"attemptedEnd\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"WithdrawAmountExceedsAccumulatedFees\",\"inputs\":[{\"name\":\"token\",\"type\":\"address\",\"internalType\":\"contractIERC20\"},{\"name\":\"available\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"requested\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"ZeroAddressNotAllowed\",\"inputs\":[{\"name\":\"varName\",\"type\":\"string\",\"internalType\":\"string\"}]}]",
}

// PaymentsABI is the input ABI used to generate the binding from.
// Deprecated: Use PaymentsMetaData.ABI instead.
var PaymentsABI = PaymentsMetaData.ABI

// Payments is an auto generated Go binding around an Ethereum contract.
type Payments struct {
	PaymentsCaller     // Read-only binding to the contract
	PaymentsTransactor // Write-only binding to the contract
	PaymentsFilterer   // Log filterer for contract events
}

// PaymentsCaller is an auto generated read-only Go binding around an Ethereum contract.
type PaymentsCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PaymentsTransactor is an auto generated write-only Go binding around an Ethereum contract.
type PaymentsTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PaymentsFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type PaymentsFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PaymentsSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type PaymentsSession struct {
	Contract     *Payments         // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// PaymentsCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type PaymentsCallerSession struct {
	Contract *PaymentsCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts   // Call options to use throughout this session
}

// PaymentsTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type PaymentsTransactorSession struct {
	Contract     *PaymentsTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts   // Transaction auth options to use throughout this session
}

// PaymentsRaw is an auto generated low-level Go binding around an Ethereum contract.
type PaymentsRaw struct {
	Contract *Payments // Generic contract binding to access the raw methods on
}

// PaymentsCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type PaymentsCallerRaw struct {
	Contract *PaymentsCaller // Generic read-only contract binding to access the raw methods on
}

// PaymentsTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type PaymentsTransactorRaw struct {
	Contract *PaymentsTransactor // Generic write-only contract binding to access the raw methods on
}

// NewPayments creates a new instance of Payments, bound to a specific deployed contract.
func NewPayments(address common.Address, backend bind.ContractBackend) (*Payments, error) {
	contract, err := bindPayments(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Payments{PaymentsCaller: PaymentsCaller{contract: contract}, PaymentsTransactor: PaymentsTransactor{contract: contract}, PaymentsFilterer: PaymentsFilterer{contract: contract}}, nil
}

// NewPaymentsCaller creates a new read-only instance of Payments, bound to a specific deployed contract.
func NewPaymentsCaller(address common.Address, caller bind.ContractCaller) (*PaymentsCaller, error) {
	contract, err := bindPayments(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &PaymentsCaller{contract: contract}, nil
}

// NewPaymentsTransactor creates a new write-only instance of Payments, bound to a specific deployed contract.
func NewPaymentsTransactor(address common.Address, transactor bind.ContractTransactor) (*PaymentsTransactor, error) {
	contract, err := bindPayments(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &PaymentsTransactor{contract: contract}, nil
}

// NewPaymentsFilterer creates a new log filterer instance of Payments, bound to a specific deployed contract.
func NewPaymentsFilterer(address common.Address, filterer bind.ContractFilterer) (*PaymentsFilterer, error) {
	contract, err := bindPayments(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &PaymentsFilterer{contract: contract}, nil
}

// bindPayments binds a generic wrapper to an already deployed contract.
func bindPayments(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := PaymentsMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Payments *PaymentsRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Payments.Contract.PaymentsCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Payments *PaymentsRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Payments.Contract.PaymentsTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Payments *PaymentsRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Payments.Contract.PaymentsTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Payments *PaymentsCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Payments.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Payments *PaymentsTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Payments.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Payments *PaymentsTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Payments.Contract.contract.Transact(opts, method, params...)
}

// COMMISSIONMAXBPS is a free data retrieval call binding the contract method 0x8aab236a.
//
// Solidity: function COMMISSION_MAX_BPS() view returns(uint256)
func (_Payments *PaymentsCaller) COMMISSIONMAXBPS(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Payments.contract.Call(opts, &out, "COMMISSION_MAX_BPS")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// COMMISSIONMAXBPS is a free data retrieval call binding the contract method 0x8aab236a.
//
// Solidity: function COMMISSION_MAX_BPS() view returns(uint256)
func (_Payments *PaymentsSession) COMMISSIONMAXBPS() (*big.Int, error) {
	return _Payments.Contract.COMMISSIONMAXBPS(&_Payments.CallOpts)
}

// COMMISSIONMAXBPS is a free data retrieval call binding the contract method 0x8aab236a.
//
// Solidity: function COMMISSION_MAX_BPS() view returns(uint256)
func (_Payments *PaymentsCallerSession) COMMISSIONMAXBPS() (*big.Int, error) {
	return _Payments.Contract.COMMISSIONMAXBPS(&_Payments.CallOpts)
}

// NETWORKFEEDENOMINATOR is a free data retrieval call binding the contract method 0xe0975cf8.
//
// Solidity: function NETWORK_FEE_DENOMINATOR() view returns(uint256)
func (_Payments *PaymentsCaller) NETWORKFEEDENOMINATOR(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Payments.contract.Call(opts, &out, "NETWORK_FEE_DENOMINATOR")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// NETWORKFEEDENOMINATOR is a free data retrieval call binding the contract method 0xe0975cf8.
//
// Solidity: function NETWORK_FEE_DENOMINATOR() view returns(uint256)
func (_Payments *PaymentsSession) NETWORKFEEDENOMINATOR() (*big.Int, error) {
	return _Payments.Contract.NETWORKFEEDENOMINATOR(&_Payments.CallOpts)
}

// NETWORKFEEDENOMINATOR is a free data retrieval call binding the contract method 0xe0975cf8.
//
// Solidity: function NETWORK_FEE_DENOMINATOR() view returns(uint256)
func (_Payments *PaymentsCallerSession) NETWORKFEEDENOMINATOR() (*big.Int, error) {
	return _Payments.Contract.NETWORKFEEDENOMINATOR(&_Payments.CallOpts)
}

// NETWORKFEENUMERATOR is a free data retrieval call binding the contract method 0x553d8c82.
//
// Solidity: function NETWORK_FEE_NUMERATOR() view returns(uint256)
func (_Payments *PaymentsCaller) NETWORKFEENUMERATOR(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Payments.contract.Call(opts, &out, "NETWORK_FEE_NUMERATOR")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// NETWORKFEENUMERATOR is a free data retrieval call binding the contract method 0x553d8c82.
//
// Solidity: function NETWORK_FEE_NUMERATOR() view returns(uint256)
func (_Payments *PaymentsSession) NETWORKFEENUMERATOR() (*big.Int, error) {
	return _Payments.Contract.NETWORKFEENUMERATOR(&_Payments.CallOpts)
}

// NETWORKFEENUMERATOR is a free data retrieval call binding the contract method 0x553d8c82.
//
// Solidity: function NETWORK_FEE_NUMERATOR() view returns(uint256)
func (_Payments *PaymentsCallerSession) NETWORKFEENUMERATOR() (*big.Int, error) {
	return _Payments.Contract.NETWORKFEENUMERATOR(&_Payments.CallOpts)
}

// Accounts is a free data retrieval call binding the contract method 0xad74b775.
//
// Solidity: function accounts(address token, address owner) view returns(uint256 funds, uint256 lockupCurrent, uint256 lockupRate, uint256 lockupLastSettledAt)
func (_Payments *PaymentsCaller) Accounts(opts *bind.CallOpts, token common.Address, owner common.Address) (struct {
	Funds               *big.Int
	LockupCurrent       *big.Int
	LockupRate          *big.Int
	LockupLastSettledAt *big.Int
}, error) {
	var out []interface{}
	err := _Payments.contract.Call(opts, &out, "accounts", token, owner)

	outstruct := new(struct {
		Funds               *big.Int
		LockupCurrent       *big.Int
		LockupRate          *big.Int
		LockupLastSettledAt *big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Funds = *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	outstruct.LockupCurrent = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	outstruct.LockupRate = *abi.ConvertType(out[2], new(*big.Int)).(**big.Int)
	outstruct.LockupLastSettledAt = *abi.ConvertType(out[3], new(*big.Int)).(**big.Int)

	return *outstruct, err

}

// Accounts is a free data retrieval call binding the contract method 0xad74b775.
//
// Solidity: function accounts(address token, address owner) view returns(uint256 funds, uint256 lockupCurrent, uint256 lockupRate, uint256 lockupLastSettledAt)
func (_Payments *PaymentsSession) Accounts(token common.Address, owner common.Address) (struct {
	Funds               *big.Int
	LockupCurrent       *big.Int
	LockupRate          *big.Int
	LockupLastSettledAt *big.Int
}, error) {
	return _Payments.Contract.Accounts(&_Payments.CallOpts, token, owner)
}

// Accounts is a free data retrieval call binding the contract method 0xad74b775.
//
// Solidity: function accounts(address token, address owner) view returns(uint256 funds, uint256 lockupCurrent, uint256 lockupRate, uint256 lockupLastSettledAt)
func (_Payments *PaymentsCallerSession) Accounts(token common.Address, owner common.Address) (struct {
	Funds               *big.Int
	LockupCurrent       *big.Int
	LockupRate          *big.Int
	LockupLastSettledAt *big.Int
}, error) {
	return _Payments.Contract.Accounts(&_Payments.CallOpts, token, owner)
}

// AuctionInfo is a free data retrieval call binding the contract method 0x0448e51a.
//
// Solidity: function auctionInfo(address token) view returns(uint88 startPrice, uint168 startTime)
func (_Payments *PaymentsCaller) AuctionInfo(opts *bind.CallOpts, token common.Address) (struct {
	StartPrice *big.Int
	StartTime  *big.Int
}, error) {
	var out []interface{}
	err := _Payments.contract.Call(opts, &out, "auctionInfo", token)

	outstruct := new(struct {
		StartPrice *big.Int
		StartTime  *big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.StartPrice = *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	outstruct.StartTime = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)

	return *outstruct, err

}

// AuctionInfo is a free data retrieval call binding the contract method 0x0448e51a.
//
// Solidity: function auctionInfo(address token) view returns(uint88 startPrice, uint168 startTime)
func (_Payments *PaymentsSession) AuctionInfo(token common.Address) (struct {
	StartPrice *big.Int
	StartTime  *big.Int
}, error) {
	return _Payments.Contract.AuctionInfo(&_Payments.CallOpts, token)
}

// AuctionInfo is a free data retrieval call binding the contract method 0x0448e51a.
//
// Solidity: function auctionInfo(address token) view returns(uint88 startPrice, uint168 startTime)
func (_Payments *PaymentsCallerSession) AuctionInfo(token common.Address) (struct {
	StartPrice *big.Int
	StartTime  *big.Int
}, error) {
	return _Payments.Contract.AuctionInfo(&_Payments.CallOpts, token)
}

// GetAccountInfoIfSettled is a free data retrieval call binding the contract method 0x05f4c536.
//
// Solidity: function getAccountInfoIfSettled(address token, address owner) view returns(uint256 fundedUntilEpoch, uint256 currentFunds, uint256 availableFunds, uint256 currentLockupRate)
func (_Payments *PaymentsCaller) GetAccountInfoIfSettled(opts *bind.CallOpts, token common.Address, owner common.Address) (struct {
	FundedUntilEpoch  *big.Int
	CurrentFunds      *big.Int
	AvailableFunds    *big.Int
	CurrentLockupRate *big.Int
}, error) {
	var out []interface{}
	err := _Payments.contract.Call(opts, &out, "getAccountInfoIfSettled", token, owner)

	outstruct := new(struct {
		FundedUntilEpoch  *big.Int
		CurrentFunds      *big.Int
		AvailableFunds    *big.Int
		CurrentLockupRate *big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.FundedUntilEpoch = *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	outstruct.CurrentFunds = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	outstruct.AvailableFunds = *abi.ConvertType(out[2], new(*big.Int)).(**big.Int)
	outstruct.CurrentLockupRate = *abi.ConvertType(out[3], new(*big.Int)).(**big.Int)

	return *outstruct, err

}

// GetAccountInfoIfSettled is a free data retrieval call binding the contract method 0x05f4c536.
//
// Solidity: function getAccountInfoIfSettled(address token, address owner) view returns(uint256 fundedUntilEpoch, uint256 currentFunds, uint256 availableFunds, uint256 currentLockupRate)
func (_Payments *PaymentsSession) GetAccountInfoIfSettled(token common.Address, owner common.Address) (struct {
	FundedUntilEpoch  *big.Int
	CurrentFunds      *big.Int
	AvailableFunds    *big.Int
	CurrentLockupRate *big.Int
}, error) {
	return _Payments.Contract.GetAccountInfoIfSettled(&_Payments.CallOpts, token, owner)
}

// GetAccountInfoIfSettled is a free data retrieval call binding the contract method 0x05f4c536.
//
// Solidity: function getAccountInfoIfSettled(address token, address owner) view returns(uint256 fundedUntilEpoch, uint256 currentFunds, uint256 availableFunds, uint256 currentLockupRate)
func (_Payments *PaymentsCallerSession) GetAccountInfoIfSettled(token common.Address, owner common.Address) (struct {
	FundedUntilEpoch  *big.Int
	CurrentFunds      *big.Int
	AvailableFunds    *big.Int
	CurrentLockupRate *big.Int
}, error) {
	return _Payments.Contract.GetAccountInfoIfSettled(&_Payments.CallOpts, token, owner)
}

// GetRail is a free data retrieval call binding the contract method 0x22e440b3.
//
// Solidity: function getRail(uint256 railId) view returns((address,address,address,address,address,uint256,uint256,uint256,uint256,uint256,uint256,address))
func (_Payments *PaymentsCaller) GetRail(opts *bind.CallOpts, railId *big.Int) (PaymentsRailView, error) {
	var out []interface{}
	err := _Payments.contract.Call(opts, &out, "getRail", railId)

	if err != nil {
		return *new(PaymentsRailView), err
	}

	out0 := *abi.ConvertType(out[0], new(PaymentsRailView)).(*PaymentsRailView)

	return out0, err

}

// GetRail is a free data retrieval call binding the contract method 0x22e440b3.
//
// Solidity: function getRail(uint256 railId) view returns((address,address,address,address,address,uint256,uint256,uint256,uint256,uint256,uint256,address))
func (_Payments *PaymentsSession) GetRail(railId *big.Int) (PaymentsRailView, error) {
	return _Payments.Contract.GetRail(&_Payments.CallOpts, railId)
}

// GetRail is a free data retrieval call binding the contract method 0x22e440b3.
//
// Solidity: function getRail(uint256 railId) view returns((address,address,address,address,address,uint256,uint256,uint256,uint256,uint256,uint256,address))
func (_Payments *PaymentsCallerSession) GetRail(railId *big.Int) (PaymentsRailView, error) {
	return _Payments.Contract.GetRail(&_Payments.CallOpts, railId)
}

// GetRailsForPayeeAndToken is a free data retrieval call binding the contract method 0x7f7562fa.
//
// Solidity: function getRailsForPayeeAndToken(address payee, address token, uint256 offset, uint256 limit) view returns((uint256,bool,uint256)[] results, uint256 nextOffset, uint256 total)
func (_Payments *PaymentsCaller) GetRailsForPayeeAndToken(opts *bind.CallOpts, payee common.Address, token common.Address, offset *big.Int, limit *big.Int) (struct {
	Results    []PaymentsRailInfo
	NextOffset *big.Int
	Total      *big.Int
}, error) {
	var out []interface{}
	err := _Payments.contract.Call(opts, &out, "getRailsForPayeeAndToken", payee, token, offset, limit)

	outstruct := new(struct {
		Results    []PaymentsRailInfo
		NextOffset *big.Int
		Total      *big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Results = *abi.ConvertType(out[0], new([]PaymentsRailInfo)).(*[]PaymentsRailInfo)
	outstruct.NextOffset = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	outstruct.Total = *abi.ConvertType(out[2], new(*big.Int)).(**big.Int)

	return *outstruct, err

}

// GetRailsForPayeeAndToken is a free data retrieval call binding the contract method 0x7f7562fa.
//
// Solidity: function getRailsForPayeeAndToken(address payee, address token, uint256 offset, uint256 limit) view returns((uint256,bool,uint256)[] results, uint256 nextOffset, uint256 total)
func (_Payments *PaymentsSession) GetRailsForPayeeAndToken(payee common.Address, token common.Address, offset *big.Int, limit *big.Int) (struct {
	Results    []PaymentsRailInfo
	NextOffset *big.Int
	Total      *big.Int
}, error) {
	return _Payments.Contract.GetRailsForPayeeAndToken(&_Payments.CallOpts, payee, token, offset, limit)
}

// GetRailsForPayeeAndToken is a free data retrieval call binding the contract method 0x7f7562fa.
//
// Solidity: function getRailsForPayeeAndToken(address payee, address token, uint256 offset, uint256 limit) view returns((uint256,bool,uint256)[] results, uint256 nextOffset, uint256 total)
func (_Payments *PaymentsCallerSession) GetRailsForPayeeAndToken(payee common.Address, token common.Address, offset *big.Int, limit *big.Int) (struct {
	Results    []PaymentsRailInfo
	NextOffset *big.Int
	Total      *big.Int
}, error) {
	return _Payments.Contract.GetRailsForPayeeAndToken(&_Payments.CallOpts, payee, token, offset, limit)
}

// GetRailsForPayerAndToken is a free data retrieval call binding the contract method 0x007b5fd1.
//
// Solidity: function getRailsForPayerAndToken(address payer, address token, uint256 offset, uint256 limit) view returns((uint256,bool,uint256)[] results, uint256 nextOffset, uint256 total)
func (_Payments *PaymentsCaller) GetRailsForPayerAndToken(opts *bind.CallOpts, payer common.Address, token common.Address, offset *big.Int, limit *big.Int) (struct {
	Results    []PaymentsRailInfo
	NextOffset *big.Int
	Total      *big.Int
}, error) {
	var out []interface{}
	err := _Payments.contract.Call(opts, &out, "getRailsForPayerAndToken", payer, token, offset, limit)

	outstruct := new(struct {
		Results    []PaymentsRailInfo
		NextOffset *big.Int
		Total      *big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Results = *abi.ConvertType(out[0], new([]PaymentsRailInfo)).(*[]PaymentsRailInfo)
	outstruct.NextOffset = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	outstruct.Total = *abi.ConvertType(out[2], new(*big.Int)).(**big.Int)

	return *outstruct, err

}

// GetRailsForPayerAndToken is a free data retrieval call binding the contract method 0x007b5fd1.
//
// Solidity: function getRailsForPayerAndToken(address payer, address token, uint256 offset, uint256 limit) view returns((uint256,bool,uint256)[] results, uint256 nextOffset, uint256 total)
func (_Payments *PaymentsSession) GetRailsForPayerAndToken(payer common.Address, token common.Address, offset *big.Int, limit *big.Int) (struct {
	Results    []PaymentsRailInfo
	NextOffset *big.Int
	Total      *big.Int
}, error) {
	return _Payments.Contract.GetRailsForPayerAndToken(&_Payments.CallOpts, payer, token, offset, limit)
}

// GetRailsForPayerAndToken is a free data retrieval call binding the contract method 0x007b5fd1.
//
// Solidity: function getRailsForPayerAndToken(address payer, address token, uint256 offset, uint256 limit) view returns((uint256,bool,uint256)[] results, uint256 nextOffset, uint256 total)
func (_Payments *PaymentsCallerSession) GetRailsForPayerAndToken(payer common.Address, token common.Address, offset *big.Int, limit *big.Int) (struct {
	Results    []PaymentsRailInfo
	NextOffset *big.Int
	Total      *big.Int
}, error) {
	return _Payments.Contract.GetRailsForPayerAndToken(&_Payments.CallOpts, payer, token, offset, limit)
}

// GetRateChangeQueueSize is a free data retrieval call binding the contract method 0x356412ae.
//
// Solidity: function getRateChangeQueueSize(uint256 railId) view returns(uint256)
func (_Payments *PaymentsCaller) GetRateChangeQueueSize(opts *bind.CallOpts, railId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _Payments.contract.Call(opts, &out, "getRateChangeQueueSize", railId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetRateChangeQueueSize is a free data retrieval call binding the contract method 0x356412ae.
//
// Solidity: function getRateChangeQueueSize(uint256 railId) view returns(uint256)
func (_Payments *PaymentsSession) GetRateChangeQueueSize(railId *big.Int) (*big.Int, error) {
	return _Payments.Contract.GetRateChangeQueueSize(&_Payments.CallOpts, railId)
}

// GetRateChangeQueueSize is a free data retrieval call binding the contract method 0x356412ae.
//
// Solidity: function getRateChangeQueueSize(uint256 railId) view returns(uint256)
func (_Payments *PaymentsCallerSession) GetRateChangeQueueSize(railId *big.Int) (*big.Int, error) {
	return _Payments.Contract.GetRateChangeQueueSize(&_Payments.CallOpts, railId)
}

// OperatorApprovals is a free data retrieval call binding the contract method 0xe3d4c69e.
//
// Solidity: function operatorApprovals(address token, address client, address operator) view returns(bool isApproved, uint256 rateAllowance, uint256 lockupAllowance, uint256 rateUsage, uint256 lockupUsage, uint256 maxLockupPeriod)
func (_Payments *PaymentsCaller) OperatorApprovals(opts *bind.CallOpts, token common.Address, client common.Address, operator common.Address) (struct {
	IsApproved      bool
	RateAllowance   *big.Int
	LockupAllowance *big.Int
	RateUsage       *big.Int
	LockupUsage     *big.Int
	MaxLockupPeriod *big.Int
}, error) {
	var out []interface{}
	err := _Payments.contract.Call(opts, &out, "operatorApprovals", token, client, operator)

	outstruct := new(struct {
		IsApproved      bool
		RateAllowance   *big.Int
		LockupAllowance *big.Int
		RateUsage       *big.Int
		LockupUsage     *big.Int
		MaxLockupPeriod *big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.IsApproved = *abi.ConvertType(out[0], new(bool)).(*bool)
	outstruct.RateAllowance = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	outstruct.LockupAllowance = *abi.ConvertType(out[2], new(*big.Int)).(**big.Int)
	outstruct.RateUsage = *abi.ConvertType(out[3], new(*big.Int)).(**big.Int)
	outstruct.LockupUsage = *abi.ConvertType(out[4], new(*big.Int)).(**big.Int)
	outstruct.MaxLockupPeriod = *abi.ConvertType(out[5], new(*big.Int)).(**big.Int)

	return *outstruct, err

}

// OperatorApprovals is a free data retrieval call binding the contract method 0xe3d4c69e.
//
// Solidity: function operatorApprovals(address token, address client, address operator) view returns(bool isApproved, uint256 rateAllowance, uint256 lockupAllowance, uint256 rateUsage, uint256 lockupUsage, uint256 maxLockupPeriod)
func (_Payments *PaymentsSession) OperatorApprovals(token common.Address, client common.Address, operator common.Address) (struct {
	IsApproved      bool
	RateAllowance   *big.Int
	LockupAllowance *big.Int
	RateUsage       *big.Int
	LockupUsage     *big.Int
	MaxLockupPeriod *big.Int
}, error) {
	return _Payments.Contract.OperatorApprovals(&_Payments.CallOpts, token, client, operator)
}

// OperatorApprovals is a free data retrieval call binding the contract method 0xe3d4c69e.
//
// Solidity: function operatorApprovals(address token, address client, address operator) view returns(bool isApproved, uint256 rateAllowance, uint256 lockupAllowance, uint256 rateUsage, uint256 lockupUsage, uint256 maxLockupPeriod)
func (_Payments *PaymentsCallerSession) OperatorApprovals(token common.Address, client common.Address, operator common.Address) (struct {
	IsApproved      bool
	RateAllowance   *big.Int
	LockupAllowance *big.Int
	RateUsage       *big.Int
	LockupUsage     *big.Int
	MaxLockupPeriod *big.Int
}, error) {
	return _Payments.Contract.OperatorApprovals(&_Payments.CallOpts, token, client, operator)
}

// BurnForFees is a paid mutator transaction binding the contract method 0x1a257300.
//
// Solidity: function burnForFees(address token, address recipient, uint256 requested) payable returns()
func (_Payments *PaymentsTransactor) BurnForFees(opts *bind.TransactOpts, token common.Address, recipient common.Address, requested *big.Int) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "burnForFees", token, recipient, requested)
}

// BurnForFees is a paid mutator transaction binding the contract method 0x1a257300.
//
// Solidity: function burnForFees(address token, address recipient, uint256 requested) payable returns()
func (_Payments *PaymentsSession) BurnForFees(token common.Address, recipient common.Address, requested *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.BurnForFees(&_Payments.TransactOpts, token, recipient, requested)
}

// BurnForFees is a paid mutator transaction binding the contract method 0x1a257300.
//
// Solidity: function burnForFees(address token, address recipient, uint256 requested) payable returns()
func (_Payments *PaymentsTransactorSession) BurnForFees(token common.Address, recipient common.Address, requested *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.BurnForFees(&_Payments.TransactOpts, token, recipient, requested)
}

// CreateRail is a paid mutator transaction binding the contract method 0xf9f78de8.
//
// Solidity: function createRail(address token, address from, address to, address validator, uint256 commissionRateBps, address serviceFeeRecipient) returns(uint256)
func (_Payments *PaymentsTransactor) CreateRail(opts *bind.TransactOpts, token common.Address, from common.Address, to common.Address, validator common.Address, commissionRateBps *big.Int, serviceFeeRecipient common.Address) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "createRail", token, from, to, validator, commissionRateBps, serviceFeeRecipient)
}

// CreateRail is a paid mutator transaction binding the contract method 0xf9f78de8.
//
// Solidity: function createRail(address token, address from, address to, address validator, uint256 commissionRateBps, address serviceFeeRecipient) returns(uint256)
func (_Payments *PaymentsSession) CreateRail(token common.Address, from common.Address, to common.Address, validator common.Address, commissionRateBps *big.Int, serviceFeeRecipient common.Address) (*types.Transaction, error) {
	return _Payments.Contract.CreateRail(&_Payments.TransactOpts, token, from, to, validator, commissionRateBps, serviceFeeRecipient)
}

// CreateRail is a paid mutator transaction binding the contract method 0xf9f78de8.
//
// Solidity: function createRail(address token, address from, address to, address validator, uint256 commissionRateBps, address serviceFeeRecipient) returns(uint256)
func (_Payments *PaymentsTransactorSession) CreateRail(token common.Address, from common.Address, to common.Address, validator common.Address, commissionRateBps *big.Int, serviceFeeRecipient common.Address) (*types.Transaction, error) {
	return _Payments.Contract.CreateRail(&_Payments.TransactOpts, token, from, to, validator, commissionRateBps, serviceFeeRecipient)
}

// Deposit is a paid mutator transaction binding the contract method 0x8340f549.
//
// Solidity: function deposit(address token, address to, uint256 amount) payable returns()
func (_Payments *PaymentsTransactor) Deposit(opts *bind.TransactOpts, token common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "deposit", token, to, amount)
}

// Deposit is a paid mutator transaction binding the contract method 0x8340f549.
//
// Solidity: function deposit(address token, address to, uint256 amount) payable returns()
func (_Payments *PaymentsSession) Deposit(token common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.Deposit(&_Payments.TransactOpts, token, to, amount)
}

// Deposit is a paid mutator transaction binding the contract method 0x8340f549.
//
// Solidity: function deposit(address token, address to, uint256 amount) payable returns()
func (_Payments *PaymentsTransactorSession) Deposit(token common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.Deposit(&_Payments.TransactOpts, token, to, amount)
}

// DepositWithAuthorization is a paid mutator transaction binding the contract method 0x8a94d4fc.
//
// Solidity: function depositWithAuthorization(address token, address to, uint256 amount, uint256 validAfter, uint256 validBefore, bytes32 nonce, uint8 v, bytes32 r, bytes32 s) returns()
func (_Payments *PaymentsTransactor) DepositWithAuthorization(opts *bind.TransactOpts, token common.Address, to common.Address, amount *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "depositWithAuthorization", token, to, amount, validAfter, validBefore, nonce, v, r, s)
}

// DepositWithAuthorization is a paid mutator transaction binding the contract method 0x8a94d4fc.
//
// Solidity: function depositWithAuthorization(address token, address to, uint256 amount, uint256 validAfter, uint256 validBefore, bytes32 nonce, uint8 v, bytes32 r, bytes32 s) returns()
func (_Payments *PaymentsSession) DepositWithAuthorization(token common.Address, to common.Address, amount *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _Payments.Contract.DepositWithAuthorization(&_Payments.TransactOpts, token, to, amount, validAfter, validBefore, nonce, v, r, s)
}

// DepositWithAuthorization is a paid mutator transaction binding the contract method 0x8a94d4fc.
//
// Solidity: function depositWithAuthorization(address token, address to, uint256 amount, uint256 validAfter, uint256 validBefore, bytes32 nonce, uint8 v, bytes32 r, bytes32 s) returns()
func (_Payments *PaymentsTransactorSession) DepositWithAuthorization(token common.Address, to common.Address, amount *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _Payments.Contract.DepositWithAuthorization(&_Payments.TransactOpts, token, to, amount, validAfter, validBefore, nonce, v, r, s)
}

// DepositWithAuthorizationAndApproveOperator is a paid mutator transaction binding the contract method 0x18ccb209.
//
// Solidity: function depositWithAuthorizationAndApproveOperator(address token, address to, uint256 amount, uint256 validAfter, uint256 validBefore, bytes32 nonce, uint8 v, bytes32 r, bytes32 s, address operator, uint256 rateAllowance, uint256 lockupAllowance, uint256 maxLockupPeriod) returns()
func (_Payments *PaymentsTransactor) DepositWithAuthorizationAndApproveOperator(opts *bind.TransactOpts, token common.Address, to common.Address, amount *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, v uint8, r [32]byte, s [32]byte, operator common.Address, rateAllowance *big.Int, lockupAllowance *big.Int, maxLockupPeriod *big.Int) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "depositWithAuthorizationAndApproveOperator", token, to, amount, validAfter, validBefore, nonce, v, r, s, operator, rateAllowance, lockupAllowance, maxLockupPeriod)
}

// DepositWithAuthorizationAndApproveOperator is a paid mutator transaction binding the contract method 0x18ccb209.
//
// Solidity: function depositWithAuthorizationAndApproveOperator(address token, address to, uint256 amount, uint256 validAfter, uint256 validBefore, bytes32 nonce, uint8 v, bytes32 r, bytes32 s, address operator, uint256 rateAllowance, uint256 lockupAllowance, uint256 maxLockupPeriod) returns()
func (_Payments *PaymentsSession) DepositWithAuthorizationAndApproveOperator(token common.Address, to common.Address, amount *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, v uint8, r [32]byte, s [32]byte, operator common.Address, rateAllowance *big.Int, lockupAllowance *big.Int, maxLockupPeriod *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.DepositWithAuthorizationAndApproveOperator(&_Payments.TransactOpts, token, to, amount, validAfter, validBefore, nonce, v, r, s, operator, rateAllowance, lockupAllowance, maxLockupPeriod)
}

// DepositWithAuthorizationAndApproveOperator is a paid mutator transaction binding the contract method 0x18ccb209.
//
// Solidity: function depositWithAuthorizationAndApproveOperator(address token, address to, uint256 amount, uint256 validAfter, uint256 validBefore, bytes32 nonce, uint8 v, bytes32 r, bytes32 s, address operator, uint256 rateAllowance, uint256 lockupAllowance, uint256 maxLockupPeriod) returns()
func (_Payments *PaymentsTransactorSession) DepositWithAuthorizationAndApproveOperator(token common.Address, to common.Address, amount *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, v uint8, r [32]byte, s [32]byte, operator common.Address, rateAllowance *big.Int, lockupAllowance *big.Int, maxLockupPeriod *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.DepositWithAuthorizationAndApproveOperator(&_Payments.TransactOpts, token, to, amount, validAfter, validBefore, nonce, v, r, s, operator, rateAllowance, lockupAllowance, maxLockupPeriod)
}

// DepositWithAuthorizationAndIncreaseOperatorApproval is a paid mutator transaction binding the contract method 0xdcaad80b.
//
// Solidity: function depositWithAuthorizationAndIncreaseOperatorApproval(address token, address to, uint256 amount, uint256 validAfter, uint256 validBefore, bytes32 nonce, uint8 v, bytes32 r, bytes32 s, address operator, uint256 rateAllowanceIncrease, uint256 lockupAllowanceIncrease) returns()
func (_Payments *PaymentsTransactor) DepositWithAuthorizationAndIncreaseOperatorApproval(opts *bind.TransactOpts, token common.Address, to common.Address, amount *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, v uint8, r [32]byte, s [32]byte, operator common.Address, rateAllowanceIncrease *big.Int, lockupAllowanceIncrease *big.Int) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "depositWithAuthorizationAndIncreaseOperatorApproval", token, to, amount, validAfter, validBefore, nonce, v, r, s, operator, rateAllowanceIncrease, lockupAllowanceIncrease)
}

// DepositWithAuthorizationAndIncreaseOperatorApproval is a paid mutator transaction binding the contract method 0xdcaad80b.
//
// Solidity: function depositWithAuthorizationAndIncreaseOperatorApproval(address token, address to, uint256 amount, uint256 validAfter, uint256 validBefore, bytes32 nonce, uint8 v, bytes32 r, bytes32 s, address operator, uint256 rateAllowanceIncrease, uint256 lockupAllowanceIncrease) returns()
func (_Payments *PaymentsSession) DepositWithAuthorizationAndIncreaseOperatorApproval(token common.Address, to common.Address, amount *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, v uint8, r [32]byte, s [32]byte, operator common.Address, rateAllowanceIncrease *big.Int, lockupAllowanceIncrease *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.DepositWithAuthorizationAndIncreaseOperatorApproval(&_Payments.TransactOpts, token, to, amount, validAfter, validBefore, nonce, v, r, s, operator, rateAllowanceIncrease, lockupAllowanceIncrease)
}

// DepositWithAuthorizationAndIncreaseOperatorApproval is a paid mutator transaction binding the contract method 0xdcaad80b.
//
// Solidity: function depositWithAuthorizationAndIncreaseOperatorApproval(address token, address to, uint256 amount, uint256 validAfter, uint256 validBefore, bytes32 nonce, uint8 v, bytes32 r, bytes32 s, address operator, uint256 rateAllowanceIncrease, uint256 lockupAllowanceIncrease) returns()
func (_Payments *PaymentsTransactorSession) DepositWithAuthorizationAndIncreaseOperatorApproval(token common.Address, to common.Address, amount *big.Int, validAfter *big.Int, validBefore *big.Int, nonce [32]byte, v uint8, r [32]byte, s [32]byte, operator common.Address, rateAllowanceIncrease *big.Int, lockupAllowanceIncrease *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.DepositWithAuthorizationAndIncreaseOperatorApproval(&_Payments.TransactOpts, token, to, amount, validAfter, validBefore, nonce, v, r, s, operator, rateAllowanceIncrease, lockupAllowanceIncrease)
}

// DepositWithPermit is a paid mutator transaction binding the contract method 0x8ef59739.
//
// Solidity: function depositWithPermit(address token, address to, uint256 amount, uint256 deadline, uint8 v, bytes32 r, bytes32 s) returns()
func (_Payments *PaymentsTransactor) DepositWithPermit(opts *bind.TransactOpts, token common.Address, to common.Address, amount *big.Int, deadline *big.Int, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "depositWithPermit", token, to, amount, deadline, v, r, s)
}

// DepositWithPermit is a paid mutator transaction binding the contract method 0x8ef59739.
//
// Solidity: function depositWithPermit(address token, address to, uint256 amount, uint256 deadline, uint8 v, bytes32 r, bytes32 s) returns()
func (_Payments *PaymentsSession) DepositWithPermit(token common.Address, to common.Address, amount *big.Int, deadline *big.Int, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _Payments.Contract.DepositWithPermit(&_Payments.TransactOpts, token, to, amount, deadline, v, r, s)
}

// DepositWithPermit is a paid mutator transaction binding the contract method 0x8ef59739.
//
// Solidity: function depositWithPermit(address token, address to, uint256 amount, uint256 deadline, uint8 v, bytes32 r, bytes32 s) returns()
func (_Payments *PaymentsTransactorSession) DepositWithPermit(token common.Address, to common.Address, amount *big.Int, deadline *big.Int, v uint8, r [32]byte, s [32]byte) (*types.Transaction, error) {
	return _Payments.Contract.DepositWithPermit(&_Payments.TransactOpts, token, to, amount, deadline, v, r, s)
}

// DepositWithPermitAndApproveOperator is a paid mutator transaction binding the contract method 0x7218b707.
//
// Solidity: function depositWithPermitAndApproveOperator(address token, address to, uint256 amount, uint256 deadline, uint8 v, bytes32 r, bytes32 s, address operator, uint256 rateAllowance, uint256 lockupAllowance, uint256 maxLockupPeriod) returns()
func (_Payments *PaymentsTransactor) DepositWithPermitAndApproveOperator(opts *bind.TransactOpts, token common.Address, to common.Address, amount *big.Int, deadline *big.Int, v uint8, r [32]byte, s [32]byte, operator common.Address, rateAllowance *big.Int, lockupAllowance *big.Int, maxLockupPeriod *big.Int) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "depositWithPermitAndApproveOperator", token, to, amount, deadline, v, r, s, operator, rateAllowance, lockupAllowance, maxLockupPeriod)
}

// DepositWithPermitAndApproveOperator is a paid mutator transaction binding the contract method 0x7218b707.
//
// Solidity: function depositWithPermitAndApproveOperator(address token, address to, uint256 amount, uint256 deadline, uint8 v, bytes32 r, bytes32 s, address operator, uint256 rateAllowance, uint256 lockupAllowance, uint256 maxLockupPeriod) returns()
func (_Payments *PaymentsSession) DepositWithPermitAndApproveOperator(token common.Address, to common.Address, amount *big.Int, deadline *big.Int, v uint8, r [32]byte, s [32]byte, operator common.Address, rateAllowance *big.Int, lockupAllowance *big.Int, maxLockupPeriod *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.DepositWithPermitAndApproveOperator(&_Payments.TransactOpts, token, to, amount, deadline, v, r, s, operator, rateAllowance, lockupAllowance, maxLockupPeriod)
}

// DepositWithPermitAndApproveOperator is a paid mutator transaction binding the contract method 0x7218b707.
//
// Solidity: function depositWithPermitAndApproveOperator(address token, address to, uint256 amount, uint256 deadline, uint8 v, bytes32 r, bytes32 s, address operator, uint256 rateAllowance, uint256 lockupAllowance, uint256 maxLockupPeriod) returns()
func (_Payments *PaymentsTransactorSession) DepositWithPermitAndApproveOperator(token common.Address, to common.Address, amount *big.Int, deadline *big.Int, v uint8, r [32]byte, s [32]byte, operator common.Address, rateAllowance *big.Int, lockupAllowance *big.Int, maxLockupPeriod *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.DepositWithPermitAndApproveOperator(&_Payments.TransactOpts, token, to, amount, deadline, v, r, s, operator, rateAllowance, lockupAllowance, maxLockupPeriod)
}

// DepositWithPermitAndIncreaseOperatorApproval is a paid mutator transaction binding the contract method 0x56b29efe.
//
// Solidity: function depositWithPermitAndIncreaseOperatorApproval(address token, address to, uint256 amount, uint256 deadline, uint8 v, bytes32 r, bytes32 s, address operator, uint256 rateAllowanceIncrease, uint256 lockupAllowanceIncrease) returns()
func (_Payments *PaymentsTransactor) DepositWithPermitAndIncreaseOperatorApproval(opts *bind.TransactOpts, token common.Address, to common.Address, amount *big.Int, deadline *big.Int, v uint8, r [32]byte, s [32]byte, operator common.Address, rateAllowanceIncrease *big.Int, lockupAllowanceIncrease *big.Int) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "depositWithPermitAndIncreaseOperatorApproval", token, to, amount, deadline, v, r, s, operator, rateAllowanceIncrease, lockupAllowanceIncrease)
}

// DepositWithPermitAndIncreaseOperatorApproval is a paid mutator transaction binding the contract method 0x56b29efe.
//
// Solidity: function depositWithPermitAndIncreaseOperatorApproval(address token, address to, uint256 amount, uint256 deadline, uint8 v, bytes32 r, bytes32 s, address operator, uint256 rateAllowanceIncrease, uint256 lockupAllowanceIncrease) returns()
func (_Payments *PaymentsSession) DepositWithPermitAndIncreaseOperatorApproval(token common.Address, to common.Address, amount *big.Int, deadline *big.Int, v uint8, r [32]byte, s [32]byte, operator common.Address, rateAllowanceIncrease *big.Int, lockupAllowanceIncrease *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.DepositWithPermitAndIncreaseOperatorApproval(&_Payments.TransactOpts, token, to, amount, deadline, v, r, s, operator, rateAllowanceIncrease, lockupAllowanceIncrease)
}

// DepositWithPermitAndIncreaseOperatorApproval is a paid mutator transaction binding the contract method 0x56b29efe.
//
// Solidity: function depositWithPermitAndIncreaseOperatorApproval(address token, address to, uint256 amount, uint256 deadline, uint8 v, bytes32 r, bytes32 s, address operator, uint256 rateAllowanceIncrease, uint256 lockupAllowanceIncrease) returns()
func (_Payments *PaymentsTransactorSession) DepositWithPermitAndIncreaseOperatorApproval(token common.Address, to common.Address, amount *big.Int, deadline *big.Int, v uint8, r [32]byte, s [32]byte, operator common.Address, rateAllowanceIncrease *big.Int, lockupAllowanceIncrease *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.DepositWithPermitAndIncreaseOperatorApproval(&_Payments.TransactOpts, token, to, amount, deadline, v, r, s, operator, rateAllowanceIncrease, lockupAllowanceIncrease)
}

// IncreaseOperatorApproval is a paid mutator transaction binding the contract method 0xa159b1ed.
//
// Solidity: function increaseOperatorApproval(address token, address operator, uint256 rateAllowanceIncrease, uint256 lockupAllowanceIncrease) returns()
func (_Payments *PaymentsTransactor) IncreaseOperatorApproval(opts *bind.TransactOpts, token common.Address, operator common.Address, rateAllowanceIncrease *big.Int, lockupAllowanceIncrease *big.Int) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "increaseOperatorApproval", token, operator, rateAllowanceIncrease, lockupAllowanceIncrease)
}

// IncreaseOperatorApproval is a paid mutator transaction binding the contract method 0xa159b1ed.
//
// Solidity: function increaseOperatorApproval(address token, address operator, uint256 rateAllowanceIncrease, uint256 lockupAllowanceIncrease) returns()
func (_Payments *PaymentsSession) IncreaseOperatorApproval(token common.Address, operator common.Address, rateAllowanceIncrease *big.Int, lockupAllowanceIncrease *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.IncreaseOperatorApproval(&_Payments.TransactOpts, token, operator, rateAllowanceIncrease, lockupAllowanceIncrease)
}

// IncreaseOperatorApproval is a paid mutator transaction binding the contract method 0xa159b1ed.
//
// Solidity: function increaseOperatorApproval(address token, address operator, uint256 rateAllowanceIncrease, uint256 lockupAllowanceIncrease) returns()
func (_Payments *PaymentsTransactorSession) IncreaseOperatorApproval(token common.Address, operator common.Address, rateAllowanceIncrease *big.Int, lockupAllowanceIncrease *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.IncreaseOperatorApproval(&_Payments.TransactOpts, token, operator, rateAllowanceIncrease, lockupAllowanceIncrease)
}

// ModifyRailLockup is a paid mutator transaction binding the contract method 0xde07b8bb.
//
// Solidity: function modifyRailLockup(uint256 railId, uint256 period, uint256 lockupFixed) returns()
func (_Payments *PaymentsTransactor) ModifyRailLockup(opts *bind.TransactOpts, railId *big.Int, period *big.Int, lockupFixed *big.Int) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "modifyRailLockup", railId, period, lockupFixed)
}

// ModifyRailLockup is a paid mutator transaction binding the contract method 0xde07b8bb.
//
// Solidity: function modifyRailLockup(uint256 railId, uint256 period, uint256 lockupFixed) returns()
func (_Payments *PaymentsSession) ModifyRailLockup(railId *big.Int, period *big.Int, lockupFixed *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.ModifyRailLockup(&_Payments.TransactOpts, railId, period, lockupFixed)
}

// ModifyRailLockup is a paid mutator transaction binding the contract method 0xde07b8bb.
//
// Solidity: function modifyRailLockup(uint256 railId, uint256 period, uint256 lockupFixed) returns()
func (_Payments *PaymentsTransactorSession) ModifyRailLockup(railId *big.Int, period *big.Int, lockupFixed *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.ModifyRailLockup(&_Payments.TransactOpts, railId, period, lockupFixed)
}

// ModifyRailPayment is a paid mutator transaction binding the contract method 0x97d3ea34.
//
// Solidity: function modifyRailPayment(uint256 railId, uint256 newRate, uint256 oneTimePayment) returns()
func (_Payments *PaymentsTransactor) ModifyRailPayment(opts *bind.TransactOpts, railId *big.Int, newRate *big.Int, oneTimePayment *big.Int) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "modifyRailPayment", railId, newRate, oneTimePayment)
}

// ModifyRailPayment is a paid mutator transaction binding the contract method 0x97d3ea34.
//
// Solidity: function modifyRailPayment(uint256 railId, uint256 newRate, uint256 oneTimePayment) returns()
func (_Payments *PaymentsSession) ModifyRailPayment(railId *big.Int, newRate *big.Int, oneTimePayment *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.ModifyRailPayment(&_Payments.TransactOpts, railId, newRate, oneTimePayment)
}

// ModifyRailPayment is a paid mutator transaction binding the contract method 0x97d3ea34.
//
// Solidity: function modifyRailPayment(uint256 railId, uint256 newRate, uint256 oneTimePayment) returns()
func (_Payments *PaymentsTransactorSession) ModifyRailPayment(railId *big.Int, newRate *big.Int, oneTimePayment *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.ModifyRailPayment(&_Payments.TransactOpts, railId, newRate, oneTimePayment)
}

// SetOperatorApproval is a paid mutator transaction binding the contract method 0x875bc8b6.
//
// Solidity: function setOperatorApproval(address token, address operator, bool approved, uint256 rateAllowance, uint256 lockupAllowance, uint256 maxLockupPeriod) returns()
func (_Payments *PaymentsTransactor) SetOperatorApproval(opts *bind.TransactOpts, token common.Address, operator common.Address, approved bool, rateAllowance *big.Int, lockupAllowance *big.Int, maxLockupPeriod *big.Int) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "setOperatorApproval", token, operator, approved, rateAllowance, lockupAllowance, maxLockupPeriod)
}

// SetOperatorApproval is a paid mutator transaction binding the contract method 0x875bc8b6.
//
// Solidity: function setOperatorApproval(address token, address operator, bool approved, uint256 rateAllowance, uint256 lockupAllowance, uint256 maxLockupPeriod) returns()
func (_Payments *PaymentsSession) SetOperatorApproval(token common.Address, operator common.Address, approved bool, rateAllowance *big.Int, lockupAllowance *big.Int, maxLockupPeriod *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.SetOperatorApproval(&_Payments.TransactOpts, token, operator, approved, rateAllowance, lockupAllowance, maxLockupPeriod)
}

// SetOperatorApproval is a paid mutator transaction binding the contract method 0x875bc8b6.
//
// Solidity: function setOperatorApproval(address token, address operator, bool approved, uint256 rateAllowance, uint256 lockupAllowance, uint256 maxLockupPeriod) returns()
func (_Payments *PaymentsTransactorSession) SetOperatorApproval(token common.Address, operator common.Address, approved bool, rateAllowance *big.Int, lockupAllowance *big.Int, maxLockupPeriod *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.SetOperatorApproval(&_Payments.TransactOpts, token, operator, approved, rateAllowance, lockupAllowance, maxLockupPeriod)
}

// SettleRail is a paid mutator transaction binding the contract method 0xbcd40bf8.
//
// Solidity: function settleRail(uint256 railId, uint256 untilEpoch) returns(uint256 totalSettledAmount, uint256 totalNetPayeeAmount, uint256 totalOperatorCommission, uint256 totalNetworkFee, uint256 finalSettledEpoch, string note)
func (_Payments *PaymentsTransactor) SettleRail(opts *bind.TransactOpts, railId *big.Int, untilEpoch *big.Int) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "settleRail", railId, untilEpoch)
}

// SettleRail is a paid mutator transaction binding the contract method 0xbcd40bf8.
//
// Solidity: function settleRail(uint256 railId, uint256 untilEpoch) returns(uint256 totalSettledAmount, uint256 totalNetPayeeAmount, uint256 totalOperatorCommission, uint256 totalNetworkFee, uint256 finalSettledEpoch, string note)
func (_Payments *PaymentsSession) SettleRail(railId *big.Int, untilEpoch *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.SettleRail(&_Payments.TransactOpts, railId, untilEpoch)
}

// SettleRail is a paid mutator transaction binding the contract method 0xbcd40bf8.
//
// Solidity: function settleRail(uint256 railId, uint256 untilEpoch) returns(uint256 totalSettledAmount, uint256 totalNetPayeeAmount, uint256 totalOperatorCommission, uint256 totalNetworkFee, uint256 finalSettledEpoch, string note)
func (_Payments *PaymentsTransactorSession) SettleRail(railId *big.Int, untilEpoch *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.SettleRail(&_Payments.TransactOpts, railId, untilEpoch)
}

// SettleTerminatedRailWithoutValidation is a paid mutator transaction binding the contract method 0x4341325c.
//
// Solidity: function settleTerminatedRailWithoutValidation(uint256 railId) returns(uint256 totalSettledAmount, uint256 totalNetPayeeAmount, uint256 totalOperatorCommission, uint256 totalNetworkFee, uint256 finalSettledEpoch, string note)
func (_Payments *PaymentsTransactor) SettleTerminatedRailWithoutValidation(opts *bind.TransactOpts, railId *big.Int) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "settleTerminatedRailWithoutValidation", railId)
}

// SettleTerminatedRailWithoutValidation is a paid mutator transaction binding the contract method 0x4341325c.
//
// Solidity: function settleTerminatedRailWithoutValidation(uint256 railId) returns(uint256 totalSettledAmount, uint256 totalNetPayeeAmount, uint256 totalOperatorCommission, uint256 totalNetworkFee, uint256 finalSettledEpoch, string note)
func (_Payments *PaymentsSession) SettleTerminatedRailWithoutValidation(railId *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.SettleTerminatedRailWithoutValidation(&_Payments.TransactOpts, railId)
}

// SettleTerminatedRailWithoutValidation is a paid mutator transaction binding the contract method 0x4341325c.
//
// Solidity: function settleTerminatedRailWithoutValidation(uint256 railId) returns(uint256 totalSettledAmount, uint256 totalNetPayeeAmount, uint256 totalOperatorCommission, uint256 totalNetworkFee, uint256 finalSettledEpoch, string note)
func (_Payments *PaymentsTransactorSession) SettleTerminatedRailWithoutValidation(railId *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.SettleTerminatedRailWithoutValidation(&_Payments.TransactOpts, railId)
}

// TerminateRail is a paid mutator transaction binding the contract method 0xcbb0bf18.
//
// Solidity: function terminateRail(uint256 railId) returns()
func (_Payments *PaymentsTransactor) TerminateRail(opts *bind.TransactOpts, railId *big.Int) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "terminateRail", railId)
}

// TerminateRail is a paid mutator transaction binding the contract method 0xcbb0bf18.
//
// Solidity: function terminateRail(uint256 railId) returns()
func (_Payments *PaymentsSession) TerminateRail(railId *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.TerminateRail(&_Payments.TransactOpts, railId)
}

// TerminateRail is a paid mutator transaction binding the contract method 0xcbb0bf18.
//
// Solidity: function terminateRail(uint256 railId) returns()
func (_Payments *PaymentsTransactorSession) TerminateRail(railId *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.TerminateRail(&_Payments.TransactOpts, railId)
}

// Withdraw is a paid mutator transaction binding the contract method 0xf3fef3a3.
//
// Solidity: function withdraw(address token, uint256 amount) returns()
func (_Payments *PaymentsTransactor) Withdraw(opts *bind.TransactOpts, token common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "withdraw", token, amount)
}

// Withdraw is a paid mutator transaction binding the contract method 0xf3fef3a3.
//
// Solidity: function withdraw(address token, uint256 amount) returns()
func (_Payments *PaymentsSession) Withdraw(token common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.Withdraw(&_Payments.TransactOpts, token, amount)
}

// Withdraw is a paid mutator transaction binding the contract method 0xf3fef3a3.
//
// Solidity: function withdraw(address token, uint256 amount) returns()
func (_Payments *PaymentsTransactorSession) Withdraw(token common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.Withdraw(&_Payments.TransactOpts, token, amount)
}

// WithdrawTo is a paid mutator transaction binding the contract method 0xc3b35a7e.
//
// Solidity: function withdrawTo(address token, address to, uint256 amount) returns()
func (_Payments *PaymentsTransactor) WithdrawTo(opts *bind.TransactOpts, token common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Payments.contract.Transact(opts, "withdrawTo", token, to, amount)
}

// WithdrawTo is a paid mutator transaction binding the contract method 0xc3b35a7e.
//
// Solidity: function withdrawTo(address token, address to, uint256 amount) returns()
func (_Payments *PaymentsSession) WithdrawTo(token common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.WithdrawTo(&_Payments.TransactOpts, token, to, amount)
}

// WithdrawTo is a paid mutator transaction binding the contract method 0xc3b35a7e.
//
// Solidity: function withdrawTo(address token, address to, uint256 amount) returns()
func (_Payments *PaymentsTransactorSession) WithdrawTo(token common.Address, to common.Address, amount *big.Int) (*types.Transaction, error) {
	return _Payments.Contract.WithdrawTo(&_Payments.TransactOpts, token, to, amount)
}

// PaymentsAccountLockupSettledIterator is returned from FilterAccountLockupSettled and is used to iterate over the raw logs and unpacked data for AccountLockupSettled events raised by the Payments contract.
type PaymentsAccountLockupSettledIterator struct {
	Event *PaymentsAccountLockupSettled // Event containing the contract specifics and raw log

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
func (it *PaymentsAccountLockupSettledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PaymentsAccountLockupSettled)
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
		it.Event = new(PaymentsAccountLockupSettled)
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
func (it *PaymentsAccountLockupSettledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PaymentsAccountLockupSettledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PaymentsAccountLockupSettled represents a AccountLockupSettled event raised by the Payments contract.
type PaymentsAccountLockupSettled struct {
	Token               common.Address
	Owner               common.Address
	LockupCurrent       *big.Int
	LockupRate          *big.Int
	LockupLastSettledAt *big.Int
	Raw                 types.Log // Blockchain specific contextual infos
}

// FilterAccountLockupSettled is a free log retrieval operation binding the contract event 0x25db253b018b2168f226371d77fc91f15152c02e8242c25af92a8271d239f450.
//
// Solidity: event AccountLockupSettled(address indexed token, address indexed owner, uint256 lockupCurrent, uint256 lockupRate, uint256 lockupLastSettledAt)
func (_Payments *PaymentsFilterer) FilterAccountLockupSettled(opts *bind.FilterOpts, token []common.Address, owner []common.Address) (*PaymentsAccountLockupSettledIterator, error) {

	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Payments.contract.FilterLogs(opts, "AccountLockupSettled", tokenRule, ownerRule)
	if err != nil {
		return nil, err
	}
	return &PaymentsAccountLockupSettledIterator{contract: _Payments.contract, event: "AccountLockupSettled", logs: logs, sub: sub}, nil
}

// WatchAccountLockupSettled is a free log subscription operation binding the contract event 0x25db253b018b2168f226371d77fc91f15152c02e8242c25af92a8271d239f450.
//
// Solidity: event AccountLockupSettled(address indexed token, address indexed owner, uint256 lockupCurrent, uint256 lockupRate, uint256 lockupLastSettledAt)
func (_Payments *PaymentsFilterer) WatchAccountLockupSettled(opts *bind.WatchOpts, sink chan<- *PaymentsAccountLockupSettled, token []common.Address, owner []common.Address) (event.Subscription, error) {

	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _Payments.contract.WatchLogs(opts, "AccountLockupSettled", tokenRule, ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PaymentsAccountLockupSettled)
				if err := _Payments.contract.UnpackLog(event, "AccountLockupSettled", log); err != nil {
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

// ParseAccountLockupSettled is a log parse operation binding the contract event 0x25db253b018b2168f226371d77fc91f15152c02e8242c25af92a8271d239f450.
//
// Solidity: event AccountLockupSettled(address indexed token, address indexed owner, uint256 lockupCurrent, uint256 lockupRate, uint256 lockupLastSettledAt)
func (_Payments *PaymentsFilterer) ParseAccountLockupSettled(log types.Log) (*PaymentsAccountLockupSettled, error) {
	event := new(PaymentsAccountLockupSettled)
	if err := _Payments.contract.UnpackLog(event, "AccountLockupSettled", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PaymentsDepositRecordedIterator is returned from FilterDepositRecorded and is used to iterate over the raw logs and unpacked data for DepositRecorded events raised by the Payments contract.
type PaymentsDepositRecordedIterator struct {
	Event *PaymentsDepositRecorded // Event containing the contract specifics and raw log

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
func (it *PaymentsDepositRecordedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PaymentsDepositRecorded)
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
		it.Event = new(PaymentsDepositRecorded)
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
func (it *PaymentsDepositRecordedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PaymentsDepositRecordedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PaymentsDepositRecorded represents a DepositRecorded event raised by the Payments contract.
type PaymentsDepositRecorded struct {
	Token  common.Address
	From   common.Address
	To     common.Address
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterDepositRecorded is a free log retrieval operation binding the contract event 0x0dc0013c9d314fc3894bafe429b311ffbd18598c3d159a5a0e31225215db94a7.
//
// Solidity: event DepositRecorded(address indexed token, address indexed from, address indexed to, uint256 amount)
func (_Payments *PaymentsFilterer) FilterDepositRecorded(opts *bind.FilterOpts, token []common.Address, from []common.Address, to []common.Address) (*PaymentsDepositRecordedIterator, error) {

	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _Payments.contract.FilterLogs(opts, "DepositRecorded", tokenRule, fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return &PaymentsDepositRecordedIterator{contract: _Payments.contract, event: "DepositRecorded", logs: logs, sub: sub}, nil
}

// WatchDepositRecorded is a free log subscription operation binding the contract event 0x0dc0013c9d314fc3894bafe429b311ffbd18598c3d159a5a0e31225215db94a7.
//
// Solidity: event DepositRecorded(address indexed token, address indexed from, address indexed to, uint256 amount)
func (_Payments *PaymentsFilterer) WatchDepositRecorded(opts *bind.WatchOpts, sink chan<- *PaymentsDepositRecorded, token []common.Address, from []common.Address, to []common.Address) (event.Subscription, error) {

	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _Payments.contract.WatchLogs(opts, "DepositRecorded", tokenRule, fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PaymentsDepositRecorded)
				if err := _Payments.contract.UnpackLog(event, "DepositRecorded", log); err != nil {
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

// ParseDepositRecorded is a log parse operation binding the contract event 0x0dc0013c9d314fc3894bafe429b311ffbd18598c3d159a5a0e31225215db94a7.
//
// Solidity: event DepositRecorded(address indexed token, address indexed from, address indexed to, uint256 amount)
func (_Payments *PaymentsFilterer) ParseDepositRecorded(log types.Log) (*PaymentsDepositRecorded, error) {
	event := new(PaymentsDepositRecorded)
	if err := _Payments.contract.UnpackLog(event, "DepositRecorded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PaymentsOperatorApprovalUpdatedIterator is returned from FilterOperatorApprovalUpdated and is used to iterate over the raw logs and unpacked data for OperatorApprovalUpdated events raised by the Payments contract.
type PaymentsOperatorApprovalUpdatedIterator struct {
	Event *PaymentsOperatorApprovalUpdated // Event containing the contract specifics and raw log

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
func (it *PaymentsOperatorApprovalUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PaymentsOperatorApprovalUpdated)
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
		it.Event = new(PaymentsOperatorApprovalUpdated)
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
func (it *PaymentsOperatorApprovalUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PaymentsOperatorApprovalUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PaymentsOperatorApprovalUpdated represents a OperatorApprovalUpdated event raised by the Payments contract.
type PaymentsOperatorApprovalUpdated struct {
	Token           common.Address
	Client          common.Address
	Operator        common.Address
	Approved        bool
	RateAllowance   *big.Int
	LockupAllowance *big.Int
	MaxLockupPeriod *big.Int
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterOperatorApprovalUpdated is a free log retrieval operation binding the contract event 0x9f4ee4f42b9fb561fb251246fa9cabfe12aeed51f1c615a17f34e5c0575b4fc8.
//
// Solidity: event OperatorApprovalUpdated(address indexed token, address indexed client, address indexed operator, bool approved, uint256 rateAllowance, uint256 lockupAllowance, uint256 maxLockupPeriod)
func (_Payments *PaymentsFilterer) FilterOperatorApprovalUpdated(opts *bind.FilterOpts, token []common.Address, client []common.Address, operator []common.Address) (*PaymentsOperatorApprovalUpdatedIterator, error) {

	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var clientRule []interface{}
	for _, clientItem := range client {
		clientRule = append(clientRule, clientItem)
	}
	var operatorRule []interface{}
	for _, operatorItem := range operator {
		operatorRule = append(operatorRule, operatorItem)
	}

	logs, sub, err := _Payments.contract.FilterLogs(opts, "OperatorApprovalUpdated", tokenRule, clientRule, operatorRule)
	if err != nil {
		return nil, err
	}
	return &PaymentsOperatorApprovalUpdatedIterator{contract: _Payments.contract, event: "OperatorApprovalUpdated", logs: logs, sub: sub}, nil
}

// WatchOperatorApprovalUpdated is a free log subscription operation binding the contract event 0x9f4ee4f42b9fb561fb251246fa9cabfe12aeed51f1c615a17f34e5c0575b4fc8.
//
// Solidity: event OperatorApprovalUpdated(address indexed token, address indexed client, address indexed operator, bool approved, uint256 rateAllowance, uint256 lockupAllowance, uint256 maxLockupPeriod)
func (_Payments *PaymentsFilterer) WatchOperatorApprovalUpdated(opts *bind.WatchOpts, sink chan<- *PaymentsOperatorApprovalUpdated, token []common.Address, client []common.Address, operator []common.Address) (event.Subscription, error) {

	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var clientRule []interface{}
	for _, clientItem := range client {
		clientRule = append(clientRule, clientItem)
	}
	var operatorRule []interface{}
	for _, operatorItem := range operator {
		operatorRule = append(operatorRule, operatorItem)
	}

	logs, sub, err := _Payments.contract.WatchLogs(opts, "OperatorApprovalUpdated", tokenRule, clientRule, operatorRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PaymentsOperatorApprovalUpdated)
				if err := _Payments.contract.UnpackLog(event, "OperatorApprovalUpdated", log); err != nil {
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

// ParseOperatorApprovalUpdated is a log parse operation binding the contract event 0x9f4ee4f42b9fb561fb251246fa9cabfe12aeed51f1c615a17f34e5c0575b4fc8.
//
// Solidity: event OperatorApprovalUpdated(address indexed token, address indexed client, address indexed operator, bool approved, uint256 rateAllowance, uint256 lockupAllowance, uint256 maxLockupPeriod)
func (_Payments *PaymentsFilterer) ParseOperatorApprovalUpdated(log types.Log) (*PaymentsOperatorApprovalUpdated, error) {
	event := new(PaymentsOperatorApprovalUpdated)
	if err := _Payments.contract.UnpackLog(event, "OperatorApprovalUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PaymentsRailCreatedIterator is returned from FilterRailCreated and is used to iterate over the raw logs and unpacked data for RailCreated events raised by the Payments contract.
type PaymentsRailCreatedIterator struct {
	Event *PaymentsRailCreated // Event containing the contract specifics and raw log

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
func (it *PaymentsRailCreatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PaymentsRailCreated)
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
		it.Event = new(PaymentsRailCreated)
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
func (it *PaymentsRailCreatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PaymentsRailCreatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PaymentsRailCreated represents a RailCreated event raised by the Payments contract.
type PaymentsRailCreated struct {
	RailId              *big.Int
	Payer               common.Address
	Payee               common.Address
	Token               common.Address
	Operator            common.Address
	Validator           common.Address
	ServiceFeeRecipient common.Address
	CommissionRateBps   *big.Int
	Raw                 types.Log // Blockchain specific contextual infos
}

// FilterRailCreated is a free log retrieval operation binding the contract event 0xb9f4f448b1c10a427fd0df9553b65fbd49cea0137977ce50f8deb47864b4754f.
//
// Solidity: event RailCreated(uint256 indexed railId, address indexed payer, address indexed payee, address token, address operator, address validator, address serviceFeeRecipient, uint256 commissionRateBps)
func (_Payments *PaymentsFilterer) FilterRailCreated(opts *bind.FilterOpts, railId []*big.Int, payer []common.Address, payee []common.Address) (*PaymentsRailCreatedIterator, error) {

	var railIdRule []interface{}
	for _, railIdItem := range railId {
		railIdRule = append(railIdRule, railIdItem)
	}
	var payerRule []interface{}
	for _, payerItem := range payer {
		payerRule = append(payerRule, payerItem)
	}
	var payeeRule []interface{}
	for _, payeeItem := range payee {
		payeeRule = append(payeeRule, payeeItem)
	}

	logs, sub, err := _Payments.contract.FilterLogs(opts, "RailCreated", railIdRule, payerRule, payeeRule)
	if err != nil {
		return nil, err
	}
	return &PaymentsRailCreatedIterator{contract: _Payments.contract, event: "RailCreated", logs: logs, sub: sub}, nil
}

// WatchRailCreated is a free log subscription operation binding the contract event 0xb9f4f448b1c10a427fd0df9553b65fbd49cea0137977ce50f8deb47864b4754f.
//
// Solidity: event RailCreated(uint256 indexed railId, address indexed payer, address indexed payee, address token, address operator, address validator, address serviceFeeRecipient, uint256 commissionRateBps)
func (_Payments *PaymentsFilterer) WatchRailCreated(opts *bind.WatchOpts, sink chan<- *PaymentsRailCreated, railId []*big.Int, payer []common.Address, payee []common.Address) (event.Subscription, error) {

	var railIdRule []interface{}
	for _, railIdItem := range railId {
		railIdRule = append(railIdRule, railIdItem)
	}
	var payerRule []interface{}
	for _, payerItem := range payer {
		payerRule = append(payerRule, payerItem)
	}
	var payeeRule []interface{}
	for _, payeeItem := range payee {
		payeeRule = append(payeeRule, payeeItem)
	}

	logs, sub, err := _Payments.contract.WatchLogs(opts, "RailCreated", railIdRule, payerRule, payeeRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PaymentsRailCreated)
				if err := _Payments.contract.UnpackLog(event, "RailCreated", log); err != nil {
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

// ParseRailCreated is a log parse operation binding the contract event 0xb9f4f448b1c10a427fd0df9553b65fbd49cea0137977ce50f8deb47864b4754f.
//
// Solidity: event RailCreated(uint256 indexed railId, address indexed payer, address indexed payee, address token, address operator, address validator, address serviceFeeRecipient, uint256 commissionRateBps)
func (_Payments *PaymentsFilterer) ParseRailCreated(log types.Log) (*PaymentsRailCreated, error) {
	event := new(PaymentsRailCreated)
	if err := _Payments.contract.UnpackLog(event, "RailCreated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PaymentsRailFinalizedIterator is returned from FilterRailFinalized and is used to iterate over the raw logs and unpacked data for RailFinalized events raised by the Payments contract.
type PaymentsRailFinalizedIterator struct {
	Event *PaymentsRailFinalized // Event containing the contract specifics and raw log

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
func (it *PaymentsRailFinalizedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PaymentsRailFinalized)
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
		it.Event = new(PaymentsRailFinalized)
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
func (it *PaymentsRailFinalizedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PaymentsRailFinalizedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PaymentsRailFinalized represents a RailFinalized event raised by the Payments contract.
type PaymentsRailFinalized struct {
	RailId *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterRailFinalized is a free log retrieval operation binding the contract event 0xeba1d176034891f68b755fb52cf844fe98a96ca13b50147fbe0e93f6cdecd9e2.
//
// Solidity: event RailFinalized(uint256 indexed railId)
func (_Payments *PaymentsFilterer) FilterRailFinalized(opts *bind.FilterOpts, railId []*big.Int) (*PaymentsRailFinalizedIterator, error) {

	var railIdRule []interface{}
	for _, railIdItem := range railId {
		railIdRule = append(railIdRule, railIdItem)
	}

	logs, sub, err := _Payments.contract.FilterLogs(opts, "RailFinalized", railIdRule)
	if err != nil {
		return nil, err
	}
	return &PaymentsRailFinalizedIterator{contract: _Payments.contract, event: "RailFinalized", logs: logs, sub: sub}, nil
}

// WatchRailFinalized is a free log subscription operation binding the contract event 0xeba1d176034891f68b755fb52cf844fe98a96ca13b50147fbe0e93f6cdecd9e2.
//
// Solidity: event RailFinalized(uint256 indexed railId)
func (_Payments *PaymentsFilterer) WatchRailFinalized(opts *bind.WatchOpts, sink chan<- *PaymentsRailFinalized, railId []*big.Int) (event.Subscription, error) {

	var railIdRule []interface{}
	for _, railIdItem := range railId {
		railIdRule = append(railIdRule, railIdItem)
	}

	logs, sub, err := _Payments.contract.WatchLogs(opts, "RailFinalized", railIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PaymentsRailFinalized)
				if err := _Payments.contract.UnpackLog(event, "RailFinalized", log); err != nil {
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

// ParseRailFinalized is a log parse operation binding the contract event 0xeba1d176034891f68b755fb52cf844fe98a96ca13b50147fbe0e93f6cdecd9e2.
//
// Solidity: event RailFinalized(uint256 indexed railId)
func (_Payments *PaymentsFilterer) ParseRailFinalized(log types.Log) (*PaymentsRailFinalized, error) {
	event := new(PaymentsRailFinalized)
	if err := _Payments.contract.UnpackLog(event, "RailFinalized", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PaymentsRailLockupModifiedIterator is returned from FilterRailLockupModified and is used to iterate over the raw logs and unpacked data for RailLockupModified events raised by the Payments contract.
type PaymentsRailLockupModifiedIterator struct {
	Event *PaymentsRailLockupModified // Event containing the contract specifics and raw log

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
func (it *PaymentsRailLockupModifiedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PaymentsRailLockupModified)
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
		it.Event = new(PaymentsRailLockupModified)
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
func (it *PaymentsRailLockupModifiedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PaymentsRailLockupModifiedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PaymentsRailLockupModified represents a RailLockupModified event raised by the Payments contract.
type PaymentsRailLockupModified struct {
	RailId          *big.Int
	OldLockupPeriod *big.Int
	NewLockupPeriod *big.Int
	OldLockupFixed  *big.Int
	NewLockupFixed  *big.Int
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterRailLockupModified is a free log retrieval operation binding the contract event 0xcceff3285f15292e6ad0acd5900af1575f7e0debe13855d76901c33981978f79.
//
// Solidity: event RailLockupModified(uint256 indexed railId, uint256 oldLockupPeriod, uint256 newLockupPeriod, uint256 oldLockupFixed, uint256 newLockupFixed)
func (_Payments *PaymentsFilterer) FilterRailLockupModified(opts *bind.FilterOpts, railId []*big.Int) (*PaymentsRailLockupModifiedIterator, error) {

	var railIdRule []interface{}
	for _, railIdItem := range railId {
		railIdRule = append(railIdRule, railIdItem)
	}

	logs, sub, err := _Payments.contract.FilterLogs(opts, "RailLockupModified", railIdRule)
	if err != nil {
		return nil, err
	}
	return &PaymentsRailLockupModifiedIterator{contract: _Payments.contract, event: "RailLockupModified", logs: logs, sub: sub}, nil
}

// WatchRailLockupModified is a free log subscription operation binding the contract event 0xcceff3285f15292e6ad0acd5900af1575f7e0debe13855d76901c33981978f79.
//
// Solidity: event RailLockupModified(uint256 indexed railId, uint256 oldLockupPeriod, uint256 newLockupPeriod, uint256 oldLockupFixed, uint256 newLockupFixed)
func (_Payments *PaymentsFilterer) WatchRailLockupModified(opts *bind.WatchOpts, sink chan<- *PaymentsRailLockupModified, railId []*big.Int) (event.Subscription, error) {

	var railIdRule []interface{}
	for _, railIdItem := range railId {
		railIdRule = append(railIdRule, railIdItem)
	}

	logs, sub, err := _Payments.contract.WatchLogs(opts, "RailLockupModified", railIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PaymentsRailLockupModified)
				if err := _Payments.contract.UnpackLog(event, "RailLockupModified", log); err != nil {
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

// ParseRailLockupModified is a log parse operation binding the contract event 0xcceff3285f15292e6ad0acd5900af1575f7e0debe13855d76901c33981978f79.
//
// Solidity: event RailLockupModified(uint256 indexed railId, uint256 oldLockupPeriod, uint256 newLockupPeriod, uint256 oldLockupFixed, uint256 newLockupFixed)
func (_Payments *PaymentsFilterer) ParseRailLockupModified(log types.Log) (*PaymentsRailLockupModified, error) {
	event := new(PaymentsRailLockupModified)
	if err := _Payments.contract.UnpackLog(event, "RailLockupModified", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PaymentsRailOneTimePaymentProcessedIterator is returned from FilterRailOneTimePaymentProcessed and is used to iterate over the raw logs and unpacked data for RailOneTimePaymentProcessed events raised by the Payments contract.
type PaymentsRailOneTimePaymentProcessedIterator struct {
	Event *PaymentsRailOneTimePaymentProcessed // Event containing the contract specifics and raw log

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
func (it *PaymentsRailOneTimePaymentProcessedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PaymentsRailOneTimePaymentProcessed)
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
		it.Event = new(PaymentsRailOneTimePaymentProcessed)
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
func (it *PaymentsRailOneTimePaymentProcessedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PaymentsRailOneTimePaymentProcessedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PaymentsRailOneTimePaymentProcessed represents a RailOneTimePaymentProcessed event raised by the Payments contract.
type PaymentsRailOneTimePaymentProcessed struct {
	RailId             *big.Int
	NetPayeeAmount     *big.Int
	OperatorCommission *big.Int
	NetworkFee         *big.Int
	Raw                types.Log // Blockchain specific contextual infos
}

// FilterRailOneTimePaymentProcessed is a free log retrieval operation binding the contract event 0x70358589bc618854360f545817cd39ae53b440c5c6ef7bb83db1c86f3496f723.
//
// Solidity: event RailOneTimePaymentProcessed(uint256 indexed railId, uint256 netPayeeAmount, uint256 operatorCommission, uint256 networkFee)
func (_Payments *PaymentsFilterer) FilterRailOneTimePaymentProcessed(opts *bind.FilterOpts, railId []*big.Int) (*PaymentsRailOneTimePaymentProcessedIterator, error) {

	var railIdRule []interface{}
	for _, railIdItem := range railId {
		railIdRule = append(railIdRule, railIdItem)
	}

	logs, sub, err := _Payments.contract.FilterLogs(opts, "RailOneTimePaymentProcessed", railIdRule)
	if err != nil {
		return nil, err
	}
	return &PaymentsRailOneTimePaymentProcessedIterator{contract: _Payments.contract, event: "RailOneTimePaymentProcessed", logs: logs, sub: sub}, nil
}

// WatchRailOneTimePaymentProcessed is a free log subscription operation binding the contract event 0x70358589bc618854360f545817cd39ae53b440c5c6ef7bb83db1c86f3496f723.
//
// Solidity: event RailOneTimePaymentProcessed(uint256 indexed railId, uint256 netPayeeAmount, uint256 operatorCommission, uint256 networkFee)
func (_Payments *PaymentsFilterer) WatchRailOneTimePaymentProcessed(opts *bind.WatchOpts, sink chan<- *PaymentsRailOneTimePaymentProcessed, railId []*big.Int) (event.Subscription, error) {

	var railIdRule []interface{}
	for _, railIdItem := range railId {
		railIdRule = append(railIdRule, railIdItem)
	}

	logs, sub, err := _Payments.contract.WatchLogs(opts, "RailOneTimePaymentProcessed", railIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PaymentsRailOneTimePaymentProcessed)
				if err := _Payments.contract.UnpackLog(event, "RailOneTimePaymentProcessed", log); err != nil {
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

// ParseRailOneTimePaymentProcessed is a log parse operation binding the contract event 0x70358589bc618854360f545817cd39ae53b440c5c6ef7bb83db1c86f3496f723.
//
// Solidity: event RailOneTimePaymentProcessed(uint256 indexed railId, uint256 netPayeeAmount, uint256 operatorCommission, uint256 networkFee)
func (_Payments *PaymentsFilterer) ParseRailOneTimePaymentProcessed(log types.Log) (*PaymentsRailOneTimePaymentProcessed, error) {
	event := new(PaymentsRailOneTimePaymentProcessed)
	if err := _Payments.contract.UnpackLog(event, "RailOneTimePaymentProcessed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PaymentsRailRateModifiedIterator is returned from FilterRailRateModified and is used to iterate over the raw logs and unpacked data for RailRateModified events raised by the Payments contract.
type PaymentsRailRateModifiedIterator struct {
	Event *PaymentsRailRateModified // Event containing the contract specifics and raw log

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
func (it *PaymentsRailRateModifiedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PaymentsRailRateModified)
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
		it.Event = new(PaymentsRailRateModified)
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
func (it *PaymentsRailRateModifiedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PaymentsRailRateModifiedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PaymentsRailRateModified represents a RailRateModified event raised by the Payments contract.
type PaymentsRailRateModified struct {
	RailId  *big.Int
	OldRate *big.Int
	NewRate *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRailRateModified is a free log retrieval operation binding the contract event 0x2e3c2d5cce45fbe45262be6ec0c3f584e0ba1ccd0f7371dd1175dbde62ec2a50.
//
// Solidity: event RailRateModified(uint256 indexed railId, uint256 oldRate, uint256 newRate)
func (_Payments *PaymentsFilterer) FilterRailRateModified(opts *bind.FilterOpts, railId []*big.Int) (*PaymentsRailRateModifiedIterator, error) {

	var railIdRule []interface{}
	for _, railIdItem := range railId {
		railIdRule = append(railIdRule, railIdItem)
	}

	logs, sub, err := _Payments.contract.FilterLogs(opts, "RailRateModified", railIdRule)
	if err != nil {
		return nil, err
	}
	return &PaymentsRailRateModifiedIterator{contract: _Payments.contract, event: "RailRateModified", logs: logs, sub: sub}, nil
}

// WatchRailRateModified is a free log subscription operation binding the contract event 0x2e3c2d5cce45fbe45262be6ec0c3f584e0ba1ccd0f7371dd1175dbde62ec2a50.
//
// Solidity: event RailRateModified(uint256 indexed railId, uint256 oldRate, uint256 newRate)
func (_Payments *PaymentsFilterer) WatchRailRateModified(opts *bind.WatchOpts, sink chan<- *PaymentsRailRateModified, railId []*big.Int) (event.Subscription, error) {

	var railIdRule []interface{}
	for _, railIdItem := range railId {
		railIdRule = append(railIdRule, railIdItem)
	}

	logs, sub, err := _Payments.contract.WatchLogs(opts, "RailRateModified", railIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PaymentsRailRateModified)
				if err := _Payments.contract.UnpackLog(event, "RailRateModified", log); err != nil {
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

// ParseRailRateModified is a log parse operation binding the contract event 0x2e3c2d5cce45fbe45262be6ec0c3f584e0ba1ccd0f7371dd1175dbde62ec2a50.
//
// Solidity: event RailRateModified(uint256 indexed railId, uint256 oldRate, uint256 newRate)
func (_Payments *PaymentsFilterer) ParseRailRateModified(log types.Log) (*PaymentsRailRateModified, error) {
	event := new(PaymentsRailRateModified)
	if err := _Payments.contract.UnpackLog(event, "RailRateModified", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PaymentsRailSettledIterator is returned from FilterRailSettled and is used to iterate over the raw logs and unpacked data for RailSettled events raised by the Payments contract.
type PaymentsRailSettledIterator struct {
	Event *PaymentsRailSettled // Event containing the contract specifics and raw log

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
func (it *PaymentsRailSettledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PaymentsRailSettled)
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
		it.Event = new(PaymentsRailSettled)
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
func (it *PaymentsRailSettledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PaymentsRailSettledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PaymentsRailSettled represents a RailSettled event raised by the Payments contract.
type PaymentsRailSettled struct {
	RailId              *big.Int
	TotalSettledAmount  *big.Int
	TotalNetPayeeAmount *big.Int
	OperatorCommission  *big.Int
	NetworkFee          *big.Int
	SettledUpTo         *big.Int
	Raw                 types.Log // Blockchain specific contextual infos
}

// FilterRailSettled is a free log retrieval operation binding the contract event 0x14e2efd598f2db6bfe762fcf9a830ffdfcba170d263d4a4956f36176ba82d3f3.
//
// Solidity: event RailSettled(uint256 indexed railId, uint256 totalSettledAmount, uint256 totalNetPayeeAmount, uint256 operatorCommission, uint256 networkFee, uint256 settledUpTo)
func (_Payments *PaymentsFilterer) FilterRailSettled(opts *bind.FilterOpts, railId []*big.Int) (*PaymentsRailSettledIterator, error) {

	var railIdRule []interface{}
	for _, railIdItem := range railId {
		railIdRule = append(railIdRule, railIdItem)
	}

	logs, sub, err := _Payments.contract.FilterLogs(opts, "RailSettled", railIdRule)
	if err != nil {
		return nil, err
	}
	return &PaymentsRailSettledIterator{contract: _Payments.contract, event: "RailSettled", logs: logs, sub: sub}, nil
}

// WatchRailSettled is a free log subscription operation binding the contract event 0x14e2efd598f2db6bfe762fcf9a830ffdfcba170d263d4a4956f36176ba82d3f3.
//
// Solidity: event RailSettled(uint256 indexed railId, uint256 totalSettledAmount, uint256 totalNetPayeeAmount, uint256 operatorCommission, uint256 networkFee, uint256 settledUpTo)
func (_Payments *PaymentsFilterer) WatchRailSettled(opts *bind.WatchOpts, sink chan<- *PaymentsRailSettled, railId []*big.Int) (event.Subscription, error) {

	var railIdRule []interface{}
	for _, railIdItem := range railId {
		railIdRule = append(railIdRule, railIdItem)
	}

	logs, sub, err := _Payments.contract.WatchLogs(opts, "RailSettled", railIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PaymentsRailSettled)
				if err := _Payments.contract.UnpackLog(event, "RailSettled", log); err != nil {
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

// ParseRailSettled is a log parse operation binding the contract event 0x14e2efd598f2db6bfe762fcf9a830ffdfcba170d263d4a4956f36176ba82d3f3.
//
// Solidity: event RailSettled(uint256 indexed railId, uint256 totalSettledAmount, uint256 totalNetPayeeAmount, uint256 operatorCommission, uint256 networkFee, uint256 settledUpTo)
func (_Payments *PaymentsFilterer) ParseRailSettled(log types.Log) (*PaymentsRailSettled, error) {
	event := new(PaymentsRailSettled)
	if err := _Payments.contract.UnpackLog(event, "RailSettled", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PaymentsRailTerminatedIterator is returned from FilterRailTerminated and is used to iterate over the raw logs and unpacked data for RailTerminated events raised by the Payments contract.
type PaymentsRailTerminatedIterator struct {
	Event *PaymentsRailTerminated // Event containing the contract specifics and raw log

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
func (it *PaymentsRailTerminatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PaymentsRailTerminated)
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
		it.Event = new(PaymentsRailTerminated)
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
func (it *PaymentsRailTerminatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PaymentsRailTerminatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PaymentsRailTerminated represents a RailTerminated event raised by the Payments contract.
type PaymentsRailTerminated struct {
	RailId   *big.Int
	By       common.Address
	EndEpoch *big.Int
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterRailTerminated is a free log retrieval operation binding the contract event 0x341cedeea2157541f32a2c3ba561c2a096f12997813844db9818532104a41aa9.
//
// Solidity: event RailTerminated(uint256 indexed railId, address indexed by, uint256 endEpoch)
func (_Payments *PaymentsFilterer) FilterRailTerminated(opts *bind.FilterOpts, railId []*big.Int, by []common.Address) (*PaymentsRailTerminatedIterator, error) {

	var railIdRule []interface{}
	for _, railIdItem := range railId {
		railIdRule = append(railIdRule, railIdItem)
	}
	var byRule []interface{}
	for _, byItem := range by {
		byRule = append(byRule, byItem)
	}

	logs, sub, err := _Payments.contract.FilterLogs(opts, "RailTerminated", railIdRule, byRule)
	if err != nil {
		return nil, err
	}
	return &PaymentsRailTerminatedIterator{contract: _Payments.contract, event: "RailTerminated", logs: logs, sub: sub}, nil
}

// WatchRailTerminated is a free log subscription operation binding the contract event 0x341cedeea2157541f32a2c3ba561c2a096f12997813844db9818532104a41aa9.
//
// Solidity: event RailTerminated(uint256 indexed railId, address indexed by, uint256 endEpoch)
func (_Payments *PaymentsFilterer) WatchRailTerminated(opts *bind.WatchOpts, sink chan<- *PaymentsRailTerminated, railId []*big.Int, by []common.Address) (event.Subscription, error) {

	var railIdRule []interface{}
	for _, railIdItem := range railId {
		railIdRule = append(railIdRule, railIdItem)
	}
	var byRule []interface{}
	for _, byItem := range by {
		byRule = append(byRule, byItem)
	}

	logs, sub, err := _Payments.contract.WatchLogs(opts, "RailTerminated", railIdRule, byRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PaymentsRailTerminated)
				if err := _Payments.contract.UnpackLog(event, "RailTerminated", log); err != nil {
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

// ParseRailTerminated is a log parse operation binding the contract event 0x341cedeea2157541f32a2c3ba561c2a096f12997813844db9818532104a41aa9.
//
// Solidity: event RailTerminated(uint256 indexed railId, address indexed by, uint256 endEpoch)
func (_Payments *PaymentsFilterer) ParseRailTerminated(log types.Log) (*PaymentsRailTerminated, error) {
	event := new(PaymentsRailTerminated)
	if err := _Payments.contract.UnpackLog(event, "RailTerminated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PaymentsWithdrawRecordedIterator is returned from FilterWithdrawRecorded and is used to iterate over the raw logs and unpacked data for WithdrawRecorded events raised by the Payments contract.
type PaymentsWithdrawRecordedIterator struct {
	Event *PaymentsWithdrawRecorded // Event containing the contract specifics and raw log

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
func (it *PaymentsWithdrawRecordedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PaymentsWithdrawRecorded)
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
		it.Event = new(PaymentsWithdrawRecorded)
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
func (it *PaymentsWithdrawRecordedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PaymentsWithdrawRecordedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PaymentsWithdrawRecorded represents a WithdrawRecorded event raised by the Payments contract.
type PaymentsWithdrawRecorded struct {
	Token  common.Address
	From   common.Address
	To     common.Address
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterWithdrawRecorded is a free log retrieval operation binding the contract event 0x332e20fbeb87ed1d267a2f391e6e3c6bdb9932c83d0cee5b5594ba827c4326c5.
//
// Solidity: event WithdrawRecorded(address indexed token, address indexed from, address indexed to, uint256 amount)
func (_Payments *PaymentsFilterer) FilterWithdrawRecorded(opts *bind.FilterOpts, token []common.Address, from []common.Address, to []common.Address) (*PaymentsWithdrawRecordedIterator, error) {

	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _Payments.contract.FilterLogs(opts, "WithdrawRecorded", tokenRule, fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return &PaymentsWithdrawRecordedIterator{contract: _Payments.contract, event: "WithdrawRecorded", logs: logs, sub: sub}, nil
}

// WatchWithdrawRecorded is a free log subscription operation binding the contract event 0x332e20fbeb87ed1d267a2f391e6e3c6bdb9932c83d0cee5b5594ba827c4326c5.
//
// Solidity: event WithdrawRecorded(address indexed token, address indexed from, address indexed to, uint256 amount)
func (_Payments *PaymentsFilterer) WatchWithdrawRecorded(opts *bind.WatchOpts, sink chan<- *PaymentsWithdrawRecorded, token []common.Address, from []common.Address, to []common.Address) (event.Subscription, error) {

	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _Payments.contract.WatchLogs(opts, "WithdrawRecorded", tokenRule, fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PaymentsWithdrawRecorded)
				if err := _Payments.contract.UnpackLog(event, "WithdrawRecorded", log); err != nil {
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

// ParseWithdrawRecorded is a log parse operation binding the contract event 0x332e20fbeb87ed1d267a2f391e6e3c6bdb9932c83d0cee5b5594ba827c4326c5.
//
// Solidity: event WithdrawRecorded(address indexed token, address indexed from, address indexed to, uint256 amount)
func (_Payments *PaymentsFilterer) ParseWithdrawRecorded(log types.Log) (*PaymentsWithdrawRecorded, error) {
	event := new(PaymentsWithdrawRecorded)
	if err := _Payments.contract.UnpackLog(event, "WithdrawRecorded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
