// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package contract

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

// CidsCid is an auto generated low-level Go binding around an user-defined struct.
type CidsCid struct {
	Data []byte
}

// IPDPTypesPieceIdAndOffset is an auto generated low-level Go binding around an user-defined struct.
type IPDPTypesPieceIdAndOffset struct {
	PieceId *big.Int
	Offset  *big.Int
}

// IPDPTypesProof is an auto generated low-level Go binding around an user-defined struct.
type IPDPTypesProof struct {
	Leaf  [32]byte
	Proof [][32]byte
}

// PDPVerifierMetaData contains all meta data concerning the PDPVerifier contract.
var PDPVerifierMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"EXTRA_DATA_MAX_SIZE\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"MAX_ENQUEUED_REMOVALS\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"MAX_PIECE_SIZE_LOG2\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"NO_CHALLENGE_SCHEDULED\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"NO_PROVEN_EPOCH\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"UPGRADE_INTERFACE_VERSION\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"VERSION\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"addPieces\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"listenerAddr\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"pieceData\",\"type\":\"tuple[]\",\"internalType\":\"structCids.Cid[]\",\"components\":[{\"name\":\"data\",\"type\":\"bytes\",\"internalType\":\"bytes\"}]},{\"name\":\"extraData\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"payable\"},{\"type\":\"function\",\"name\":\"calculateProofFee\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"calculateProofFeeForSize\",\"inputs\":[{\"name\":\"rawSize\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"claimDataSetStorageProvider\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"extraData\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"createDataSet\",\"inputs\":[{\"name\":\"listenerAddr\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"extraData\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"payable\"},{\"type\":\"function\",\"name\":\"dataSetLive\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"deleteDataSet\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"extraData\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"feeEffectiveTime\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"feePerTiB\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint96\",\"internalType\":\"uint96\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"findPieceIds\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"leafIndexs\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"}],\"outputs\":[{\"name\":\"\",\"type\":\"tuple[]\",\"internalType\":\"structIPDPTypes.PieceIdAndOffset[]\",\"components\":[{\"name\":\"pieceId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"offset\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getActivePieceCount\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"activeCount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getActivePieces\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"offset\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"limit\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"pieces\",\"type\":\"tuple[]\",\"internalType\":\"structCids.Cid[]\",\"components\":[{\"name\":\"data\",\"type\":\"bytes\",\"internalType\":\"bytes\"}]},{\"name\":\"pieceIds\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"},{\"name\":\"rawSizes\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"},{\"name\":\"hasMore\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getChallengeFinality\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getChallengeRange\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getDataSetLastProvenEpoch\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getDataSetLeafCount\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getDataSetListener\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getDataSetStorageProvider\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getNextChallengeEpoch\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getNextDataSetId\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getNextPieceId\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getPieceCid\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"pieceId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"tuple\",\"internalType\":\"structCids.Cid\",\"components\":[{\"name\":\"data\",\"type\":\"bytes\",\"internalType\":\"bytes\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getPieceLeafCount\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"pieceId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getRandomness\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getScheduledRemovals\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"initialize\",\"inputs\":[{\"name\":\"_challengeFinality\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"migrate\",\"inputs\":[],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"nextProvingPeriod\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"challengeEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"extraData\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"owner\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"pieceChallengable\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"pieceId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"pieceLive\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"pieceId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"proposeDataSetStorageProvider\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"newStorageProvider\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"proposedFeePerTiB\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint96\",\"internalType\":\"uint96\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"provePossession\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"proofs\",\"type\":\"tuple[]\",\"internalType\":\"structIPDPTypes.Proof[]\",\"components\":[{\"name\":\"leaf\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"proof\",\"type\":\"bytes32[]\",\"internalType\":\"bytes32[]\"}]}],\"outputs\":[],\"stateMutability\":\"payable\"},{\"type\":\"function\",\"name\":\"proxiableUUID\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"renounceOwnership\",\"inputs\":[],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"schedulePieceDeletions\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"pieceIds\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"},{\"name\":\"extraData\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"transferOwnership\",\"inputs\":[{\"name\":\"newOwner\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"updateProofFee\",\"inputs\":[{\"name\":\"newFeePerTiB\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"upgradeToAndCall\",\"inputs\":[{\"name\":\"newImplementation\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"data\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"payable\"},{\"type\":\"event\",\"name\":\"ContractUpgraded\",\"inputs\":[{\"name\":\"version\",\"type\":\"string\",\"indexed\":false,\"internalType\":\"string\"},{\"name\":\"implementation\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"DataSetCreated\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"storageProvider\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"DataSetDeleted\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"deletedLeafCount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"DataSetEmpty\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"FeeUpdateProposed\",\"inputs\":[{\"name\":\"currentFee\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"newFee\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"effectiveTime\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Initialized\",\"inputs\":[{\"name\":\"version\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"NextProvingPeriod\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"challengeEpoch\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"leafCount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"OwnershipTransferred\",\"inputs\":[{\"name\":\"previousOwner\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"newOwner\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"PiecesAdded\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"pieceIds\",\"type\":\"uint256[]\",\"indexed\":false,\"internalType\":\"uint256[]\"},{\"name\":\"pieceCids\",\"type\":\"tuple[]\",\"indexed\":false,\"internalType\":\"structCids.Cid[]\",\"components\":[{\"name\":\"data\",\"type\":\"bytes\",\"internalType\":\"bytes\"}]}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"PiecesRemoved\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"pieceIds\",\"type\":\"uint256[]\",\"indexed\":false,\"internalType\":\"uint256[]\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"PossessionProven\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"challenges\",\"type\":\"tuple[]\",\"indexed\":false,\"internalType\":\"structIPDPTypes.PieceIdAndOffset[]\",\"components\":[{\"name\":\"pieceId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"offset\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ProofFeePaid\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"fee\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"StorageProviderChanged\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"oldStorageProvider\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"newStorageProvider\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Upgraded\",\"inputs\":[{\"name\":\"implementation\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"error\",\"name\":\"AddressEmptyCode\",\"inputs\":[{\"name\":\"target\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"ERC1967InvalidImplementation\",\"inputs\":[{\"name\":\"implementation\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"ERC1967NonPayable\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"FailedCall\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"IndexedError\",\"inputs\":[{\"name\":\"idx\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"msg\",\"type\":\"string\",\"internalType\":\"string\"}]},{\"type\":\"error\",\"name\":\"InvalidInitialization\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"NotInitializing\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"OwnableInvalidOwner\",\"inputs\":[{\"name\":\"owner\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"OwnableUnauthorizedAccount\",\"inputs\":[{\"name\":\"account\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"UUPSUnauthorizedCallContext\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"UUPSUnsupportedProxiableUUID\",\"inputs\":[{\"name\":\"slot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}]}]",
}

// PDPVerifierABI is the input ABI used to generate the binding from.
// Deprecated: Use PDPVerifierMetaData.ABI instead.
var PDPVerifierABI = PDPVerifierMetaData.ABI

// PDPVerifier is an auto generated Go binding around an Ethereum contract.
type PDPVerifier struct {
	PDPVerifierCaller     // Read-only binding to the contract
	PDPVerifierTransactor // Write-only binding to the contract
	PDPVerifierFilterer   // Log filterer for contract events
}

// PDPVerifierCaller is an auto generated read-only Go binding around an Ethereum contract.
type PDPVerifierCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PDPVerifierTransactor is an auto generated write-only Go binding around an Ethereum contract.
type PDPVerifierTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PDPVerifierFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type PDPVerifierFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PDPVerifierSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type PDPVerifierSession struct {
	Contract     *PDPVerifier      // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// PDPVerifierCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type PDPVerifierCallerSession struct {
	Contract *PDPVerifierCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts      // Call options to use throughout this session
}

// PDPVerifierTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type PDPVerifierTransactorSession struct {
	Contract     *PDPVerifierTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts      // Transaction auth options to use throughout this session
}

// PDPVerifierRaw is an auto generated low-level Go binding around an Ethereum contract.
type PDPVerifierRaw struct {
	Contract *PDPVerifier // Generic contract binding to access the raw methods on
}

// PDPVerifierCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type PDPVerifierCallerRaw struct {
	Contract *PDPVerifierCaller // Generic read-only contract binding to access the raw methods on
}

// PDPVerifierTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type PDPVerifierTransactorRaw struct {
	Contract *PDPVerifierTransactor // Generic write-only contract binding to access the raw methods on
}

// NewPDPVerifier creates a new instance of PDPVerifier, bound to a specific deployed contract.
func NewPDPVerifier(address common.Address, backend bind.ContractBackend) (*PDPVerifier, error) {
	contract, err := bindPDPVerifier(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &PDPVerifier{PDPVerifierCaller: PDPVerifierCaller{contract: contract}, PDPVerifierTransactor: PDPVerifierTransactor{contract: contract}, PDPVerifierFilterer: PDPVerifierFilterer{contract: contract}}, nil
}

// NewPDPVerifierCaller creates a new read-only instance of PDPVerifier, bound to a specific deployed contract.
func NewPDPVerifierCaller(address common.Address, caller bind.ContractCaller) (*PDPVerifierCaller, error) {
	contract, err := bindPDPVerifier(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierCaller{contract: contract}, nil
}

// NewPDPVerifierTransactor creates a new write-only instance of PDPVerifier, bound to a specific deployed contract.
func NewPDPVerifierTransactor(address common.Address, transactor bind.ContractTransactor) (*PDPVerifierTransactor, error) {
	contract, err := bindPDPVerifier(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierTransactor{contract: contract}, nil
}

// NewPDPVerifierFilterer creates a new log filterer instance of PDPVerifier, bound to a specific deployed contract.
func NewPDPVerifierFilterer(address common.Address, filterer bind.ContractFilterer) (*PDPVerifierFilterer, error) {
	contract, err := bindPDPVerifier(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierFilterer{contract: contract}, nil
}

// bindPDPVerifier binds a generic wrapper to an already deployed contract.
func bindPDPVerifier(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := PDPVerifierMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_PDPVerifier *PDPVerifierRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _PDPVerifier.Contract.PDPVerifierCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_PDPVerifier *PDPVerifierRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _PDPVerifier.Contract.PDPVerifierTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_PDPVerifier *PDPVerifierRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _PDPVerifier.Contract.PDPVerifierTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_PDPVerifier *PDPVerifierCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _PDPVerifier.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_PDPVerifier *PDPVerifierTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _PDPVerifier.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_PDPVerifier *PDPVerifierTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _PDPVerifier.Contract.contract.Transact(opts, method, params...)
}

// EXTRADATAMAXSIZE is a free data retrieval call binding the contract method 0x029b4646.
//
// Solidity: function EXTRA_DATA_MAX_SIZE() view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) EXTRADATAMAXSIZE(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "EXTRA_DATA_MAX_SIZE")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// EXTRADATAMAXSIZE is a free data retrieval call binding the contract method 0x029b4646.
//
// Solidity: function EXTRA_DATA_MAX_SIZE() view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) EXTRADATAMAXSIZE() (*big.Int, error) {
	return _PDPVerifier.Contract.EXTRADATAMAXSIZE(&_PDPVerifier.CallOpts)
}

// EXTRADATAMAXSIZE is a free data retrieval call binding the contract method 0x029b4646.
//
// Solidity: function EXTRA_DATA_MAX_SIZE() view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) EXTRADATAMAXSIZE() (*big.Int, error) {
	return _PDPVerifier.Contract.EXTRADATAMAXSIZE(&_PDPVerifier.CallOpts)
}

// MAXENQUEUEDREMOVALS is a free data retrieval call binding the contract method 0x9f8cb3bd.
//
// Solidity: function MAX_ENQUEUED_REMOVALS() view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) MAXENQUEUEDREMOVALS(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "MAX_ENQUEUED_REMOVALS")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MAXENQUEUEDREMOVALS is a free data retrieval call binding the contract method 0x9f8cb3bd.
//
// Solidity: function MAX_ENQUEUED_REMOVALS() view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) MAXENQUEUEDREMOVALS() (*big.Int, error) {
	return _PDPVerifier.Contract.MAXENQUEUEDREMOVALS(&_PDPVerifier.CallOpts)
}

// MAXENQUEUEDREMOVALS is a free data retrieval call binding the contract method 0x9f8cb3bd.
//
// Solidity: function MAX_ENQUEUED_REMOVALS() view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) MAXENQUEUEDREMOVALS() (*big.Int, error) {
	return _PDPVerifier.Contract.MAXENQUEUEDREMOVALS(&_PDPVerifier.CallOpts)
}

// MAXPIECESIZELOG2 is a free data retrieval call binding the contract method 0xf8eb8276.
//
// Solidity: function MAX_PIECE_SIZE_LOG2() view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) MAXPIECESIZELOG2(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "MAX_PIECE_SIZE_LOG2")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MAXPIECESIZELOG2 is a free data retrieval call binding the contract method 0xf8eb8276.
//
// Solidity: function MAX_PIECE_SIZE_LOG2() view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) MAXPIECESIZELOG2() (*big.Int, error) {
	return _PDPVerifier.Contract.MAXPIECESIZELOG2(&_PDPVerifier.CallOpts)
}

// MAXPIECESIZELOG2 is a free data retrieval call binding the contract method 0xf8eb8276.
//
// Solidity: function MAX_PIECE_SIZE_LOG2() view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) MAXPIECESIZELOG2() (*big.Int, error) {
	return _PDPVerifier.Contract.MAXPIECESIZELOG2(&_PDPVerifier.CallOpts)
}

// NOCHALLENGESCHEDULED is a free data retrieval call binding the contract method 0x462dd449.
//
// Solidity: function NO_CHALLENGE_SCHEDULED() view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) NOCHALLENGESCHEDULED(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "NO_CHALLENGE_SCHEDULED")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// NOCHALLENGESCHEDULED is a free data retrieval call binding the contract method 0x462dd449.
//
// Solidity: function NO_CHALLENGE_SCHEDULED() view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) NOCHALLENGESCHEDULED() (*big.Int, error) {
	return _PDPVerifier.Contract.NOCHALLENGESCHEDULED(&_PDPVerifier.CallOpts)
}

// NOCHALLENGESCHEDULED is a free data retrieval call binding the contract method 0x462dd449.
//
// Solidity: function NO_CHALLENGE_SCHEDULED() view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) NOCHALLENGESCHEDULED() (*big.Int, error) {
	return _PDPVerifier.Contract.NOCHALLENGESCHEDULED(&_PDPVerifier.CallOpts)
}

// NOPROVENEPOCH is a free data retrieval call binding the contract method 0xf178b1be.
//
// Solidity: function NO_PROVEN_EPOCH() view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) NOPROVENEPOCH(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "NO_PROVEN_EPOCH")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// NOPROVENEPOCH is a free data retrieval call binding the contract method 0xf178b1be.
//
// Solidity: function NO_PROVEN_EPOCH() view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) NOPROVENEPOCH() (*big.Int, error) {
	return _PDPVerifier.Contract.NOPROVENEPOCH(&_PDPVerifier.CallOpts)
}

// NOPROVENEPOCH is a free data retrieval call binding the contract method 0xf178b1be.
//
// Solidity: function NO_PROVEN_EPOCH() view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) NOPROVENEPOCH() (*big.Int, error) {
	return _PDPVerifier.Contract.NOPROVENEPOCH(&_PDPVerifier.CallOpts)
}

// UPGRADEINTERFACEVERSION is a free data retrieval call binding the contract method 0xad3cb1cc.
//
// Solidity: function UPGRADE_INTERFACE_VERSION() view returns(string)
func (_PDPVerifier *PDPVerifierCaller) UPGRADEINTERFACEVERSION(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "UPGRADE_INTERFACE_VERSION")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// UPGRADEINTERFACEVERSION is a free data retrieval call binding the contract method 0xad3cb1cc.
//
// Solidity: function UPGRADE_INTERFACE_VERSION() view returns(string)
func (_PDPVerifier *PDPVerifierSession) UPGRADEINTERFACEVERSION() (string, error) {
	return _PDPVerifier.Contract.UPGRADEINTERFACEVERSION(&_PDPVerifier.CallOpts)
}

// UPGRADEINTERFACEVERSION is a free data retrieval call binding the contract method 0xad3cb1cc.
//
// Solidity: function UPGRADE_INTERFACE_VERSION() view returns(string)
func (_PDPVerifier *PDPVerifierCallerSession) UPGRADEINTERFACEVERSION() (string, error) {
	return _PDPVerifier.Contract.UPGRADEINTERFACEVERSION(&_PDPVerifier.CallOpts)
}

// VERSION is a free data retrieval call binding the contract method 0xffa1ad74.
//
// Solidity: function VERSION() view returns(string)
func (_PDPVerifier *PDPVerifierCaller) VERSION(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "VERSION")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// VERSION is a free data retrieval call binding the contract method 0xffa1ad74.
//
// Solidity: function VERSION() view returns(string)
func (_PDPVerifier *PDPVerifierSession) VERSION() (string, error) {
	return _PDPVerifier.Contract.VERSION(&_PDPVerifier.CallOpts)
}

// VERSION is a free data retrieval call binding the contract method 0xffa1ad74.
//
// Solidity: function VERSION() view returns(string)
func (_PDPVerifier *PDPVerifierCallerSession) VERSION() (string, error) {
	return _PDPVerifier.Contract.VERSION(&_PDPVerifier.CallOpts)
}

// CalculateProofFee is a free data retrieval call binding the contract method 0x86981308.
//
// Solidity: function calculateProofFee(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) CalculateProofFee(opts *bind.CallOpts, setId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "calculateProofFee", setId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// CalculateProofFee is a free data retrieval call binding the contract method 0x86981308.
//
// Solidity: function calculateProofFee(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) CalculateProofFee(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.CalculateProofFee(&_PDPVerifier.CallOpts, setId)
}

// CalculateProofFee is a free data retrieval call binding the contract method 0x86981308.
//
// Solidity: function calculateProofFee(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) CalculateProofFee(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.CalculateProofFee(&_PDPVerifier.CallOpts, setId)
}

// CalculateProofFeeForSize is a free data retrieval call binding the contract method 0xe9a31a55.
//
// Solidity: function calculateProofFeeForSize(uint256 rawSize) view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) CalculateProofFeeForSize(opts *bind.CallOpts, rawSize *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "calculateProofFeeForSize", rawSize)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// CalculateProofFeeForSize is a free data retrieval call binding the contract method 0xe9a31a55.
//
// Solidity: function calculateProofFeeForSize(uint256 rawSize) view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) CalculateProofFeeForSize(rawSize *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.CalculateProofFeeForSize(&_PDPVerifier.CallOpts, rawSize)
}

// CalculateProofFeeForSize is a free data retrieval call binding the contract method 0xe9a31a55.
//
// Solidity: function calculateProofFeeForSize(uint256 rawSize) view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) CalculateProofFeeForSize(rawSize *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.CalculateProofFeeForSize(&_PDPVerifier.CallOpts, rawSize)
}

// DataSetLive is a free data retrieval call binding the contract method 0xca759f27.
//
// Solidity: function dataSetLive(uint256 setId) view returns(bool)
func (_PDPVerifier *PDPVerifierCaller) DataSetLive(opts *bind.CallOpts, setId *big.Int) (bool, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "dataSetLive", setId)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// DataSetLive is a free data retrieval call binding the contract method 0xca759f27.
//
// Solidity: function dataSetLive(uint256 setId) view returns(bool)
func (_PDPVerifier *PDPVerifierSession) DataSetLive(setId *big.Int) (bool, error) {
	return _PDPVerifier.Contract.DataSetLive(&_PDPVerifier.CallOpts, setId)
}

// DataSetLive is a free data retrieval call binding the contract method 0xca759f27.
//
// Solidity: function dataSetLive(uint256 setId) view returns(bool)
func (_PDPVerifier *PDPVerifierCallerSession) DataSetLive(setId *big.Int) (bool, error) {
	return _PDPVerifier.Contract.DataSetLive(&_PDPVerifier.CallOpts, setId)
}

// FeeEffectiveTime is a free data retrieval call binding the contract method 0x996ad96a.
//
// Solidity: function feeEffectiveTime() view returns(uint64)
func (_PDPVerifier *PDPVerifierCaller) FeeEffectiveTime(opts *bind.CallOpts) (uint64, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "feeEffectiveTime")

	if err != nil {
		return *new(uint64), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)

	return out0, err

}

// FeeEffectiveTime is a free data retrieval call binding the contract method 0x996ad96a.
//
// Solidity: function feeEffectiveTime() view returns(uint64)
func (_PDPVerifier *PDPVerifierSession) FeeEffectiveTime() (uint64, error) {
	return _PDPVerifier.Contract.FeeEffectiveTime(&_PDPVerifier.CallOpts)
}

// FeeEffectiveTime is a free data retrieval call binding the contract method 0x996ad96a.
//
// Solidity: function feeEffectiveTime() view returns(uint64)
func (_PDPVerifier *PDPVerifierCallerSession) FeeEffectiveTime() (uint64, error) {
	return _PDPVerifier.Contract.FeeEffectiveTime(&_PDPVerifier.CallOpts)
}

// FeePerTiB is a free data retrieval call binding the contract method 0x22ef3f73.
//
// Solidity: function feePerTiB() view returns(uint96)
func (_PDPVerifier *PDPVerifierCaller) FeePerTiB(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "feePerTiB")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// FeePerTiB is a free data retrieval call binding the contract method 0x22ef3f73.
//
// Solidity: function feePerTiB() view returns(uint96)
func (_PDPVerifier *PDPVerifierSession) FeePerTiB() (*big.Int, error) {
	return _PDPVerifier.Contract.FeePerTiB(&_PDPVerifier.CallOpts)
}

// FeePerTiB is a free data retrieval call binding the contract method 0x22ef3f73.
//
// Solidity: function feePerTiB() view returns(uint96)
func (_PDPVerifier *PDPVerifierCallerSession) FeePerTiB() (*big.Int, error) {
	return _PDPVerifier.Contract.FeePerTiB(&_PDPVerifier.CallOpts)
}

// FindPieceIds is a free data retrieval call binding the contract method 0x349c9179.
//
// Solidity: function findPieceIds(uint256 setId, uint256[] leafIndexs) view returns((uint256,uint256)[])
func (_PDPVerifier *PDPVerifierCaller) FindPieceIds(opts *bind.CallOpts, setId *big.Int, leafIndexs []*big.Int) ([]IPDPTypesPieceIdAndOffset, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "findPieceIds", setId, leafIndexs)

	if err != nil {
		return *new([]IPDPTypesPieceIdAndOffset), err
	}

	out0 := *abi.ConvertType(out[0], new([]IPDPTypesPieceIdAndOffset)).(*[]IPDPTypesPieceIdAndOffset)

	return out0, err

}

// FindPieceIds is a free data retrieval call binding the contract method 0x349c9179.
//
// Solidity: function findPieceIds(uint256 setId, uint256[] leafIndexs) view returns((uint256,uint256)[])
func (_PDPVerifier *PDPVerifierSession) FindPieceIds(setId *big.Int, leafIndexs []*big.Int) ([]IPDPTypesPieceIdAndOffset, error) {
	return _PDPVerifier.Contract.FindPieceIds(&_PDPVerifier.CallOpts, setId, leafIndexs)
}

// FindPieceIds is a free data retrieval call binding the contract method 0x349c9179.
//
// Solidity: function findPieceIds(uint256 setId, uint256[] leafIndexs) view returns((uint256,uint256)[])
func (_PDPVerifier *PDPVerifierCallerSession) FindPieceIds(setId *big.Int, leafIndexs []*big.Int) ([]IPDPTypesPieceIdAndOffset, error) {
	return _PDPVerifier.Contract.FindPieceIds(&_PDPVerifier.CallOpts, setId, leafIndexs)
}

// GetActivePieceCount is a free data retrieval call binding the contract method 0x5353bdfd.
//
// Solidity: function getActivePieceCount(uint256 setId) view returns(uint256 activeCount)
func (_PDPVerifier *PDPVerifierCaller) GetActivePieceCount(opts *bind.CallOpts, setId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getActivePieceCount", setId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetActivePieceCount is a free data retrieval call binding the contract method 0x5353bdfd.
//
// Solidity: function getActivePieceCount(uint256 setId) view returns(uint256 activeCount)
func (_PDPVerifier *PDPVerifierSession) GetActivePieceCount(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetActivePieceCount(&_PDPVerifier.CallOpts, setId)
}

// GetActivePieceCount is a free data retrieval call binding the contract method 0x5353bdfd.
//
// Solidity: function getActivePieceCount(uint256 setId) view returns(uint256 activeCount)
func (_PDPVerifier *PDPVerifierCallerSession) GetActivePieceCount(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetActivePieceCount(&_PDPVerifier.CallOpts, setId)
}

// GetActivePieces is a free data retrieval call binding the contract method 0x39f51544.
//
// Solidity: function getActivePieces(uint256 setId, uint256 offset, uint256 limit) view returns((bytes)[] pieces, uint256[] pieceIds, uint256[] rawSizes, bool hasMore)
func (_PDPVerifier *PDPVerifierCaller) GetActivePieces(opts *bind.CallOpts, setId *big.Int, offset *big.Int, limit *big.Int) (struct {
	Pieces   []CidsCid
	PieceIds []*big.Int
	RawSizes []*big.Int
	HasMore  bool
}, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getActivePieces", setId, offset, limit)

	outstruct := new(struct {
		Pieces   []CidsCid
		PieceIds []*big.Int
		RawSizes []*big.Int
		HasMore  bool
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Pieces = *abi.ConvertType(out[0], new([]CidsCid)).(*[]CidsCid)
	outstruct.PieceIds = *abi.ConvertType(out[1], new([]*big.Int)).(*[]*big.Int)
	outstruct.RawSizes = *abi.ConvertType(out[2], new([]*big.Int)).(*[]*big.Int)
	outstruct.HasMore = *abi.ConvertType(out[3], new(bool)).(*bool)

	return *outstruct, err

}

// GetActivePieces is a free data retrieval call binding the contract method 0x39f51544.
//
// Solidity: function getActivePieces(uint256 setId, uint256 offset, uint256 limit) view returns((bytes)[] pieces, uint256[] pieceIds, uint256[] rawSizes, bool hasMore)
func (_PDPVerifier *PDPVerifierSession) GetActivePieces(setId *big.Int, offset *big.Int, limit *big.Int) (struct {
	Pieces   []CidsCid
	PieceIds []*big.Int
	RawSizes []*big.Int
	HasMore  bool
}, error) {
	return _PDPVerifier.Contract.GetActivePieces(&_PDPVerifier.CallOpts, setId, offset, limit)
}

// GetActivePieces is a free data retrieval call binding the contract method 0x39f51544.
//
// Solidity: function getActivePieces(uint256 setId, uint256 offset, uint256 limit) view returns((bytes)[] pieces, uint256[] pieceIds, uint256[] rawSizes, bool hasMore)
func (_PDPVerifier *PDPVerifierCallerSession) GetActivePieces(setId *big.Int, offset *big.Int, limit *big.Int) (struct {
	Pieces   []CidsCid
	PieceIds []*big.Int
	RawSizes []*big.Int
	HasMore  bool
}, error) {
	return _PDPVerifier.Contract.GetActivePieces(&_PDPVerifier.CallOpts, setId, offset, limit)
}

// GetChallengeFinality is a free data retrieval call binding the contract method 0xf83758fe.
//
// Solidity: function getChallengeFinality() view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) GetChallengeFinality(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getChallengeFinality")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetChallengeFinality is a free data retrieval call binding the contract method 0xf83758fe.
//
// Solidity: function getChallengeFinality() view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) GetChallengeFinality() (*big.Int, error) {
	return _PDPVerifier.Contract.GetChallengeFinality(&_PDPVerifier.CallOpts)
}

// GetChallengeFinality is a free data retrieval call binding the contract method 0xf83758fe.
//
// Solidity: function getChallengeFinality() view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) GetChallengeFinality() (*big.Int, error) {
	return _PDPVerifier.Contract.GetChallengeFinality(&_PDPVerifier.CallOpts)
}

// GetChallengeRange is a free data retrieval call binding the contract method 0x89208ba9.
//
// Solidity: function getChallengeRange(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) GetChallengeRange(opts *bind.CallOpts, setId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getChallengeRange", setId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetChallengeRange is a free data retrieval call binding the contract method 0x89208ba9.
//
// Solidity: function getChallengeRange(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) GetChallengeRange(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetChallengeRange(&_PDPVerifier.CallOpts, setId)
}

// GetChallengeRange is a free data retrieval call binding the contract method 0x89208ba9.
//
// Solidity: function getChallengeRange(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) GetChallengeRange(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetChallengeRange(&_PDPVerifier.CallOpts, setId)
}

// GetDataSetLastProvenEpoch is a free data retrieval call binding the contract method 0x04595c1a.
//
// Solidity: function getDataSetLastProvenEpoch(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) GetDataSetLastProvenEpoch(opts *bind.CallOpts, setId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getDataSetLastProvenEpoch", setId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetDataSetLastProvenEpoch is a free data retrieval call binding the contract method 0x04595c1a.
//
// Solidity: function getDataSetLastProvenEpoch(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) GetDataSetLastProvenEpoch(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetDataSetLastProvenEpoch(&_PDPVerifier.CallOpts, setId)
}

// GetDataSetLastProvenEpoch is a free data retrieval call binding the contract method 0x04595c1a.
//
// Solidity: function getDataSetLastProvenEpoch(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) GetDataSetLastProvenEpoch(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetDataSetLastProvenEpoch(&_PDPVerifier.CallOpts, setId)
}

// GetDataSetLeafCount is a free data retrieval call binding the contract method 0xa531998c.
//
// Solidity: function getDataSetLeafCount(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) GetDataSetLeafCount(opts *bind.CallOpts, setId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getDataSetLeafCount", setId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetDataSetLeafCount is a free data retrieval call binding the contract method 0xa531998c.
//
// Solidity: function getDataSetLeafCount(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) GetDataSetLeafCount(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetDataSetLeafCount(&_PDPVerifier.CallOpts, setId)
}

// GetDataSetLeafCount is a free data retrieval call binding the contract method 0xa531998c.
//
// Solidity: function getDataSetLeafCount(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) GetDataSetLeafCount(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetDataSetLeafCount(&_PDPVerifier.CallOpts, setId)
}

// GetDataSetListener is a free data retrieval call binding the contract method 0x2b3129bb.
//
// Solidity: function getDataSetListener(uint256 setId) view returns(address)
func (_PDPVerifier *PDPVerifierCaller) GetDataSetListener(opts *bind.CallOpts, setId *big.Int) (common.Address, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getDataSetListener", setId)

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// GetDataSetListener is a free data retrieval call binding the contract method 0x2b3129bb.
//
// Solidity: function getDataSetListener(uint256 setId) view returns(address)
func (_PDPVerifier *PDPVerifierSession) GetDataSetListener(setId *big.Int) (common.Address, error) {
	return _PDPVerifier.Contract.GetDataSetListener(&_PDPVerifier.CallOpts, setId)
}

// GetDataSetListener is a free data retrieval call binding the contract method 0x2b3129bb.
//
// Solidity: function getDataSetListener(uint256 setId) view returns(address)
func (_PDPVerifier *PDPVerifierCallerSession) GetDataSetListener(setId *big.Int) (common.Address, error) {
	return _PDPVerifier.Contract.GetDataSetListener(&_PDPVerifier.CallOpts, setId)
}

// GetDataSetStorageProvider is a free data retrieval call binding the contract method 0x21b7cd1c.
//
// Solidity: function getDataSetStorageProvider(uint256 setId) view returns(address, address)
func (_PDPVerifier *PDPVerifierCaller) GetDataSetStorageProvider(opts *bind.CallOpts, setId *big.Int) (common.Address, common.Address, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getDataSetStorageProvider", setId)

	if err != nil {
		return *new(common.Address), *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	out1 := *abi.ConvertType(out[1], new(common.Address)).(*common.Address)

	return out0, out1, err

}

// GetDataSetStorageProvider is a free data retrieval call binding the contract method 0x21b7cd1c.
//
// Solidity: function getDataSetStorageProvider(uint256 setId) view returns(address, address)
func (_PDPVerifier *PDPVerifierSession) GetDataSetStorageProvider(setId *big.Int) (common.Address, common.Address, error) {
	return _PDPVerifier.Contract.GetDataSetStorageProvider(&_PDPVerifier.CallOpts, setId)
}

// GetDataSetStorageProvider is a free data retrieval call binding the contract method 0x21b7cd1c.
//
// Solidity: function getDataSetStorageProvider(uint256 setId) view returns(address, address)
func (_PDPVerifier *PDPVerifierCallerSession) GetDataSetStorageProvider(setId *big.Int) (common.Address, common.Address, error) {
	return _PDPVerifier.Contract.GetDataSetStorageProvider(&_PDPVerifier.CallOpts, setId)
}

// GetNextChallengeEpoch is a free data retrieval call binding the contract method 0x6ba4608f.
//
// Solidity: function getNextChallengeEpoch(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) GetNextChallengeEpoch(opts *bind.CallOpts, setId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getNextChallengeEpoch", setId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetNextChallengeEpoch is a free data retrieval call binding the contract method 0x6ba4608f.
//
// Solidity: function getNextChallengeEpoch(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) GetNextChallengeEpoch(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetNextChallengeEpoch(&_PDPVerifier.CallOpts, setId)
}

// GetNextChallengeEpoch is a free data retrieval call binding the contract method 0x6ba4608f.
//
// Solidity: function getNextChallengeEpoch(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) GetNextChallengeEpoch(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetNextChallengeEpoch(&_PDPVerifier.CallOpts, setId)
}

// GetNextDataSetId is a free data retrieval call binding the contract method 0x442cded3.
//
// Solidity: function getNextDataSetId() view returns(uint64)
func (_PDPVerifier *PDPVerifierCaller) GetNextDataSetId(opts *bind.CallOpts) (uint64, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getNextDataSetId")

	if err != nil {
		return *new(uint64), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)

	return out0, err

}

// GetNextDataSetId is a free data retrieval call binding the contract method 0x442cded3.
//
// Solidity: function getNextDataSetId() view returns(uint64)
func (_PDPVerifier *PDPVerifierSession) GetNextDataSetId() (uint64, error) {
	return _PDPVerifier.Contract.GetNextDataSetId(&_PDPVerifier.CallOpts)
}

// GetNextDataSetId is a free data retrieval call binding the contract method 0x442cded3.
//
// Solidity: function getNextDataSetId() view returns(uint64)
func (_PDPVerifier *PDPVerifierCallerSession) GetNextDataSetId() (uint64, error) {
	return _PDPVerifier.Contract.GetNextDataSetId(&_PDPVerifier.CallOpts)
}

// GetNextPieceId is a free data retrieval call binding the contract method 0x1c5ae80f.
//
// Solidity: function getNextPieceId(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) GetNextPieceId(opts *bind.CallOpts, setId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getNextPieceId", setId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetNextPieceId is a free data retrieval call binding the contract method 0x1c5ae80f.
//
// Solidity: function getNextPieceId(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) GetNextPieceId(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetNextPieceId(&_PDPVerifier.CallOpts, setId)
}

// GetNextPieceId is a free data retrieval call binding the contract method 0x1c5ae80f.
//
// Solidity: function getNextPieceId(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) GetNextPieceId(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetNextPieceId(&_PDPVerifier.CallOpts, setId)
}

// GetPieceCid is a free data retrieval call binding the contract method 0x25bbbedf.
//
// Solidity: function getPieceCid(uint256 setId, uint256 pieceId) view returns((bytes))
func (_PDPVerifier *PDPVerifierCaller) GetPieceCid(opts *bind.CallOpts, setId *big.Int, pieceId *big.Int) (CidsCid, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getPieceCid", setId, pieceId)

	if err != nil {
		return *new(CidsCid), err
	}

	out0 := *abi.ConvertType(out[0], new(CidsCid)).(*CidsCid)

	return out0, err

}

// GetPieceCid is a free data retrieval call binding the contract method 0x25bbbedf.
//
// Solidity: function getPieceCid(uint256 setId, uint256 pieceId) view returns((bytes))
func (_PDPVerifier *PDPVerifierSession) GetPieceCid(setId *big.Int, pieceId *big.Int) (CidsCid, error) {
	return _PDPVerifier.Contract.GetPieceCid(&_PDPVerifier.CallOpts, setId, pieceId)
}

// GetPieceCid is a free data retrieval call binding the contract method 0x25bbbedf.
//
// Solidity: function getPieceCid(uint256 setId, uint256 pieceId) view returns((bytes))
func (_PDPVerifier *PDPVerifierCallerSession) GetPieceCid(setId *big.Int, pieceId *big.Int) (CidsCid, error) {
	return _PDPVerifier.Contract.GetPieceCid(&_PDPVerifier.CallOpts, setId, pieceId)
}

// GetPieceLeafCount is a free data retrieval call binding the contract method 0x0cd7b880.
//
// Solidity: function getPieceLeafCount(uint256 setId, uint256 pieceId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) GetPieceLeafCount(opts *bind.CallOpts, setId *big.Int, pieceId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getPieceLeafCount", setId, pieceId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetPieceLeafCount is a free data retrieval call binding the contract method 0x0cd7b880.
//
// Solidity: function getPieceLeafCount(uint256 setId, uint256 pieceId) view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) GetPieceLeafCount(setId *big.Int, pieceId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetPieceLeafCount(&_PDPVerifier.CallOpts, setId, pieceId)
}

// GetPieceLeafCount is a free data retrieval call binding the contract method 0x0cd7b880.
//
// Solidity: function getPieceLeafCount(uint256 setId, uint256 pieceId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) GetPieceLeafCount(setId *big.Int, pieceId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetPieceLeafCount(&_PDPVerifier.CallOpts, setId, pieceId)
}

// GetRandomness is a free data retrieval call binding the contract method 0x453f4f62.
//
// Solidity: function getRandomness(uint256 epoch) view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) GetRandomness(opts *bind.CallOpts, epoch *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getRandomness", epoch)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetRandomness is a free data retrieval call binding the contract method 0x453f4f62.
//
// Solidity: function getRandomness(uint256 epoch) view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) GetRandomness(epoch *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetRandomness(&_PDPVerifier.CallOpts, epoch)
}

// GetRandomness is a free data retrieval call binding the contract method 0x453f4f62.
//
// Solidity: function getRandomness(uint256 epoch) view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) GetRandomness(epoch *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetRandomness(&_PDPVerifier.CallOpts, epoch)
}

// GetScheduledRemovals is a free data retrieval call binding the contract method 0x6fa44692.
//
// Solidity: function getScheduledRemovals(uint256 setId) view returns(uint256[])
func (_PDPVerifier *PDPVerifierCaller) GetScheduledRemovals(opts *bind.CallOpts, setId *big.Int) ([]*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getScheduledRemovals", setId)

	if err != nil {
		return *new([]*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new([]*big.Int)).(*[]*big.Int)

	return out0, err

}

// GetScheduledRemovals is a free data retrieval call binding the contract method 0x6fa44692.
//
// Solidity: function getScheduledRemovals(uint256 setId) view returns(uint256[])
func (_PDPVerifier *PDPVerifierSession) GetScheduledRemovals(setId *big.Int) ([]*big.Int, error) {
	return _PDPVerifier.Contract.GetScheduledRemovals(&_PDPVerifier.CallOpts, setId)
}

// GetScheduledRemovals is a free data retrieval call binding the contract method 0x6fa44692.
//
// Solidity: function getScheduledRemovals(uint256 setId) view returns(uint256[])
func (_PDPVerifier *PDPVerifierCallerSession) GetScheduledRemovals(setId *big.Int) ([]*big.Int, error) {
	return _PDPVerifier.Contract.GetScheduledRemovals(&_PDPVerifier.CallOpts, setId)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_PDPVerifier *PDPVerifierCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_PDPVerifier *PDPVerifierSession) Owner() (common.Address, error) {
	return _PDPVerifier.Contract.Owner(&_PDPVerifier.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_PDPVerifier *PDPVerifierCallerSession) Owner() (common.Address, error) {
	return _PDPVerifier.Contract.Owner(&_PDPVerifier.CallOpts)
}

// PieceChallengable is a free data retrieval call binding the contract method 0xdc635266.
//
// Solidity: function pieceChallengable(uint256 setId, uint256 pieceId) view returns(bool)
func (_PDPVerifier *PDPVerifierCaller) PieceChallengable(opts *bind.CallOpts, setId *big.Int, pieceId *big.Int) (bool, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "pieceChallengable", setId, pieceId)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// PieceChallengable is a free data retrieval call binding the contract method 0xdc635266.
//
// Solidity: function pieceChallengable(uint256 setId, uint256 pieceId) view returns(bool)
func (_PDPVerifier *PDPVerifierSession) PieceChallengable(setId *big.Int, pieceId *big.Int) (bool, error) {
	return _PDPVerifier.Contract.PieceChallengable(&_PDPVerifier.CallOpts, setId, pieceId)
}

// PieceChallengable is a free data retrieval call binding the contract method 0xdc635266.
//
// Solidity: function pieceChallengable(uint256 setId, uint256 pieceId) view returns(bool)
func (_PDPVerifier *PDPVerifierCallerSession) PieceChallengable(setId *big.Int, pieceId *big.Int) (bool, error) {
	return _PDPVerifier.Contract.PieceChallengable(&_PDPVerifier.CallOpts, setId, pieceId)
}

// PieceLive is a free data retrieval call binding the contract method 0x1a271225.
//
// Solidity: function pieceLive(uint256 setId, uint256 pieceId) view returns(bool)
func (_PDPVerifier *PDPVerifierCaller) PieceLive(opts *bind.CallOpts, setId *big.Int, pieceId *big.Int) (bool, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "pieceLive", setId, pieceId)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// PieceLive is a free data retrieval call binding the contract method 0x1a271225.
//
// Solidity: function pieceLive(uint256 setId, uint256 pieceId) view returns(bool)
func (_PDPVerifier *PDPVerifierSession) PieceLive(setId *big.Int, pieceId *big.Int) (bool, error) {
	return _PDPVerifier.Contract.PieceLive(&_PDPVerifier.CallOpts, setId, pieceId)
}

// PieceLive is a free data retrieval call binding the contract method 0x1a271225.
//
// Solidity: function pieceLive(uint256 setId, uint256 pieceId) view returns(bool)
func (_PDPVerifier *PDPVerifierCallerSession) PieceLive(setId *big.Int, pieceId *big.Int) (bool, error) {
	return _PDPVerifier.Contract.PieceLive(&_PDPVerifier.CallOpts, setId, pieceId)
}

// ProposedFeePerTiB is a free data retrieval call binding the contract method 0xba74d94c.
//
// Solidity: function proposedFeePerTiB() view returns(uint96)
func (_PDPVerifier *PDPVerifierCaller) ProposedFeePerTiB(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "proposedFeePerTiB")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ProposedFeePerTiB is a free data retrieval call binding the contract method 0xba74d94c.
//
// Solidity: function proposedFeePerTiB() view returns(uint96)
func (_PDPVerifier *PDPVerifierSession) ProposedFeePerTiB() (*big.Int, error) {
	return _PDPVerifier.Contract.ProposedFeePerTiB(&_PDPVerifier.CallOpts)
}

// ProposedFeePerTiB is a free data retrieval call binding the contract method 0xba74d94c.
//
// Solidity: function proposedFeePerTiB() view returns(uint96)
func (_PDPVerifier *PDPVerifierCallerSession) ProposedFeePerTiB() (*big.Int, error) {
	return _PDPVerifier.Contract.ProposedFeePerTiB(&_PDPVerifier.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_PDPVerifier *PDPVerifierCaller) ProxiableUUID(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "proxiableUUID")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_PDPVerifier *PDPVerifierSession) ProxiableUUID() ([32]byte, error) {
	return _PDPVerifier.Contract.ProxiableUUID(&_PDPVerifier.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_PDPVerifier *PDPVerifierCallerSession) ProxiableUUID() ([32]byte, error) {
	return _PDPVerifier.Contract.ProxiableUUID(&_PDPVerifier.CallOpts)
}

// AddPieces is a paid mutator transaction binding the contract method 0x9afd37f2.
//
// Solidity: function addPieces(uint256 setId, address listenerAddr, (bytes)[] pieceData, bytes extraData) payable returns(uint256)
func (_PDPVerifier *PDPVerifierTransactor) AddPieces(opts *bind.TransactOpts, setId *big.Int, listenerAddr common.Address, pieceData []CidsCid, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "addPieces", setId, listenerAddr, pieceData, extraData)
}

// AddPieces is a paid mutator transaction binding the contract method 0x9afd37f2.
//
// Solidity: function addPieces(uint256 setId, address listenerAddr, (bytes)[] pieceData, bytes extraData) payable returns(uint256)
func (_PDPVerifier *PDPVerifierSession) AddPieces(setId *big.Int, listenerAddr common.Address, pieceData []CidsCid, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.AddPieces(&_PDPVerifier.TransactOpts, setId, listenerAddr, pieceData, extraData)
}

// AddPieces is a paid mutator transaction binding the contract method 0x9afd37f2.
//
// Solidity: function addPieces(uint256 setId, address listenerAddr, (bytes)[] pieceData, bytes extraData) payable returns(uint256)
func (_PDPVerifier *PDPVerifierTransactorSession) AddPieces(setId *big.Int, listenerAddr common.Address, pieceData []CidsCid, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.AddPieces(&_PDPVerifier.TransactOpts, setId, listenerAddr, pieceData, extraData)
}

// ClaimDataSetStorageProvider is a paid mutator transaction binding the contract method 0xdf0f3248.
//
// Solidity: function claimDataSetStorageProvider(uint256 setId, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierTransactor) ClaimDataSetStorageProvider(opts *bind.TransactOpts, setId *big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "claimDataSetStorageProvider", setId, extraData)
}

// ClaimDataSetStorageProvider is a paid mutator transaction binding the contract method 0xdf0f3248.
//
// Solidity: function claimDataSetStorageProvider(uint256 setId, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierSession) ClaimDataSetStorageProvider(setId *big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.ClaimDataSetStorageProvider(&_PDPVerifier.TransactOpts, setId, extraData)
}

// ClaimDataSetStorageProvider is a paid mutator transaction binding the contract method 0xdf0f3248.
//
// Solidity: function claimDataSetStorageProvider(uint256 setId, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierTransactorSession) ClaimDataSetStorageProvider(setId *big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.ClaimDataSetStorageProvider(&_PDPVerifier.TransactOpts, setId, extraData)
}

// CreateDataSet is a paid mutator transaction binding the contract method 0xbbae41cb.
//
// Solidity: function createDataSet(address listenerAddr, bytes extraData) payable returns(uint256)
func (_PDPVerifier *PDPVerifierTransactor) CreateDataSet(opts *bind.TransactOpts, listenerAddr common.Address, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "createDataSet", listenerAddr, extraData)
}

// CreateDataSet is a paid mutator transaction binding the contract method 0xbbae41cb.
//
// Solidity: function createDataSet(address listenerAddr, bytes extraData) payable returns(uint256)
func (_PDPVerifier *PDPVerifierSession) CreateDataSet(listenerAddr common.Address, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.CreateDataSet(&_PDPVerifier.TransactOpts, listenerAddr, extraData)
}

// CreateDataSet is a paid mutator transaction binding the contract method 0xbbae41cb.
//
// Solidity: function createDataSet(address listenerAddr, bytes extraData) payable returns(uint256)
func (_PDPVerifier *PDPVerifierTransactorSession) CreateDataSet(listenerAddr common.Address, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.CreateDataSet(&_PDPVerifier.TransactOpts, listenerAddr, extraData)
}

// DeleteDataSet is a paid mutator transaction binding the contract method 0x7a1e2990.
//
// Solidity: function deleteDataSet(uint256 setId, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierTransactor) DeleteDataSet(opts *bind.TransactOpts, setId *big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "deleteDataSet", setId, extraData)
}

// DeleteDataSet is a paid mutator transaction binding the contract method 0x7a1e2990.
//
// Solidity: function deleteDataSet(uint256 setId, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierSession) DeleteDataSet(setId *big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.DeleteDataSet(&_PDPVerifier.TransactOpts, setId, extraData)
}

// DeleteDataSet is a paid mutator transaction binding the contract method 0x7a1e2990.
//
// Solidity: function deleteDataSet(uint256 setId, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierTransactorSession) DeleteDataSet(setId *big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.DeleteDataSet(&_PDPVerifier.TransactOpts, setId, extraData)
}

// Initialize is a paid mutator transaction binding the contract method 0xfe4b84df.
//
// Solidity: function initialize(uint256 _challengeFinality) returns()
func (_PDPVerifier *PDPVerifierTransactor) Initialize(opts *bind.TransactOpts, _challengeFinality *big.Int) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "initialize", _challengeFinality)
}

// Initialize is a paid mutator transaction binding the contract method 0xfe4b84df.
//
// Solidity: function initialize(uint256 _challengeFinality) returns()
func (_PDPVerifier *PDPVerifierSession) Initialize(_challengeFinality *big.Int) (*types.Transaction, error) {
	return _PDPVerifier.Contract.Initialize(&_PDPVerifier.TransactOpts, _challengeFinality)
}

// Initialize is a paid mutator transaction binding the contract method 0xfe4b84df.
//
// Solidity: function initialize(uint256 _challengeFinality) returns()
func (_PDPVerifier *PDPVerifierTransactorSession) Initialize(_challengeFinality *big.Int) (*types.Transaction, error) {
	return _PDPVerifier.Contract.Initialize(&_PDPVerifier.TransactOpts, _challengeFinality)
}

// Migrate is a paid mutator transaction binding the contract method 0x8fd3ab80.
//
// Solidity: function migrate() returns()
func (_PDPVerifier *PDPVerifierTransactor) Migrate(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "migrate")
}

// Migrate is a paid mutator transaction binding the contract method 0x8fd3ab80.
//
// Solidity: function migrate() returns()
func (_PDPVerifier *PDPVerifierSession) Migrate() (*types.Transaction, error) {
	return _PDPVerifier.Contract.Migrate(&_PDPVerifier.TransactOpts)
}

// Migrate is a paid mutator transaction binding the contract method 0x8fd3ab80.
//
// Solidity: function migrate() returns()
func (_PDPVerifier *PDPVerifierTransactorSession) Migrate() (*types.Transaction, error) {
	return _PDPVerifier.Contract.Migrate(&_PDPVerifier.TransactOpts)
}

// NextProvingPeriod is a paid mutator transaction binding the contract method 0x45c0b92d.
//
// Solidity: function nextProvingPeriod(uint256 setId, uint256 challengeEpoch, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierTransactor) NextProvingPeriod(opts *bind.TransactOpts, setId *big.Int, challengeEpoch *big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "nextProvingPeriod", setId, challengeEpoch, extraData)
}

// NextProvingPeriod is a paid mutator transaction binding the contract method 0x45c0b92d.
//
// Solidity: function nextProvingPeriod(uint256 setId, uint256 challengeEpoch, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierSession) NextProvingPeriod(setId *big.Int, challengeEpoch *big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.NextProvingPeriod(&_PDPVerifier.TransactOpts, setId, challengeEpoch, extraData)
}

// NextProvingPeriod is a paid mutator transaction binding the contract method 0x45c0b92d.
//
// Solidity: function nextProvingPeriod(uint256 setId, uint256 challengeEpoch, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierTransactorSession) NextProvingPeriod(setId *big.Int, challengeEpoch *big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.NextProvingPeriod(&_PDPVerifier.TransactOpts, setId, challengeEpoch, extraData)
}

// ProposeDataSetStorageProvider is a paid mutator transaction binding the contract method 0x43186080.
//
// Solidity: function proposeDataSetStorageProvider(uint256 setId, address newStorageProvider) returns()
func (_PDPVerifier *PDPVerifierTransactor) ProposeDataSetStorageProvider(opts *bind.TransactOpts, setId *big.Int, newStorageProvider common.Address) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "proposeDataSetStorageProvider", setId, newStorageProvider)
}

// ProposeDataSetStorageProvider is a paid mutator transaction binding the contract method 0x43186080.
//
// Solidity: function proposeDataSetStorageProvider(uint256 setId, address newStorageProvider) returns()
func (_PDPVerifier *PDPVerifierSession) ProposeDataSetStorageProvider(setId *big.Int, newStorageProvider common.Address) (*types.Transaction, error) {
	return _PDPVerifier.Contract.ProposeDataSetStorageProvider(&_PDPVerifier.TransactOpts, setId, newStorageProvider)
}

// ProposeDataSetStorageProvider is a paid mutator transaction binding the contract method 0x43186080.
//
// Solidity: function proposeDataSetStorageProvider(uint256 setId, address newStorageProvider) returns()
func (_PDPVerifier *PDPVerifierTransactorSession) ProposeDataSetStorageProvider(setId *big.Int, newStorageProvider common.Address) (*types.Transaction, error) {
	return _PDPVerifier.Contract.ProposeDataSetStorageProvider(&_PDPVerifier.TransactOpts, setId, newStorageProvider)
}

// ProvePossession is a paid mutator transaction binding the contract method 0xf58f952b.
//
// Solidity: function provePossession(uint256 setId, (bytes32,bytes32[])[] proofs) payable returns()
func (_PDPVerifier *PDPVerifierTransactor) ProvePossession(opts *bind.TransactOpts, setId *big.Int, proofs []IPDPTypesProof) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "provePossession", setId, proofs)
}

// ProvePossession is a paid mutator transaction binding the contract method 0xf58f952b.
//
// Solidity: function provePossession(uint256 setId, (bytes32,bytes32[])[] proofs) payable returns()
func (_PDPVerifier *PDPVerifierSession) ProvePossession(setId *big.Int, proofs []IPDPTypesProof) (*types.Transaction, error) {
	return _PDPVerifier.Contract.ProvePossession(&_PDPVerifier.TransactOpts, setId, proofs)
}

// ProvePossession is a paid mutator transaction binding the contract method 0xf58f952b.
//
// Solidity: function provePossession(uint256 setId, (bytes32,bytes32[])[] proofs) payable returns()
func (_PDPVerifier *PDPVerifierTransactorSession) ProvePossession(setId *big.Int, proofs []IPDPTypesProof) (*types.Transaction, error) {
	return _PDPVerifier.Contract.ProvePossession(&_PDPVerifier.TransactOpts, setId, proofs)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_PDPVerifier *PDPVerifierTransactor) RenounceOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "renounceOwnership")
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_PDPVerifier *PDPVerifierSession) RenounceOwnership() (*types.Transaction, error) {
	return _PDPVerifier.Contract.RenounceOwnership(&_PDPVerifier.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_PDPVerifier *PDPVerifierTransactorSession) RenounceOwnership() (*types.Transaction, error) {
	return _PDPVerifier.Contract.RenounceOwnership(&_PDPVerifier.TransactOpts)
}

// SchedulePieceDeletions is a paid mutator transaction binding the contract method 0x0c292024.
//
// Solidity: function schedulePieceDeletions(uint256 setId, uint256[] pieceIds, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierTransactor) SchedulePieceDeletions(opts *bind.TransactOpts, setId *big.Int, pieceIds []*big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "schedulePieceDeletions", setId, pieceIds, extraData)
}

// SchedulePieceDeletions is a paid mutator transaction binding the contract method 0x0c292024.
//
// Solidity: function schedulePieceDeletions(uint256 setId, uint256[] pieceIds, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierSession) SchedulePieceDeletions(setId *big.Int, pieceIds []*big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.SchedulePieceDeletions(&_PDPVerifier.TransactOpts, setId, pieceIds, extraData)
}

// SchedulePieceDeletions is a paid mutator transaction binding the contract method 0x0c292024.
//
// Solidity: function schedulePieceDeletions(uint256 setId, uint256[] pieceIds, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierTransactorSession) SchedulePieceDeletions(setId *big.Int, pieceIds []*big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.SchedulePieceDeletions(&_PDPVerifier.TransactOpts, setId, pieceIds, extraData)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_PDPVerifier *PDPVerifierTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_PDPVerifier *PDPVerifierSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _PDPVerifier.Contract.TransferOwnership(&_PDPVerifier.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_PDPVerifier *PDPVerifierTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _PDPVerifier.Contract.TransferOwnership(&_PDPVerifier.TransactOpts, newOwner)
}

// UpdateProofFee is a paid mutator transaction binding the contract method 0x46bf7ed3.
//
// Solidity: function updateProofFee(uint256 newFeePerTiB) returns()
func (_PDPVerifier *PDPVerifierTransactor) UpdateProofFee(opts *bind.TransactOpts, newFeePerTiB *big.Int) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "updateProofFee", newFeePerTiB)
}

// UpdateProofFee is a paid mutator transaction binding the contract method 0x46bf7ed3.
//
// Solidity: function updateProofFee(uint256 newFeePerTiB) returns()
func (_PDPVerifier *PDPVerifierSession) UpdateProofFee(newFeePerTiB *big.Int) (*types.Transaction, error) {
	return _PDPVerifier.Contract.UpdateProofFee(&_PDPVerifier.TransactOpts, newFeePerTiB)
}

// UpdateProofFee is a paid mutator transaction binding the contract method 0x46bf7ed3.
//
// Solidity: function updateProofFee(uint256 newFeePerTiB) returns()
func (_PDPVerifier *PDPVerifierTransactorSession) UpdateProofFee(newFeePerTiB *big.Int) (*types.Transaction, error) {
	return _PDPVerifier.Contract.UpdateProofFee(&_PDPVerifier.TransactOpts, newFeePerTiB)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_PDPVerifier *PDPVerifierTransactor) UpgradeToAndCall(opts *bind.TransactOpts, newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "upgradeToAndCall", newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_PDPVerifier *PDPVerifierSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.UpgradeToAndCall(&_PDPVerifier.TransactOpts, newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_PDPVerifier *PDPVerifierTransactorSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.UpgradeToAndCall(&_PDPVerifier.TransactOpts, newImplementation, data)
}

// PDPVerifierContractUpgradedIterator is returned from FilterContractUpgraded and is used to iterate over the raw logs and unpacked data for ContractUpgraded events raised by the PDPVerifier contract.
type PDPVerifierContractUpgradedIterator struct {
	Event *PDPVerifierContractUpgraded // Event containing the contract specifics and raw log

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
func (it *PDPVerifierContractUpgradedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierContractUpgraded)
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
		it.Event = new(PDPVerifierContractUpgraded)
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
func (it *PDPVerifierContractUpgradedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierContractUpgradedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierContractUpgraded represents a ContractUpgraded event raised by the PDPVerifier contract.
type PDPVerifierContractUpgraded struct {
	Version        string
	Implementation common.Address
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterContractUpgraded is a free log retrieval operation binding the contract event 0x2b51ff7c4cc8e6fe1c72e9d9685b7d2a88a5d82ad3a644afbdceb0272c89c1c3.
//
// Solidity: event ContractUpgraded(string version, address implementation)
func (_PDPVerifier *PDPVerifierFilterer) FilterContractUpgraded(opts *bind.FilterOpts) (*PDPVerifierContractUpgradedIterator, error) {

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "ContractUpgraded")
	if err != nil {
		return nil, err
	}
	return &PDPVerifierContractUpgradedIterator{contract: _PDPVerifier.contract, event: "ContractUpgraded", logs: logs, sub: sub}, nil
}

// WatchContractUpgraded is a free log subscription operation binding the contract event 0x2b51ff7c4cc8e6fe1c72e9d9685b7d2a88a5d82ad3a644afbdceb0272c89c1c3.
//
// Solidity: event ContractUpgraded(string version, address implementation)
func (_PDPVerifier *PDPVerifierFilterer) WatchContractUpgraded(opts *bind.WatchOpts, sink chan<- *PDPVerifierContractUpgraded) (event.Subscription, error) {

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "ContractUpgraded")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierContractUpgraded)
				if err := _PDPVerifier.contract.UnpackLog(event, "ContractUpgraded", log); err != nil {
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
func (_PDPVerifier *PDPVerifierFilterer) ParseContractUpgraded(log types.Log) (*PDPVerifierContractUpgraded, error) {
	event := new(PDPVerifierContractUpgraded)
	if err := _PDPVerifier.contract.UnpackLog(event, "ContractUpgraded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierDataSetCreatedIterator is returned from FilterDataSetCreated and is used to iterate over the raw logs and unpacked data for DataSetCreated events raised by the PDPVerifier contract.
type PDPVerifierDataSetCreatedIterator struct {
	Event *PDPVerifierDataSetCreated // Event containing the contract specifics and raw log

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
func (it *PDPVerifierDataSetCreatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierDataSetCreated)
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
		it.Event = new(PDPVerifierDataSetCreated)
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
func (it *PDPVerifierDataSetCreatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierDataSetCreatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierDataSetCreated represents a DataSetCreated event raised by the PDPVerifier contract.
type PDPVerifierDataSetCreated struct {
	SetId           *big.Int
	StorageProvider common.Address
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterDataSetCreated is a free log retrieval operation binding the contract event 0x11369440e1b7135015c16acb9bc14b55b0f4b23b02010c363d34aec2e5b96281.
//
// Solidity: event DataSetCreated(uint256 indexed setId, address indexed storageProvider)
func (_PDPVerifier *PDPVerifierFilterer) FilterDataSetCreated(opts *bind.FilterOpts, setId []*big.Int, storageProvider []common.Address) (*PDPVerifierDataSetCreatedIterator, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}
	var storageProviderRule []interface{}
	for _, storageProviderItem := range storageProvider {
		storageProviderRule = append(storageProviderRule, storageProviderItem)
	}

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "DataSetCreated", setIdRule, storageProviderRule)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierDataSetCreatedIterator{contract: _PDPVerifier.contract, event: "DataSetCreated", logs: logs, sub: sub}, nil
}

// WatchDataSetCreated is a free log subscription operation binding the contract event 0x11369440e1b7135015c16acb9bc14b55b0f4b23b02010c363d34aec2e5b96281.
//
// Solidity: event DataSetCreated(uint256 indexed setId, address indexed storageProvider)
func (_PDPVerifier *PDPVerifierFilterer) WatchDataSetCreated(opts *bind.WatchOpts, sink chan<- *PDPVerifierDataSetCreated, setId []*big.Int, storageProvider []common.Address) (event.Subscription, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}
	var storageProviderRule []interface{}
	for _, storageProviderItem := range storageProvider {
		storageProviderRule = append(storageProviderRule, storageProviderItem)
	}

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "DataSetCreated", setIdRule, storageProviderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierDataSetCreated)
				if err := _PDPVerifier.contract.UnpackLog(event, "DataSetCreated", log); err != nil {
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

// ParseDataSetCreated is a log parse operation binding the contract event 0x11369440e1b7135015c16acb9bc14b55b0f4b23b02010c363d34aec2e5b96281.
//
// Solidity: event DataSetCreated(uint256 indexed setId, address indexed storageProvider)
func (_PDPVerifier *PDPVerifierFilterer) ParseDataSetCreated(log types.Log) (*PDPVerifierDataSetCreated, error) {
	event := new(PDPVerifierDataSetCreated)
	if err := _PDPVerifier.contract.UnpackLog(event, "DataSetCreated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierDataSetDeletedIterator is returned from FilterDataSetDeleted and is used to iterate over the raw logs and unpacked data for DataSetDeleted events raised by the PDPVerifier contract.
type PDPVerifierDataSetDeletedIterator struct {
	Event *PDPVerifierDataSetDeleted // Event containing the contract specifics and raw log

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
func (it *PDPVerifierDataSetDeletedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierDataSetDeleted)
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
		it.Event = new(PDPVerifierDataSetDeleted)
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
func (it *PDPVerifierDataSetDeletedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierDataSetDeletedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierDataSetDeleted represents a DataSetDeleted event raised by the PDPVerifier contract.
type PDPVerifierDataSetDeleted struct {
	SetId            *big.Int
	DeletedLeafCount *big.Int
	Raw              types.Log // Blockchain specific contextual infos
}

// FilterDataSetDeleted is a free log retrieval operation binding the contract event 0x14eeeef7679fcb051c6572811f61c07bedccd0f1cfc1f9b79b23e47c5c52aeb7.
//
// Solidity: event DataSetDeleted(uint256 indexed setId, uint256 deletedLeafCount)
func (_PDPVerifier *PDPVerifierFilterer) FilterDataSetDeleted(opts *bind.FilterOpts, setId []*big.Int) (*PDPVerifierDataSetDeletedIterator, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "DataSetDeleted", setIdRule)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierDataSetDeletedIterator{contract: _PDPVerifier.contract, event: "DataSetDeleted", logs: logs, sub: sub}, nil
}

// WatchDataSetDeleted is a free log subscription operation binding the contract event 0x14eeeef7679fcb051c6572811f61c07bedccd0f1cfc1f9b79b23e47c5c52aeb7.
//
// Solidity: event DataSetDeleted(uint256 indexed setId, uint256 deletedLeafCount)
func (_PDPVerifier *PDPVerifierFilterer) WatchDataSetDeleted(opts *bind.WatchOpts, sink chan<- *PDPVerifierDataSetDeleted, setId []*big.Int) (event.Subscription, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "DataSetDeleted", setIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierDataSetDeleted)
				if err := _PDPVerifier.contract.UnpackLog(event, "DataSetDeleted", log); err != nil {
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

// ParseDataSetDeleted is a log parse operation binding the contract event 0x14eeeef7679fcb051c6572811f61c07bedccd0f1cfc1f9b79b23e47c5c52aeb7.
//
// Solidity: event DataSetDeleted(uint256 indexed setId, uint256 deletedLeafCount)
func (_PDPVerifier *PDPVerifierFilterer) ParseDataSetDeleted(log types.Log) (*PDPVerifierDataSetDeleted, error) {
	event := new(PDPVerifierDataSetDeleted)
	if err := _PDPVerifier.contract.UnpackLog(event, "DataSetDeleted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierDataSetEmptyIterator is returned from FilterDataSetEmpty and is used to iterate over the raw logs and unpacked data for DataSetEmpty events raised by the PDPVerifier contract.
type PDPVerifierDataSetEmptyIterator struct {
	Event *PDPVerifierDataSetEmpty // Event containing the contract specifics and raw log

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
func (it *PDPVerifierDataSetEmptyIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierDataSetEmpty)
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
		it.Event = new(PDPVerifierDataSetEmpty)
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
func (it *PDPVerifierDataSetEmptyIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierDataSetEmptyIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierDataSetEmpty represents a DataSetEmpty event raised by the PDPVerifier contract.
type PDPVerifierDataSetEmpty struct {
	SetId *big.Int
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterDataSetEmpty is a free log retrieval operation binding the contract event 0x02a8400fc343f45098cb00c3a6ea694174771939a5503f663e0ff6f4eb7c2842.
//
// Solidity: event DataSetEmpty(uint256 indexed setId)
func (_PDPVerifier *PDPVerifierFilterer) FilterDataSetEmpty(opts *bind.FilterOpts, setId []*big.Int) (*PDPVerifierDataSetEmptyIterator, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "DataSetEmpty", setIdRule)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierDataSetEmptyIterator{contract: _PDPVerifier.contract, event: "DataSetEmpty", logs: logs, sub: sub}, nil
}

// WatchDataSetEmpty is a free log subscription operation binding the contract event 0x02a8400fc343f45098cb00c3a6ea694174771939a5503f663e0ff6f4eb7c2842.
//
// Solidity: event DataSetEmpty(uint256 indexed setId)
func (_PDPVerifier *PDPVerifierFilterer) WatchDataSetEmpty(opts *bind.WatchOpts, sink chan<- *PDPVerifierDataSetEmpty, setId []*big.Int) (event.Subscription, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "DataSetEmpty", setIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierDataSetEmpty)
				if err := _PDPVerifier.contract.UnpackLog(event, "DataSetEmpty", log); err != nil {
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

// ParseDataSetEmpty is a log parse operation binding the contract event 0x02a8400fc343f45098cb00c3a6ea694174771939a5503f663e0ff6f4eb7c2842.
//
// Solidity: event DataSetEmpty(uint256 indexed setId)
func (_PDPVerifier *PDPVerifierFilterer) ParseDataSetEmpty(log types.Log) (*PDPVerifierDataSetEmpty, error) {
	event := new(PDPVerifierDataSetEmpty)
	if err := _PDPVerifier.contract.UnpackLog(event, "DataSetEmpty", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierFeeUpdateProposedIterator is returned from FilterFeeUpdateProposed and is used to iterate over the raw logs and unpacked data for FeeUpdateProposed events raised by the PDPVerifier contract.
type PDPVerifierFeeUpdateProposedIterator struct {
	Event *PDPVerifierFeeUpdateProposed // Event containing the contract specifics and raw log

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
func (it *PDPVerifierFeeUpdateProposedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierFeeUpdateProposed)
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
		it.Event = new(PDPVerifierFeeUpdateProposed)
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
func (it *PDPVerifierFeeUpdateProposedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierFeeUpdateProposedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierFeeUpdateProposed represents a FeeUpdateProposed event raised by the PDPVerifier contract.
type PDPVerifierFeeUpdateProposed struct {
	CurrentFee    *big.Int
	NewFee        *big.Int
	EffectiveTime *big.Int
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterFeeUpdateProposed is a free log retrieval operation binding the contract event 0x239c396012e4038117d18910fba2aab3452e37696f685a457098e4c4864d8bcb.
//
// Solidity: event FeeUpdateProposed(uint256 currentFee, uint256 newFee, uint256 effectiveTime)
func (_PDPVerifier *PDPVerifierFilterer) FilterFeeUpdateProposed(opts *bind.FilterOpts) (*PDPVerifierFeeUpdateProposedIterator, error) {

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "FeeUpdateProposed")
	if err != nil {
		return nil, err
	}
	return &PDPVerifierFeeUpdateProposedIterator{contract: _PDPVerifier.contract, event: "FeeUpdateProposed", logs: logs, sub: sub}, nil
}

// WatchFeeUpdateProposed is a free log subscription operation binding the contract event 0x239c396012e4038117d18910fba2aab3452e37696f685a457098e4c4864d8bcb.
//
// Solidity: event FeeUpdateProposed(uint256 currentFee, uint256 newFee, uint256 effectiveTime)
func (_PDPVerifier *PDPVerifierFilterer) WatchFeeUpdateProposed(opts *bind.WatchOpts, sink chan<- *PDPVerifierFeeUpdateProposed) (event.Subscription, error) {

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "FeeUpdateProposed")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierFeeUpdateProposed)
				if err := _PDPVerifier.contract.UnpackLog(event, "FeeUpdateProposed", log); err != nil {
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

// ParseFeeUpdateProposed is a log parse operation binding the contract event 0x239c396012e4038117d18910fba2aab3452e37696f685a457098e4c4864d8bcb.
//
// Solidity: event FeeUpdateProposed(uint256 currentFee, uint256 newFee, uint256 effectiveTime)
func (_PDPVerifier *PDPVerifierFilterer) ParseFeeUpdateProposed(log types.Log) (*PDPVerifierFeeUpdateProposed, error) {
	event := new(PDPVerifierFeeUpdateProposed)
	if err := _PDPVerifier.contract.UnpackLog(event, "FeeUpdateProposed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierInitializedIterator is returned from FilterInitialized and is used to iterate over the raw logs and unpacked data for Initialized events raised by the PDPVerifier contract.
type PDPVerifierInitializedIterator struct {
	Event *PDPVerifierInitialized // Event containing the contract specifics and raw log

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
func (it *PDPVerifierInitializedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierInitialized)
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
		it.Event = new(PDPVerifierInitialized)
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
func (it *PDPVerifierInitializedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierInitializedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierInitialized represents a Initialized event raised by the PDPVerifier contract.
type PDPVerifierInitialized struct {
	Version uint64
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterInitialized is a free log retrieval operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_PDPVerifier *PDPVerifierFilterer) FilterInitialized(opts *bind.FilterOpts) (*PDPVerifierInitializedIterator, error) {

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return &PDPVerifierInitializedIterator{contract: _PDPVerifier.contract, event: "Initialized", logs: logs, sub: sub}, nil
}

// WatchInitialized is a free log subscription operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_PDPVerifier *PDPVerifierFilterer) WatchInitialized(opts *bind.WatchOpts, sink chan<- *PDPVerifierInitialized) (event.Subscription, error) {

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierInitialized)
				if err := _PDPVerifier.contract.UnpackLog(event, "Initialized", log); err != nil {
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
func (_PDPVerifier *PDPVerifierFilterer) ParseInitialized(log types.Log) (*PDPVerifierInitialized, error) {
	event := new(PDPVerifierInitialized)
	if err := _PDPVerifier.contract.UnpackLog(event, "Initialized", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierNextProvingPeriodIterator is returned from FilterNextProvingPeriod and is used to iterate over the raw logs and unpacked data for NextProvingPeriod events raised by the PDPVerifier contract.
type PDPVerifierNextProvingPeriodIterator struct {
	Event *PDPVerifierNextProvingPeriod // Event containing the contract specifics and raw log

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
func (it *PDPVerifierNextProvingPeriodIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierNextProvingPeriod)
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
		it.Event = new(PDPVerifierNextProvingPeriod)
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
func (it *PDPVerifierNextProvingPeriodIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierNextProvingPeriodIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierNextProvingPeriod represents a NextProvingPeriod event raised by the PDPVerifier contract.
type PDPVerifierNextProvingPeriod struct {
	SetId          *big.Int
	ChallengeEpoch *big.Int
	LeafCount      *big.Int
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterNextProvingPeriod is a free log retrieval operation binding the contract event 0xc099ffec4e3e773644a4d1dda368c46af853a0eeb15babde217f53a657396e1e.
//
// Solidity: event NextProvingPeriod(uint256 indexed setId, uint256 challengeEpoch, uint256 leafCount)
func (_PDPVerifier *PDPVerifierFilterer) FilterNextProvingPeriod(opts *bind.FilterOpts, setId []*big.Int) (*PDPVerifierNextProvingPeriodIterator, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "NextProvingPeriod", setIdRule)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierNextProvingPeriodIterator{contract: _PDPVerifier.contract, event: "NextProvingPeriod", logs: logs, sub: sub}, nil
}

// WatchNextProvingPeriod is a free log subscription operation binding the contract event 0xc099ffec4e3e773644a4d1dda368c46af853a0eeb15babde217f53a657396e1e.
//
// Solidity: event NextProvingPeriod(uint256 indexed setId, uint256 challengeEpoch, uint256 leafCount)
func (_PDPVerifier *PDPVerifierFilterer) WatchNextProvingPeriod(opts *bind.WatchOpts, sink chan<- *PDPVerifierNextProvingPeriod, setId []*big.Int) (event.Subscription, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "NextProvingPeriod", setIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierNextProvingPeriod)
				if err := _PDPVerifier.contract.UnpackLog(event, "NextProvingPeriod", log); err != nil {
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

// ParseNextProvingPeriod is a log parse operation binding the contract event 0xc099ffec4e3e773644a4d1dda368c46af853a0eeb15babde217f53a657396e1e.
//
// Solidity: event NextProvingPeriod(uint256 indexed setId, uint256 challengeEpoch, uint256 leafCount)
func (_PDPVerifier *PDPVerifierFilterer) ParseNextProvingPeriod(log types.Log) (*PDPVerifierNextProvingPeriod, error) {
	event := new(PDPVerifierNextProvingPeriod)
	if err := _PDPVerifier.contract.UnpackLog(event, "NextProvingPeriod", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the PDPVerifier contract.
type PDPVerifierOwnershipTransferredIterator struct {
	Event *PDPVerifierOwnershipTransferred // Event containing the contract specifics and raw log

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
func (it *PDPVerifierOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierOwnershipTransferred)
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
		it.Event = new(PDPVerifierOwnershipTransferred)
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
func (it *PDPVerifierOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierOwnershipTransferred represents a OwnershipTransferred event raised by the PDPVerifier contract.
type PDPVerifierOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_PDPVerifier *PDPVerifierFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*PDPVerifierOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierOwnershipTransferredIterator{contract: _PDPVerifier.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_PDPVerifier *PDPVerifierFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *PDPVerifierOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierOwnershipTransferred)
				if err := _PDPVerifier.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
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
func (_PDPVerifier *PDPVerifierFilterer) ParseOwnershipTransferred(log types.Log) (*PDPVerifierOwnershipTransferred, error) {
	event := new(PDPVerifierOwnershipTransferred)
	if err := _PDPVerifier.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierPiecesAddedIterator is returned from FilterPiecesAdded and is used to iterate over the raw logs and unpacked data for PiecesAdded events raised by the PDPVerifier contract.
type PDPVerifierPiecesAddedIterator struct {
	Event *PDPVerifierPiecesAdded // Event containing the contract specifics and raw log

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
func (it *PDPVerifierPiecesAddedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierPiecesAdded)
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
		it.Event = new(PDPVerifierPiecesAdded)
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
func (it *PDPVerifierPiecesAddedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierPiecesAddedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierPiecesAdded represents a PiecesAdded event raised by the PDPVerifier contract.
type PDPVerifierPiecesAdded struct {
	SetId     *big.Int
	PieceIds  []*big.Int
	PieceCids []CidsCid
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterPiecesAdded is a free log retrieval operation binding the contract event 0x396df50222a87662e94bb7d173792d5e61fe0b193b6ccf791f7ce433f0b28207.
//
// Solidity: event PiecesAdded(uint256 indexed setId, uint256[] pieceIds, (bytes)[] pieceCids)
func (_PDPVerifier *PDPVerifierFilterer) FilterPiecesAdded(opts *bind.FilterOpts, setId []*big.Int) (*PDPVerifierPiecesAddedIterator, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "PiecesAdded", setIdRule)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierPiecesAddedIterator{contract: _PDPVerifier.contract, event: "PiecesAdded", logs: logs, sub: sub}, nil
}

// WatchPiecesAdded is a free log subscription operation binding the contract event 0x396df50222a87662e94bb7d173792d5e61fe0b193b6ccf791f7ce433f0b28207.
//
// Solidity: event PiecesAdded(uint256 indexed setId, uint256[] pieceIds, (bytes)[] pieceCids)
func (_PDPVerifier *PDPVerifierFilterer) WatchPiecesAdded(opts *bind.WatchOpts, sink chan<- *PDPVerifierPiecesAdded, setId []*big.Int) (event.Subscription, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "PiecesAdded", setIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierPiecesAdded)
				if err := _PDPVerifier.contract.UnpackLog(event, "PiecesAdded", log); err != nil {
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

// ParsePiecesAdded is a log parse operation binding the contract event 0x396df50222a87662e94bb7d173792d5e61fe0b193b6ccf791f7ce433f0b28207.
//
// Solidity: event PiecesAdded(uint256 indexed setId, uint256[] pieceIds, (bytes)[] pieceCids)
func (_PDPVerifier *PDPVerifierFilterer) ParsePiecesAdded(log types.Log) (*PDPVerifierPiecesAdded, error) {
	event := new(PDPVerifierPiecesAdded)
	if err := _PDPVerifier.contract.UnpackLog(event, "PiecesAdded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierPiecesRemovedIterator is returned from FilterPiecesRemoved and is used to iterate over the raw logs and unpacked data for PiecesRemoved events raised by the PDPVerifier contract.
type PDPVerifierPiecesRemovedIterator struct {
	Event *PDPVerifierPiecesRemoved // Event containing the contract specifics and raw log

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
func (it *PDPVerifierPiecesRemovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierPiecesRemoved)
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
		it.Event = new(PDPVerifierPiecesRemoved)
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
func (it *PDPVerifierPiecesRemovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierPiecesRemovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierPiecesRemoved represents a PiecesRemoved event raised by the PDPVerifier contract.
type PDPVerifierPiecesRemoved struct {
	SetId    *big.Int
	PieceIds []*big.Int
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterPiecesRemoved is a free log retrieval operation binding the contract event 0x6e87df804629ac17804b57ba7abbdfac8bdc36bab504fb8a8801eb313a8ce7b1.
//
// Solidity: event PiecesRemoved(uint256 indexed setId, uint256[] pieceIds)
func (_PDPVerifier *PDPVerifierFilterer) FilterPiecesRemoved(opts *bind.FilterOpts, setId []*big.Int) (*PDPVerifierPiecesRemovedIterator, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "PiecesRemoved", setIdRule)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierPiecesRemovedIterator{contract: _PDPVerifier.contract, event: "PiecesRemoved", logs: logs, sub: sub}, nil
}

// WatchPiecesRemoved is a free log subscription operation binding the contract event 0x6e87df804629ac17804b57ba7abbdfac8bdc36bab504fb8a8801eb313a8ce7b1.
//
// Solidity: event PiecesRemoved(uint256 indexed setId, uint256[] pieceIds)
func (_PDPVerifier *PDPVerifierFilterer) WatchPiecesRemoved(opts *bind.WatchOpts, sink chan<- *PDPVerifierPiecesRemoved, setId []*big.Int) (event.Subscription, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "PiecesRemoved", setIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierPiecesRemoved)
				if err := _PDPVerifier.contract.UnpackLog(event, "PiecesRemoved", log); err != nil {
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

// ParsePiecesRemoved is a log parse operation binding the contract event 0x6e87df804629ac17804b57ba7abbdfac8bdc36bab504fb8a8801eb313a8ce7b1.
//
// Solidity: event PiecesRemoved(uint256 indexed setId, uint256[] pieceIds)
func (_PDPVerifier *PDPVerifierFilterer) ParsePiecesRemoved(log types.Log) (*PDPVerifierPiecesRemoved, error) {
	event := new(PDPVerifierPiecesRemoved)
	if err := _PDPVerifier.contract.UnpackLog(event, "PiecesRemoved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierPossessionProvenIterator is returned from FilterPossessionProven and is used to iterate over the raw logs and unpacked data for PossessionProven events raised by the PDPVerifier contract.
type PDPVerifierPossessionProvenIterator struct {
	Event *PDPVerifierPossessionProven // Event containing the contract specifics and raw log

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
func (it *PDPVerifierPossessionProvenIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierPossessionProven)
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
		it.Event = new(PDPVerifierPossessionProven)
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
func (it *PDPVerifierPossessionProvenIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierPossessionProvenIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierPossessionProven represents a PossessionProven event raised by the PDPVerifier contract.
type PDPVerifierPossessionProven struct {
	SetId      *big.Int
	Challenges []IPDPTypesPieceIdAndOffset
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterPossessionProven is a free log retrieval operation binding the contract event 0x1acf7df9f0c1b0208c23be6178950c0273f89b766805a2c0bd1e53d25c700e50.
//
// Solidity: event PossessionProven(uint256 indexed setId, (uint256,uint256)[] challenges)
func (_PDPVerifier *PDPVerifierFilterer) FilterPossessionProven(opts *bind.FilterOpts, setId []*big.Int) (*PDPVerifierPossessionProvenIterator, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "PossessionProven", setIdRule)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierPossessionProvenIterator{contract: _PDPVerifier.contract, event: "PossessionProven", logs: logs, sub: sub}, nil
}

// WatchPossessionProven is a free log subscription operation binding the contract event 0x1acf7df9f0c1b0208c23be6178950c0273f89b766805a2c0bd1e53d25c700e50.
//
// Solidity: event PossessionProven(uint256 indexed setId, (uint256,uint256)[] challenges)
func (_PDPVerifier *PDPVerifierFilterer) WatchPossessionProven(opts *bind.WatchOpts, sink chan<- *PDPVerifierPossessionProven, setId []*big.Int) (event.Subscription, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "PossessionProven", setIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierPossessionProven)
				if err := _PDPVerifier.contract.UnpackLog(event, "PossessionProven", log); err != nil {
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

// ParsePossessionProven is a log parse operation binding the contract event 0x1acf7df9f0c1b0208c23be6178950c0273f89b766805a2c0bd1e53d25c700e50.
//
// Solidity: event PossessionProven(uint256 indexed setId, (uint256,uint256)[] challenges)
func (_PDPVerifier *PDPVerifierFilterer) ParsePossessionProven(log types.Log) (*PDPVerifierPossessionProven, error) {
	event := new(PDPVerifierPossessionProven)
	if err := _PDPVerifier.contract.UnpackLog(event, "PossessionProven", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierProofFeePaidIterator is returned from FilterProofFeePaid and is used to iterate over the raw logs and unpacked data for ProofFeePaid events raised by the PDPVerifier contract.
type PDPVerifierProofFeePaidIterator struct {
	Event *PDPVerifierProofFeePaid // Event containing the contract specifics and raw log

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
func (it *PDPVerifierProofFeePaidIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierProofFeePaid)
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
		it.Event = new(PDPVerifierProofFeePaid)
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
func (it *PDPVerifierProofFeePaidIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierProofFeePaidIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierProofFeePaid represents a ProofFeePaid event raised by the PDPVerifier contract.
type PDPVerifierProofFeePaid struct {
	SetId *big.Int
	Fee   *big.Int
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterProofFeePaid is a free log retrieval operation binding the contract event 0x58b7742b13c8873fc0ba58f695b33ca0044b2db7ff9c5208181dbaec2a5b291e.
//
// Solidity: event ProofFeePaid(uint256 indexed setId, uint256 fee)
func (_PDPVerifier *PDPVerifierFilterer) FilterProofFeePaid(opts *bind.FilterOpts, setId []*big.Int) (*PDPVerifierProofFeePaidIterator, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "ProofFeePaid", setIdRule)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierProofFeePaidIterator{contract: _PDPVerifier.contract, event: "ProofFeePaid", logs: logs, sub: sub}, nil
}

// WatchProofFeePaid is a free log subscription operation binding the contract event 0x58b7742b13c8873fc0ba58f695b33ca0044b2db7ff9c5208181dbaec2a5b291e.
//
// Solidity: event ProofFeePaid(uint256 indexed setId, uint256 fee)
func (_PDPVerifier *PDPVerifierFilterer) WatchProofFeePaid(opts *bind.WatchOpts, sink chan<- *PDPVerifierProofFeePaid, setId []*big.Int) (event.Subscription, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "ProofFeePaid", setIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierProofFeePaid)
				if err := _PDPVerifier.contract.UnpackLog(event, "ProofFeePaid", log); err != nil {
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

// ParseProofFeePaid is a log parse operation binding the contract event 0x58b7742b13c8873fc0ba58f695b33ca0044b2db7ff9c5208181dbaec2a5b291e.
//
// Solidity: event ProofFeePaid(uint256 indexed setId, uint256 fee)
func (_PDPVerifier *PDPVerifierFilterer) ParseProofFeePaid(log types.Log) (*PDPVerifierProofFeePaid, error) {
	event := new(PDPVerifierProofFeePaid)
	if err := _PDPVerifier.contract.UnpackLog(event, "ProofFeePaid", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierStorageProviderChangedIterator is returned from FilterStorageProviderChanged and is used to iterate over the raw logs and unpacked data for StorageProviderChanged events raised by the PDPVerifier contract.
type PDPVerifierStorageProviderChangedIterator struct {
	Event *PDPVerifierStorageProviderChanged // Event containing the contract specifics and raw log

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
func (it *PDPVerifierStorageProviderChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierStorageProviderChanged)
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
		it.Event = new(PDPVerifierStorageProviderChanged)
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
func (it *PDPVerifierStorageProviderChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierStorageProviderChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierStorageProviderChanged represents a StorageProviderChanged event raised by the PDPVerifier contract.
type PDPVerifierStorageProviderChanged struct {
	SetId              *big.Int
	OldStorageProvider common.Address
	NewStorageProvider common.Address
	Raw                types.Log // Blockchain specific contextual infos
}

// FilterStorageProviderChanged is a free log retrieval operation binding the contract event 0x686146a80f2bf4dc855942926481871515b39b508826d7982a2e0212d20552c9.
//
// Solidity: event StorageProviderChanged(uint256 indexed setId, address indexed oldStorageProvider, address indexed newStorageProvider)
func (_PDPVerifier *PDPVerifierFilterer) FilterStorageProviderChanged(opts *bind.FilterOpts, setId []*big.Int, oldStorageProvider []common.Address, newStorageProvider []common.Address) (*PDPVerifierStorageProviderChangedIterator, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}
	var oldStorageProviderRule []interface{}
	for _, oldStorageProviderItem := range oldStorageProvider {
		oldStorageProviderRule = append(oldStorageProviderRule, oldStorageProviderItem)
	}
	var newStorageProviderRule []interface{}
	for _, newStorageProviderItem := range newStorageProvider {
		newStorageProviderRule = append(newStorageProviderRule, newStorageProviderItem)
	}

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "StorageProviderChanged", setIdRule, oldStorageProviderRule, newStorageProviderRule)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierStorageProviderChangedIterator{contract: _PDPVerifier.contract, event: "StorageProviderChanged", logs: logs, sub: sub}, nil
}

// WatchStorageProviderChanged is a free log subscription operation binding the contract event 0x686146a80f2bf4dc855942926481871515b39b508826d7982a2e0212d20552c9.
//
// Solidity: event StorageProviderChanged(uint256 indexed setId, address indexed oldStorageProvider, address indexed newStorageProvider)
func (_PDPVerifier *PDPVerifierFilterer) WatchStorageProviderChanged(opts *bind.WatchOpts, sink chan<- *PDPVerifierStorageProviderChanged, setId []*big.Int, oldStorageProvider []common.Address, newStorageProvider []common.Address) (event.Subscription, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}
	var oldStorageProviderRule []interface{}
	for _, oldStorageProviderItem := range oldStorageProvider {
		oldStorageProviderRule = append(oldStorageProviderRule, oldStorageProviderItem)
	}
	var newStorageProviderRule []interface{}
	for _, newStorageProviderItem := range newStorageProvider {
		newStorageProviderRule = append(newStorageProviderRule, newStorageProviderItem)
	}

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "StorageProviderChanged", setIdRule, oldStorageProviderRule, newStorageProviderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierStorageProviderChanged)
				if err := _PDPVerifier.contract.UnpackLog(event, "StorageProviderChanged", log); err != nil {
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

// ParseStorageProviderChanged is a log parse operation binding the contract event 0x686146a80f2bf4dc855942926481871515b39b508826d7982a2e0212d20552c9.
//
// Solidity: event StorageProviderChanged(uint256 indexed setId, address indexed oldStorageProvider, address indexed newStorageProvider)
func (_PDPVerifier *PDPVerifierFilterer) ParseStorageProviderChanged(log types.Log) (*PDPVerifierStorageProviderChanged, error) {
	event := new(PDPVerifierStorageProviderChanged)
	if err := _PDPVerifier.contract.UnpackLog(event, "StorageProviderChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierUpgradedIterator is returned from FilterUpgraded and is used to iterate over the raw logs and unpacked data for Upgraded events raised by the PDPVerifier contract.
type PDPVerifierUpgradedIterator struct {
	Event *PDPVerifierUpgraded // Event containing the contract specifics and raw log

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
func (it *PDPVerifierUpgradedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierUpgraded)
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
		it.Event = new(PDPVerifierUpgraded)
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
func (it *PDPVerifierUpgradedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierUpgradedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierUpgraded represents a Upgraded event raised by the PDPVerifier contract.
type PDPVerifierUpgraded struct {
	Implementation common.Address
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterUpgraded is a free log retrieval operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_PDPVerifier *PDPVerifierFilterer) FilterUpgraded(opts *bind.FilterOpts, implementation []common.Address) (*PDPVerifierUpgradedIterator, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierUpgradedIterator{contract: _PDPVerifier.contract, event: "Upgraded", logs: logs, sub: sub}, nil
}

// WatchUpgraded is a free log subscription operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_PDPVerifier *PDPVerifierFilterer) WatchUpgraded(opts *bind.WatchOpts, sink chan<- *PDPVerifierUpgraded, implementation []common.Address) (event.Subscription, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierUpgraded)
				if err := _PDPVerifier.contract.UnpackLog(event, "Upgraded", log); err != nil {
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
func (_PDPVerifier *PDPVerifierFilterer) ParseUpgraded(log types.Log) (*PDPVerifierUpgraded, error) {
	event := new(PDPVerifierUpgraded)
	if err := _PDPVerifier.contract.UnpackLog(event, "Upgraded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
