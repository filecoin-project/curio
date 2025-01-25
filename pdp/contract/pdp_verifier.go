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

// PDPVerifierProof is an auto generated low-level Go binding around an user-defined struct.
type PDPVerifierProof struct {
	Leaf  [32]byte
	Proof [][32]byte
}

// PDPVerifierRootData is an auto generated low-level Go binding around an user-defined struct.
type PDPVerifierRootData struct {
	Root    CidsCid
	RawSize *big.Int
}

// PDPVerifierRootIdAndOffset is an auto generated low-level Go binding around an user-defined struct.
type PDPVerifierRootIdAndOffset struct {
	RootId *big.Int
	Offset *big.Int
}

// PDPVerifierMetaData contains all meta data concerning the PDPVerifier contract.
var PDPVerifierMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"BURN_ACTOR\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"EXTRA_DATA_MAX_SIZE\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"FIL_USD_PRICE_FEED_ID\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"LEAF_SIZE\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"MAX_ENQUEUED_REMOVALS\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"MAX_ROOT_SIZE\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"NO_CHALLENGE_SCHEDULED\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"PYTH\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"contractIPyth\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"RANDOMNESS_PRECOMPILE\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"SECONDS_IN_DAY\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"UPGRADE_INTERFACE_VERSION\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"addRoots\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"rootData\",\"type\":\"tuple[]\",\"internalType\":\"structPDPVerifier.RootData[]\",\"components\":[{\"name\":\"root\",\"type\":\"tuple\",\"internalType\":\"structCids.Cid\",\"components\":[{\"name\":\"data\",\"type\":\"bytes\",\"internalType\":\"bytes\"}]},{\"name\":\"rawSize\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"name\":\"extraData\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"calculateProofFee\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"estimatedGasFee\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"claimProofSetOwnership\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"createProofSet\",\"inputs\":[{\"name\":\"listenerAddr\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"extraData\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"payable\"},{\"type\":\"function\",\"name\":\"deleteProofSet\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"extraData\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"findRootIds\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"leafIndexs\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"}],\"outputs\":[{\"name\":\"\",\"type\":\"tuple[]\",\"internalType\":\"structPDPVerifier.RootIdAndOffset[]\",\"components\":[{\"name\":\"rootId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"offset\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getChallengeFinality\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getChallengeRange\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getFILUSDPrice\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"\",\"type\":\"int32\",\"internalType\":\"int32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getNextChallengeEpoch\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getNextProofSetId\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getNextRootId\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getProofSetLastProvenEpoch\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getProofSetLeafCount\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getProofSetListener\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getProofSetOwner\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getRandomness\",\"inputs\":[{\"name\":\"epoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getRootCid\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"rootId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"tuple\",\"internalType\":\"structCids.Cid\",\"components\":[{\"name\":\"data\",\"type\":\"bytes\",\"internalType\":\"bytes\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getRootLeafCount\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"rootId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getScheduledRemovals\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"initialize\",\"inputs\":[{\"name\":\"_challengeFinality\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"nextProvingPeriod\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"challengeEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"extraData\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"owner\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"proofSetLive\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"proposeProofSetOwner\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"newOwner\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"provePossession\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"proofs\",\"type\":\"tuple[]\",\"internalType\":\"structPDPVerifier.Proof[]\",\"components\":[{\"name\":\"leaf\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"proof\",\"type\":\"bytes32[]\",\"internalType\":\"bytes32[]\"}]}],\"outputs\":[],\"stateMutability\":\"payable\"},{\"type\":\"function\",\"name\":\"proxiableUUID\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"renounceOwnership\",\"inputs\":[],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"rootChallengable\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"rootId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"rootLive\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"rootId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"scheduleRemovals\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"rootIds\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"},{\"name\":\"extraData\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"transferOwnership\",\"inputs\":[{\"name\":\"newOwner\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"upgradeToAndCall\",\"inputs\":[{\"name\":\"newImplementation\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"data\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"payable\"},{\"type\":\"event\",\"name\":\"Debug\",\"inputs\":[{\"name\":\"message\",\"type\":\"string\",\"indexed\":false,\"internalType\":\"string\"},{\"name\":\"value\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Initialized\",\"inputs\":[{\"name\":\"version\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"OwnershipTransferred\",\"inputs\":[{\"name\":\"previousOwner\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"newOwner\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ProofFeePaid\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"fee\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"price\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"},{\"name\":\"expo\",\"type\":\"int32\",\"indexed\":false,\"internalType\":\"int32\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ProofSetCreated\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ProofSetDeleted\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"deletedLeafCount\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ProofSetEmpty\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"RootsAdded\",\"inputs\":[{\"name\":\"firstAdded\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"RootsRemoved\",\"inputs\":[{\"name\":\"rootIds\",\"type\":\"uint256[]\",\"indexed\":true,\"internalType\":\"uint256[]\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Upgraded\",\"inputs\":[{\"name\":\"implementation\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"error\",\"name\":\"AddressEmptyCode\",\"inputs\":[{\"name\":\"target\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"ERC1967InvalidImplementation\",\"inputs\":[{\"name\":\"implementation\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"ERC1967NonPayable\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"FailedCall\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"IndexedError\",\"inputs\":[{\"name\":\"idx\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"msg\",\"type\":\"string\",\"internalType\":\"string\"}]},{\"type\":\"error\",\"name\":\"InvalidInitialization\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"NotInitializing\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"OwnableInvalidOwner\",\"inputs\":[{\"name\":\"owner\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"OwnableUnauthorizedAccount\",\"inputs\":[{\"name\":\"account\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"UUPSUnauthorizedCallContext\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"UUPSUnsupportedProxiableUUID\",\"inputs\":[{\"name\":\"slot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}]}]",
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

// BURNACTOR is a free data retrieval call binding the contract method 0x0a6a63f1.
//
// Solidity: function BURN_ACTOR() view returns(address)
func (_PDPVerifier *PDPVerifierCaller) BURNACTOR(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "BURN_ACTOR")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// BURNACTOR is a free data retrieval call binding the contract method 0x0a6a63f1.
//
// Solidity: function BURN_ACTOR() view returns(address)
func (_PDPVerifier *PDPVerifierSession) BURNACTOR() (common.Address, error) {
	return _PDPVerifier.Contract.BURNACTOR(&_PDPVerifier.CallOpts)
}

// BURNACTOR is a free data retrieval call binding the contract method 0x0a6a63f1.
//
// Solidity: function BURN_ACTOR() view returns(address)
func (_PDPVerifier *PDPVerifierCallerSession) BURNACTOR() (common.Address, error) {
	return _PDPVerifier.Contract.BURNACTOR(&_PDPVerifier.CallOpts)
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

// FILUSDPRICEFEEDID is a free data retrieval call binding the contract method 0x19c75950.
//
// Solidity: function FIL_USD_PRICE_FEED_ID() view returns(bytes32)
func (_PDPVerifier *PDPVerifierCaller) FILUSDPRICEFEEDID(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "FIL_USD_PRICE_FEED_ID")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// FILUSDPRICEFEEDID is a free data retrieval call binding the contract method 0x19c75950.
//
// Solidity: function FIL_USD_PRICE_FEED_ID() view returns(bytes32)
func (_PDPVerifier *PDPVerifierSession) FILUSDPRICEFEEDID() ([32]byte, error) {
	return _PDPVerifier.Contract.FILUSDPRICEFEEDID(&_PDPVerifier.CallOpts)
}

// FILUSDPRICEFEEDID is a free data retrieval call binding the contract method 0x19c75950.
//
// Solidity: function FIL_USD_PRICE_FEED_ID() view returns(bytes32)
func (_PDPVerifier *PDPVerifierCallerSession) FILUSDPRICEFEEDID() ([32]byte, error) {
	return _PDPVerifier.Contract.FILUSDPRICEFEEDID(&_PDPVerifier.CallOpts)
}

// LEAFSIZE is a free data retrieval call binding the contract method 0xc0e15949.
//
// Solidity: function LEAF_SIZE() view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) LEAFSIZE(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "LEAF_SIZE")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// LEAFSIZE is a free data retrieval call binding the contract method 0xc0e15949.
//
// Solidity: function LEAF_SIZE() view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) LEAFSIZE() (*big.Int, error) {
	return _PDPVerifier.Contract.LEAFSIZE(&_PDPVerifier.CallOpts)
}

// LEAFSIZE is a free data retrieval call binding the contract method 0xc0e15949.
//
// Solidity: function LEAF_SIZE() view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) LEAFSIZE() (*big.Int, error) {
	return _PDPVerifier.Contract.LEAFSIZE(&_PDPVerifier.CallOpts)
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

// MAXROOTSIZE is a free data retrieval call binding the contract method 0x16e2bcd5.
//
// Solidity: function MAX_ROOT_SIZE() view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) MAXROOTSIZE(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "MAX_ROOT_SIZE")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MAXROOTSIZE is a free data retrieval call binding the contract method 0x16e2bcd5.
//
// Solidity: function MAX_ROOT_SIZE() view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) MAXROOTSIZE() (*big.Int, error) {
	return _PDPVerifier.Contract.MAXROOTSIZE(&_PDPVerifier.CallOpts)
}

// MAXROOTSIZE is a free data retrieval call binding the contract method 0x16e2bcd5.
//
// Solidity: function MAX_ROOT_SIZE() view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) MAXROOTSIZE() (*big.Int, error) {
	return _PDPVerifier.Contract.MAXROOTSIZE(&_PDPVerifier.CallOpts)
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

// PYTH is a free data retrieval call binding the contract method 0x67e406d5.
//
// Solidity: function PYTH() view returns(address)
func (_PDPVerifier *PDPVerifierCaller) PYTH(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "PYTH")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// PYTH is a free data retrieval call binding the contract method 0x67e406d5.
//
// Solidity: function PYTH() view returns(address)
func (_PDPVerifier *PDPVerifierSession) PYTH() (common.Address, error) {
	return _PDPVerifier.Contract.PYTH(&_PDPVerifier.CallOpts)
}

// PYTH is a free data retrieval call binding the contract method 0x67e406d5.
//
// Solidity: function PYTH() view returns(address)
func (_PDPVerifier *PDPVerifierCallerSession) PYTH() (common.Address, error) {
	return _PDPVerifier.Contract.PYTH(&_PDPVerifier.CallOpts)
}

// RANDOMNESSPRECOMPILE is a free data retrieval call binding the contract method 0x15b17570.
//
// Solidity: function RANDOMNESS_PRECOMPILE() view returns(address)
func (_PDPVerifier *PDPVerifierCaller) RANDOMNESSPRECOMPILE(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "RANDOMNESS_PRECOMPILE")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// RANDOMNESSPRECOMPILE is a free data retrieval call binding the contract method 0x15b17570.
//
// Solidity: function RANDOMNESS_PRECOMPILE() view returns(address)
func (_PDPVerifier *PDPVerifierSession) RANDOMNESSPRECOMPILE() (common.Address, error) {
	return _PDPVerifier.Contract.RANDOMNESSPRECOMPILE(&_PDPVerifier.CallOpts)
}

// RANDOMNESSPRECOMPILE is a free data retrieval call binding the contract method 0x15b17570.
//
// Solidity: function RANDOMNESS_PRECOMPILE() view returns(address)
func (_PDPVerifier *PDPVerifierCallerSession) RANDOMNESSPRECOMPILE() (common.Address, error) {
	return _PDPVerifier.Contract.RANDOMNESSPRECOMPILE(&_PDPVerifier.CallOpts)
}

// SECONDSINDAY is a free data retrieval call binding the contract method 0x61a52a36.
//
// Solidity: function SECONDS_IN_DAY() view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) SECONDSINDAY(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "SECONDS_IN_DAY")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// SECONDSINDAY is a free data retrieval call binding the contract method 0x61a52a36.
//
// Solidity: function SECONDS_IN_DAY() view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) SECONDSINDAY() (*big.Int, error) {
	return _PDPVerifier.Contract.SECONDSINDAY(&_PDPVerifier.CallOpts)
}

// SECONDSINDAY is a free data retrieval call binding the contract method 0x61a52a36.
//
// Solidity: function SECONDS_IN_DAY() view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) SECONDSINDAY() (*big.Int, error) {
	return _PDPVerifier.Contract.SECONDSINDAY(&_PDPVerifier.CallOpts)
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

// CalculateProofFee is a free data retrieval call binding the contract method 0x4903704a.
//
// Solidity: function calculateProofFee(uint256 setId, uint256 estimatedGasFee) view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) CalculateProofFee(opts *bind.CallOpts, setId *big.Int, estimatedGasFee *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "calculateProofFee", setId, estimatedGasFee)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// CalculateProofFee is a free data retrieval call binding the contract method 0x4903704a.
//
// Solidity: function calculateProofFee(uint256 setId, uint256 estimatedGasFee) view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) CalculateProofFee(setId *big.Int, estimatedGasFee *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.CalculateProofFee(&_PDPVerifier.CallOpts, setId, estimatedGasFee)
}

// CalculateProofFee is a free data retrieval call binding the contract method 0x4903704a.
//
// Solidity: function calculateProofFee(uint256 setId, uint256 estimatedGasFee) view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) CalculateProofFee(setId *big.Int, estimatedGasFee *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.CalculateProofFee(&_PDPVerifier.CallOpts, setId, estimatedGasFee)
}

// FindRootIds is a free data retrieval call binding the contract method 0x0528a55b.
//
// Solidity: function findRootIds(uint256 setId, uint256[] leafIndexs) view returns((uint256,uint256)[])
func (_PDPVerifier *PDPVerifierCaller) FindRootIds(opts *bind.CallOpts, setId *big.Int, leafIndexs []*big.Int) ([]PDPVerifierRootIdAndOffset, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "findRootIds", setId, leafIndexs)

	if err != nil {
		return *new([]PDPVerifierRootIdAndOffset), err
	}

	out0 := *abi.ConvertType(out[0], new([]PDPVerifierRootIdAndOffset)).(*[]PDPVerifierRootIdAndOffset)

	return out0, err

}

// FindRootIds is a free data retrieval call binding the contract method 0x0528a55b.
//
// Solidity: function findRootIds(uint256 setId, uint256[] leafIndexs) view returns((uint256,uint256)[])
func (_PDPVerifier *PDPVerifierSession) FindRootIds(setId *big.Int, leafIndexs []*big.Int) ([]PDPVerifierRootIdAndOffset, error) {
	return _PDPVerifier.Contract.FindRootIds(&_PDPVerifier.CallOpts, setId, leafIndexs)
}

// FindRootIds is a free data retrieval call binding the contract method 0x0528a55b.
//
// Solidity: function findRootIds(uint256 setId, uint256[] leafIndexs) view returns((uint256,uint256)[])
func (_PDPVerifier *PDPVerifierCallerSession) FindRootIds(setId *big.Int, leafIndexs []*big.Int) ([]PDPVerifierRootIdAndOffset, error) {
	return _PDPVerifier.Contract.FindRootIds(&_PDPVerifier.CallOpts, setId, leafIndexs)
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

// GetFILUSDPrice is a free data retrieval call binding the contract method 0x4fa27920.
//
// Solidity: function getFILUSDPrice() view returns(uint64, int32)
func (_PDPVerifier *PDPVerifierCaller) GetFILUSDPrice(opts *bind.CallOpts) (uint64, int32, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getFILUSDPrice")

	if err != nil {
		return *new(uint64), *new(int32), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)
	out1 := *abi.ConvertType(out[1], new(int32)).(*int32)

	return out0, out1, err

}

// GetFILUSDPrice is a free data retrieval call binding the contract method 0x4fa27920.
//
// Solidity: function getFILUSDPrice() view returns(uint64, int32)
func (_PDPVerifier *PDPVerifierSession) GetFILUSDPrice() (uint64, int32, error) {
	return _PDPVerifier.Contract.GetFILUSDPrice(&_PDPVerifier.CallOpts)
}

// GetFILUSDPrice is a free data retrieval call binding the contract method 0x4fa27920.
//
// Solidity: function getFILUSDPrice() view returns(uint64, int32)
func (_PDPVerifier *PDPVerifierCallerSession) GetFILUSDPrice() (uint64, int32, error) {
	return _PDPVerifier.Contract.GetFILUSDPrice(&_PDPVerifier.CallOpts)
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

// GetNextProofSetId is a free data retrieval call binding the contract method 0x8ea417e5.
//
// Solidity: function getNextProofSetId() view returns(uint64)
func (_PDPVerifier *PDPVerifierCaller) GetNextProofSetId(opts *bind.CallOpts) (uint64, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getNextProofSetId")

	if err != nil {
		return *new(uint64), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)

	return out0, err

}

// GetNextProofSetId is a free data retrieval call binding the contract method 0x8ea417e5.
//
// Solidity: function getNextProofSetId() view returns(uint64)
func (_PDPVerifier *PDPVerifierSession) GetNextProofSetId() (uint64, error) {
	return _PDPVerifier.Contract.GetNextProofSetId(&_PDPVerifier.CallOpts)
}

// GetNextProofSetId is a free data retrieval call binding the contract method 0x8ea417e5.
//
// Solidity: function getNextProofSetId() view returns(uint64)
func (_PDPVerifier *PDPVerifierCallerSession) GetNextProofSetId() (uint64, error) {
	return _PDPVerifier.Contract.GetNextProofSetId(&_PDPVerifier.CallOpts)
}

// GetNextRootId is a free data retrieval call binding the contract method 0xd49245c1.
//
// Solidity: function getNextRootId(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) GetNextRootId(opts *bind.CallOpts, setId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getNextRootId", setId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetNextRootId is a free data retrieval call binding the contract method 0xd49245c1.
//
// Solidity: function getNextRootId(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) GetNextRootId(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetNextRootId(&_PDPVerifier.CallOpts, setId)
}

// GetNextRootId is a free data retrieval call binding the contract method 0xd49245c1.
//
// Solidity: function getNextRootId(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) GetNextRootId(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetNextRootId(&_PDPVerifier.CallOpts, setId)
}

// GetProofSetLastProvenEpoch is a free data retrieval call binding the contract method 0xfaa67163.
//
// Solidity: function getProofSetLastProvenEpoch(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) GetProofSetLastProvenEpoch(opts *bind.CallOpts, setId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getProofSetLastProvenEpoch", setId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetProofSetLastProvenEpoch is a free data retrieval call binding the contract method 0xfaa67163.
//
// Solidity: function getProofSetLastProvenEpoch(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) GetProofSetLastProvenEpoch(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetProofSetLastProvenEpoch(&_PDPVerifier.CallOpts, setId)
}

// GetProofSetLastProvenEpoch is a free data retrieval call binding the contract method 0xfaa67163.
//
// Solidity: function getProofSetLastProvenEpoch(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) GetProofSetLastProvenEpoch(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetProofSetLastProvenEpoch(&_PDPVerifier.CallOpts, setId)
}

// GetProofSetLeafCount is a free data retrieval call binding the contract method 0x3f84135f.
//
// Solidity: function getProofSetLeafCount(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) GetProofSetLeafCount(opts *bind.CallOpts, setId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getProofSetLeafCount", setId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetProofSetLeafCount is a free data retrieval call binding the contract method 0x3f84135f.
//
// Solidity: function getProofSetLeafCount(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) GetProofSetLeafCount(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetProofSetLeafCount(&_PDPVerifier.CallOpts, setId)
}

// GetProofSetLeafCount is a free data retrieval call binding the contract method 0x3f84135f.
//
// Solidity: function getProofSetLeafCount(uint256 setId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) GetProofSetLeafCount(setId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetProofSetLeafCount(&_PDPVerifier.CallOpts, setId)
}

// GetProofSetListener is a free data retrieval call binding the contract method 0x31601226.
//
// Solidity: function getProofSetListener(uint256 setId) view returns(address)
func (_PDPVerifier *PDPVerifierCaller) GetProofSetListener(opts *bind.CallOpts, setId *big.Int) (common.Address, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getProofSetListener", setId)

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// GetProofSetListener is a free data retrieval call binding the contract method 0x31601226.
//
// Solidity: function getProofSetListener(uint256 setId) view returns(address)
func (_PDPVerifier *PDPVerifierSession) GetProofSetListener(setId *big.Int) (common.Address, error) {
	return _PDPVerifier.Contract.GetProofSetListener(&_PDPVerifier.CallOpts, setId)
}

// GetProofSetListener is a free data retrieval call binding the contract method 0x31601226.
//
// Solidity: function getProofSetListener(uint256 setId) view returns(address)
func (_PDPVerifier *PDPVerifierCallerSession) GetProofSetListener(setId *big.Int) (common.Address, error) {
	return _PDPVerifier.Contract.GetProofSetListener(&_PDPVerifier.CallOpts, setId)
}

// GetProofSetOwner is a free data retrieval call binding the contract method 0x4726075b.
//
// Solidity: function getProofSetOwner(uint256 setId) view returns(address, address)
func (_PDPVerifier *PDPVerifierCaller) GetProofSetOwner(opts *bind.CallOpts, setId *big.Int) (common.Address, common.Address, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getProofSetOwner", setId)

	if err != nil {
		return *new(common.Address), *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	out1 := *abi.ConvertType(out[1], new(common.Address)).(*common.Address)

	return out0, out1, err

}

// GetProofSetOwner is a free data retrieval call binding the contract method 0x4726075b.
//
// Solidity: function getProofSetOwner(uint256 setId) view returns(address, address)
func (_PDPVerifier *PDPVerifierSession) GetProofSetOwner(setId *big.Int) (common.Address, common.Address, error) {
	return _PDPVerifier.Contract.GetProofSetOwner(&_PDPVerifier.CallOpts, setId)
}

// GetProofSetOwner is a free data retrieval call binding the contract method 0x4726075b.
//
// Solidity: function getProofSetOwner(uint256 setId) view returns(address, address)
func (_PDPVerifier *PDPVerifierCallerSession) GetProofSetOwner(setId *big.Int) (common.Address, common.Address, error) {
	return _PDPVerifier.Contract.GetProofSetOwner(&_PDPVerifier.CallOpts, setId)
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

// GetRootCid is a free data retrieval call binding the contract method 0x3b7ae913.
//
// Solidity: function getRootCid(uint256 setId, uint256 rootId) view returns((bytes))
func (_PDPVerifier *PDPVerifierCaller) GetRootCid(opts *bind.CallOpts, setId *big.Int, rootId *big.Int) (CidsCid, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getRootCid", setId, rootId)

	if err != nil {
		return *new(CidsCid), err
	}

	out0 := *abi.ConvertType(out[0], new(CidsCid)).(*CidsCid)

	return out0, err

}

// GetRootCid is a free data retrieval call binding the contract method 0x3b7ae913.
//
// Solidity: function getRootCid(uint256 setId, uint256 rootId) view returns((bytes))
func (_PDPVerifier *PDPVerifierSession) GetRootCid(setId *big.Int, rootId *big.Int) (CidsCid, error) {
	return _PDPVerifier.Contract.GetRootCid(&_PDPVerifier.CallOpts, setId, rootId)
}

// GetRootCid is a free data retrieval call binding the contract method 0x3b7ae913.
//
// Solidity: function getRootCid(uint256 setId, uint256 rootId) view returns((bytes))
func (_PDPVerifier *PDPVerifierCallerSession) GetRootCid(setId *big.Int, rootId *big.Int) (CidsCid, error) {
	return _PDPVerifier.Contract.GetRootCid(&_PDPVerifier.CallOpts, setId, rootId)
}

// GetRootLeafCount is a free data retrieval call binding the contract method 0x9153e64b.
//
// Solidity: function getRootLeafCount(uint256 setId, uint256 rootId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCaller) GetRootLeafCount(opts *bind.CallOpts, setId *big.Int, rootId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "getRootLeafCount", setId, rootId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetRootLeafCount is a free data retrieval call binding the contract method 0x9153e64b.
//
// Solidity: function getRootLeafCount(uint256 setId, uint256 rootId) view returns(uint256)
func (_PDPVerifier *PDPVerifierSession) GetRootLeafCount(setId *big.Int, rootId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetRootLeafCount(&_PDPVerifier.CallOpts, setId, rootId)
}

// GetRootLeafCount is a free data retrieval call binding the contract method 0x9153e64b.
//
// Solidity: function getRootLeafCount(uint256 setId, uint256 rootId) view returns(uint256)
func (_PDPVerifier *PDPVerifierCallerSession) GetRootLeafCount(setId *big.Int, rootId *big.Int) (*big.Int, error) {
	return _PDPVerifier.Contract.GetRootLeafCount(&_PDPVerifier.CallOpts, setId, rootId)
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

// ProofSetLive is a free data retrieval call binding the contract method 0xf5cac1ba.
//
// Solidity: function proofSetLive(uint256 setId) view returns(bool)
func (_PDPVerifier *PDPVerifierCaller) ProofSetLive(opts *bind.CallOpts, setId *big.Int) (bool, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "proofSetLive", setId)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// ProofSetLive is a free data retrieval call binding the contract method 0xf5cac1ba.
//
// Solidity: function proofSetLive(uint256 setId) view returns(bool)
func (_PDPVerifier *PDPVerifierSession) ProofSetLive(setId *big.Int) (bool, error) {
	return _PDPVerifier.Contract.ProofSetLive(&_PDPVerifier.CallOpts, setId)
}

// ProofSetLive is a free data retrieval call binding the contract method 0xf5cac1ba.
//
// Solidity: function proofSetLive(uint256 setId) view returns(bool)
func (_PDPVerifier *PDPVerifierCallerSession) ProofSetLive(setId *big.Int) (bool, error) {
	return _PDPVerifier.Contract.ProofSetLive(&_PDPVerifier.CallOpts, setId)
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

// RootChallengable is a free data retrieval call binding the contract method 0x71cf2a16.
//
// Solidity: function rootChallengable(uint256 setId, uint256 rootId) view returns(bool)
func (_PDPVerifier *PDPVerifierCaller) RootChallengable(opts *bind.CallOpts, setId *big.Int, rootId *big.Int) (bool, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "rootChallengable", setId, rootId)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// RootChallengable is a free data retrieval call binding the contract method 0x71cf2a16.
//
// Solidity: function rootChallengable(uint256 setId, uint256 rootId) view returns(bool)
func (_PDPVerifier *PDPVerifierSession) RootChallengable(setId *big.Int, rootId *big.Int) (bool, error) {
	return _PDPVerifier.Contract.RootChallengable(&_PDPVerifier.CallOpts, setId, rootId)
}

// RootChallengable is a free data retrieval call binding the contract method 0x71cf2a16.
//
// Solidity: function rootChallengable(uint256 setId, uint256 rootId) view returns(bool)
func (_PDPVerifier *PDPVerifierCallerSession) RootChallengable(setId *big.Int, rootId *big.Int) (bool, error) {
	return _PDPVerifier.Contract.RootChallengable(&_PDPVerifier.CallOpts, setId, rootId)
}

// RootLive is a free data retrieval call binding the contract method 0x47331050.
//
// Solidity: function rootLive(uint256 setId, uint256 rootId) view returns(bool)
func (_PDPVerifier *PDPVerifierCaller) RootLive(opts *bind.CallOpts, setId *big.Int, rootId *big.Int) (bool, error) {
	var out []interface{}
	err := _PDPVerifier.contract.Call(opts, &out, "rootLive", setId, rootId)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// RootLive is a free data retrieval call binding the contract method 0x47331050.
//
// Solidity: function rootLive(uint256 setId, uint256 rootId) view returns(bool)
func (_PDPVerifier *PDPVerifierSession) RootLive(setId *big.Int, rootId *big.Int) (bool, error) {
	return _PDPVerifier.Contract.RootLive(&_PDPVerifier.CallOpts, setId, rootId)
}

// RootLive is a free data retrieval call binding the contract method 0x47331050.
//
// Solidity: function rootLive(uint256 setId, uint256 rootId) view returns(bool)
func (_PDPVerifier *PDPVerifierCallerSession) RootLive(setId *big.Int, rootId *big.Int) (bool, error) {
	return _PDPVerifier.Contract.RootLive(&_PDPVerifier.CallOpts, setId, rootId)
}

// AddRoots is a paid mutator transaction binding the contract method 0x11c0ee4a.
//
// Solidity: function addRoots(uint256 setId, ((bytes),uint256)[] rootData, bytes extraData) returns(uint256)
func (_PDPVerifier *PDPVerifierTransactor) AddRoots(opts *bind.TransactOpts, setId *big.Int, rootData []PDPVerifierRootData, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "addRoots", setId, rootData, extraData)
}

// AddRoots is a paid mutator transaction binding the contract method 0x11c0ee4a.
//
// Solidity: function addRoots(uint256 setId, ((bytes),uint256)[] rootData, bytes extraData) returns(uint256)
func (_PDPVerifier *PDPVerifierSession) AddRoots(setId *big.Int, rootData []PDPVerifierRootData, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.AddRoots(&_PDPVerifier.TransactOpts, setId, rootData, extraData)
}

// AddRoots is a paid mutator transaction binding the contract method 0x11c0ee4a.
//
// Solidity: function addRoots(uint256 setId, ((bytes),uint256)[] rootData, bytes extraData) returns(uint256)
func (_PDPVerifier *PDPVerifierTransactorSession) AddRoots(setId *big.Int, rootData []PDPVerifierRootData, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.AddRoots(&_PDPVerifier.TransactOpts, setId, rootData, extraData)
}

// ClaimProofSetOwnership is a paid mutator transaction binding the contract method 0xee3dac65.
//
// Solidity: function claimProofSetOwnership(uint256 setId) returns()
func (_PDPVerifier *PDPVerifierTransactor) ClaimProofSetOwnership(opts *bind.TransactOpts, setId *big.Int) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "claimProofSetOwnership", setId)
}

// ClaimProofSetOwnership is a paid mutator transaction binding the contract method 0xee3dac65.
//
// Solidity: function claimProofSetOwnership(uint256 setId) returns()
func (_PDPVerifier *PDPVerifierSession) ClaimProofSetOwnership(setId *big.Int) (*types.Transaction, error) {
	return _PDPVerifier.Contract.ClaimProofSetOwnership(&_PDPVerifier.TransactOpts, setId)
}

// ClaimProofSetOwnership is a paid mutator transaction binding the contract method 0xee3dac65.
//
// Solidity: function claimProofSetOwnership(uint256 setId) returns()
func (_PDPVerifier *PDPVerifierTransactorSession) ClaimProofSetOwnership(setId *big.Int) (*types.Transaction, error) {
	return _PDPVerifier.Contract.ClaimProofSetOwnership(&_PDPVerifier.TransactOpts, setId)
}

// CreateProofSet is a paid mutator transaction binding the contract method 0x0a4d7932.
//
// Solidity: function createProofSet(address listenerAddr, bytes extraData) payable returns(uint256)
func (_PDPVerifier *PDPVerifierTransactor) CreateProofSet(opts *bind.TransactOpts, listenerAddr common.Address, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "createProofSet", listenerAddr, extraData)
}

// CreateProofSet is a paid mutator transaction binding the contract method 0x0a4d7932.
//
// Solidity: function createProofSet(address listenerAddr, bytes extraData) payable returns(uint256)
func (_PDPVerifier *PDPVerifierSession) CreateProofSet(listenerAddr common.Address, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.CreateProofSet(&_PDPVerifier.TransactOpts, listenerAddr, extraData)
}

// CreateProofSet is a paid mutator transaction binding the contract method 0x0a4d7932.
//
// Solidity: function createProofSet(address listenerAddr, bytes extraData) payable returns(uint256)
func (_PDPVerifier *PDPVerifierTransactorSession) CreateProofSet(listenerAddr common.Address, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.CreateProofSet(&_PDPVerifier.TransactOpts, listenerAddr, extraData)
}

// DeleteProofSet is a paid mutator transaction binding the contract method 0x847d1d06.
//
// Solidity: function deleteProofSet(uint256 setId, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierTransactor) DeleteProofSet(opts *bind.TransactOpts, setId *big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "deleteProofSet", setId, extraData)
}

// DeleteProofSet is a paid mutator transaction binding the contract method 0x847d1d06.
//
// Solidity: function deleteProofSet(uint256 setId, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierSession) DeleteProofSet(setId *big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.DeleteProofSet(&_PDPVerifier.TransactOpts, setId, extraData)
}

// DeleteProofSet is a paid mutator transaction binding the contract method 0x847d1d06.
//
// Solidity: function deleteProofSet(uint256 setId, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierTransactorSession) DeleteProofSet(setId *big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.DeleteProofSet(&_PDPVerifier.TransactOpts, setId, extraData)
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

// ProposeProofSetOwner is a paid mutator transaction binding the contract method 0x6cb55c16.
//
// Solidity: function proposeProofSetOwner(uint256 setId, address newOwner) returns()
func (_PDPVerifier *PDPVerifierTransactor) ProposeProofSetOwner(opts *bind.TransactOpts, setId *big.Int, newOwner common.Address) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "proposeProofSetOwner", setId, newOwner)
}

// ProposeProofSetOwner is a paid mutator transaction binding the contract method 0x6cb55c16.
//
// Solidity: function proposeProofSetOwner(uint256 setId, address newOwner) returns()
func (_PDPVerifier *PDPVerifierSession) ProposeProofSetOwner(setId *big.Int, newOwner common.Address) (*types.Transaction, error) {
	return _PDPVerifier.Contract.ProposeProofSetOwner(&_PDPVerifier.TransactOpts, setId, newOwner)
}

// ProposeProofSetOwner is a paid mutator transaction binding the contract method 0x6cb55c16.
//
// Solidity: function proposeProofSetOwner(uint256 setId, address newOwner) returns()
func (_PDPVerifier *PDPVerifierTransactorSession) ProposeProofSetOwner(setId *big.Int, newOwner common.Address) (*types.Transaction, error) {
	return _PDPVerifier.Contract.ProposeProofSetOwner(&_PDPVerifier.TransactOpts, setId, newOwner)
}

// ProvePossession is a paid mutator transaction binding the contract method 0xf58f952b.
//
// Solidity: function provePossession(uint256 setId, (bytes32,bytes32[])[] proofs) payable returns()
func (_PDPVerifier *PDPVerifierTransactor) ProvePossession(opts *bind.TransactOpts, setId *big.Int, proofs []PDPVerifierProof) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "provePossession", setId, proofs)
}

// ProvePossession is a paid mutator transaction binding the contract method 0xf58f952b.
//
// Solidity: function provePossession(uint256 setId, (bytes32,bytes32[])[] proofs) payable returns()
func (_PDPVerifier *PDPVerifierSession) ProvePossession(setId *big.Int, proofs []PDPVerifierProof) (*types.Transaction, error) {
	return _PDPVerifier.Contract.ProvePossession(&_PDPVerifier.TransactOpts, setId, proofs)
}

// ProvePossession is a paid mutator transaction binding the contract method 0xf58f952b.
//
// Solidity: function provePossession(uint256 setId, (bytes32,bytes32[])[] proofs) payable returns()
func (_PDPVerifier *PDPVerifierTransactorSession) ProvePossession(setId *big.Int, proofs []PDPVerifierProof) (*types.Transaction, error) {
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

// ScheduleRemovals is a paid mutator transaction binding the contract method 0x3b68e4e9.
//
// Solidity: function scheduleRemovals(uint256 setId, uint256[] rootIds, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierTransactor) ScheduleRemovals(opts *bind.TransactOpts, setId *big.Int, rootIds []*big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.contract.Transact(opts, "scheduleRemovals", setId, rootIds, extraData)
}

// ScheduleRemovals is a paid mutator transaction binding the contract method 0x3b68e4e9.
//
// Solidity: function scheduleRemovals(uint256 setId, uint256[] rootIds, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierSession) ScheduleRemovals(setId *big.Int, rootIds []*big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.ScheduleRemovals(&_PDPVerifier.TransactOpts, setId, rootIds, extraData)
}

// ScheduleRemovals is a paid mutator transaction binding the contract method 0x3b68e4e9.
//
// Solidity: function scheduleRemovals(uint256 setId, uint256[] rootIds, bytes extraData) returns()
func (_PDPVerifier *PDPVerifierTransactorSession) ScheduleRemovals(setId *big.Int, rootIds []*big.Int, extraData []byte) (*types.Transaction, error) {
	return _PDPVerifier.Contract.ScheduleRemovals(&_PDPVerifier.TransactOpts, setId, rootIds, extraData)
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

// PDPVerifierDebugIterator is returned from FilterDebug and is used to iterate over the raw logs and unpacked data for Debug events raised by the PDPVerifier contract.
type PDPVerifierDebugIterator struct {
	Event *PDPVerifierDebug // Event containing the contract specifics and raw log

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
func (it *PDPVerifierDebugIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierDebug)
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
		it.Event = new(PDPVerifierDebug)
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
func (it *PDPVerifierDebugIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierDebugIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierDebug represents a Debug event raised by the PDPVerifier contract.
type PDPVerifierDebug struct {
	Message string
	Value   *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterDebug is a free log retrieval operation binding the contract event 0x3c5ad147104e56be34a9176a6692f7df8d2f4b29a5af06bc6b98970d329d6577.
//
// Solidity: event Debug(string message, uint256 value)
func (_PDPVerifier *PDPVerifierFilterer) FilterDebug(opts *bind.FilterOpts) (*PDPVerifierDebugIterator, error) {

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "Debug")
	if err != nil {
		return nil, err
	}
	return &PDPVerifierDebugIterator{contract: _PDPVerifier.contract, event: "Debug", logs: logs, sub: sub}, nil
}

// WatchDebug is a free log subscription operation binding the contract event 0x3c5ad147104e56be34a9176a6692f7df8d2f4b29a5af06bc6b98970d329d6577.
//
// Solidity: event Debug(string message, uint256 value)
func (_PDPVerifier *PDPVerifierFilterer) WatchDebug(opts *bind.WatchOpts, sink chan<- *PDPVerifierDebug) (event.Subscription, error) {

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "Debug")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierDebug)
				if err := _PDPVerifier.contract.UnpackLog(event, "Debug", log); err != nil {
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

// ParseDebug is a log parse operation binding the contract event 0x3c5ad147104e56be34a9176a6692f7df8d2f4b29a5af06bc6b98970d329d6577.
//
// Solidity: event Debug(string message, uint256 value)
func (_PDPVerifier *PDPVerifierFilterer) ParseDebug(log types.Log) (*PDPVerifierDebug, error) {
	event := new(PDPVerifierDebug)
	if err := _PDPVerifier.contract.UnpackLog(event, "Debug", log); err != nil {
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
	Price uint64
	Expo  int32
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterProofFeePaid is a free log retrieval operation binding the contract event 0x928bbf5188022bf8b9a0e59f5e81e179d0a4c729bdba2856ac971af2063fbf2b.
//
// Solidity: event ProofFeePaid(uint256 indexed setId, uint256 fee, uint64 price, int32 expo)
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

// WatchProofFeePaid is a free log subscription operation binding the contract event 0x928bbf5188022bf8b9a0e59f5e81e179d0a4c729bdba2856ac971af2063fbf2b.
//
// Solidity: event ProofFeePaid(uint256 indexed setId, uint256 fee, uint64 price, int32 expo)
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

// ParseProofFeePaid is a log parse operation binding the contract event 0x928bbf5188022bf8b9a0e59f5e81e179d0a4c729bdba2856ac971af2063fbf2b.
//
// Solidity: event ProofFeePaid(uint256 indexed setId, uint256 fee, uint64 price, int32 expo)
func (_PDPVerifier *PDPVerifierFilterer) ParseProofFeePaid(log types.Log) (*PDPVerifierProofFeePaid, error) {
	event := new(PDPVerifierProofFeePaid)
	if err := _PDPVerifier.contract.UnpackLog(event, "ProofFeePaid", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierProofSetCreatedIterator is returned from FilterProofSetCreated and is used to iterate over the raw logs and unpacked data for ProofSetCreated events raised by the PDPVerifier contract.
type PDPVerifierProofSetCreatedIterator struct {
	Event *PDPVerifierProofSetCreated // Event containing the contract specifics and raw log

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
func (it *PDPVerifierProofSetCreatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierProofSetCreated)
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
		it.Event = new(PDPVerifierProofSetCreated)
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
func (it *PDPVerifierProofSetCreatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierProofSetCreatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierProofSetCreated represents a ProofSetCreated event raised by the PDPVerifier contract.
type PDPVerifierProofSetCreated struct {
	SetId *big.Int
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterProofSetCreated is a free log retrieval operation binding the contract event 0x5979d495e336598dba8459e44f8eb2a1c957ce30fcc10cabea4bb0ffe969df6a.
//
// Solidity: event ProofSetCreated(uint256 indexed setId)
func (_PDPVerifier *PDPVerifierFilterer) FilterProofSetCreated(opts *bind.FilterOpts, setId []*big.Int) (*PDPVerifierProofSetCreatedIterator, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "ProofSetCreated", setIdRule)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierProofSetCreatedIterator{contract: _PDPVerifier.contract, event: "ProofSetCreated", logs: logs, sub: sub}, nil
}

// WatchProofSetCreated is a free log subscription operation binding the contract event 0x5979d495e336598dba8459e44f8eb2a1c957ce30fcc10cabea4bb0ffe969df6a.
//
// Solidity: event ProofSetCreated(uint256 indexed setId)
func (_PDPVerifier *PDPVerifierFilterer) WatchProofSetCreated(opts *bind.WatchOpts, sink chan<- *PDPVerifierProofSetCreated, setId []*big.Int) (event.Subscription, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "ProofSetCreated", setIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierProofSetCreated)
				if err := _PDPVerifier.contract.UnpackLog(event, "ProofSetCreated", log); err != nil {
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

// ParseProofSetCreated is a log parse operation binding the contract event 0x5979d495e336598dba8459e44f8eb2a1c957ce30fcc10cabea4bb0ffe969df6a.
//
// Solidity: event ProofSetCreated(uint256 indexed setId)
func (_PDPVerifier *PDPVerifierFilterer) ParseProofSetCreated(log types.Log) (*PDPVerifierProofSetCreated, error) {
	event := new(PDPVerifierProofSetCreated)
	if err := _PDPVerifier.contract.UnpackLog(event, "ProofSetCreated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierProofSetDeletedIterator is returned from FilterProofSetDeleted and is used to iterate over the raw logs and unpacked data for ProofSetDeleted events raised by the PDPVerifier contract.
type PDPVerifierProofSetDeletedIterator struct {
	Event *PDPVerifierProofSetDeleted // Event containing the contract specifics and raw log

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
func (it *PDPVerifierProofSetDeletedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierProofSetDeleted)
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
		it.Event = new(PDPVerifierProofSetDeleted)
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
func (it *PDPVerifierProofSetDeletedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierProofSetDeletedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierProofSetDeleted represents a ProofSetDeleted event raised by the PDPVerifier contract.
type PDPVerifierProofSetDeleted struct {
	SetId            *big.Int
	DeletedLeafCount *big.Int
	Raw              types.Log // Blockchain specific contextual infos
}

// FilterProofSetDeleted is a free log retrieval operation binding the contract event 0x589e9a441b5bddda77c4ab647b0108764a9cc1a7f655aa9b7bc50b8bdfab8673.
//
// Solidity: event ProofSetDeleted(uint256 indexed setId, uint256 deletedLeafCount)
func (_PDPVerifier *PDPVerifierFilterer) FilterProofSetDeleted(opts *bind.FilterOpts, setId []*big.Int) (*PDPVerifierProofSetDeletedIterator, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "ProofSetDeleted", setIdRule)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierProofSetDeletedIterator{contract: _PDPVerifier.contract, event: "ProofSetDeleted", logs: logs, sub: sub}, nil
}

// WatchProofSetDeleted is a free log subscription operation binding the contract event 0x589e9a441b5bddda77c4ab647b0108764a9cc1a7f655aa9b7bc50b8bdfab8673.
//
// Solidity: event ProofSetDeleted(uint256 indexed setId, uint256 deletedLeafCount)
func (_PDPVerifier *PDPVerifierFilterer) WatchProofSetDeleted(opts *bind.WatchOpts, sink chan<- *PDPVerifierProofSetDeleted, setId []*big.Int) (event.Subscription, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "ProofSetDeleted", setIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierProofSetDeleted)
				if err := _PDPVerifier.contract.UnpackLog(event, "ProofSetDeleted", log); err != nil {
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

// ParseProofSetDeleted is a log parse operation binding the contract event 0x589e9a441b5bddda77c4ab647b0108764a9cc1a7f655aa9b7bc50b8bdfab8673.
//
// Solidity: event ProofSetDeleted(uint256 indexed setId, uint256 deletedLeafCount)
func (_PDPVerifier *PDPVerifierFilterer) ParseProofSetDeleted(log types.Log) (*PDPVerifierProofSetDeleted, error) {
	event := new(PDPVerifierProofSetDeleted)
	if err := _PDPVerifier.contract.UnpackLog(event, "ProofSetDeleted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierProofSetEmptyIterator is returned from FilterProofSetEmpty and is used to iterate over the raw logs and unpacked data for ProofSetEmpty events raised by the PDPVerifier contract.
type PDPVerifierProofSetEmptyIterator struct {
	Event *PDPVerifierProofSetEmpty // Event containing the contract specifics and raw log

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
func (it *PDPVerifierProofSetEmptyIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierProofSetEmpty)
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
		it.Event = new(PDPVerifierProofSetEmpty)
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
func (it *PDPVerifierProofSetEmptyIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierProofSetEmptyIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierProofSetEmpty represents a ProofSetEmpty event raised by the PDPVerifier contract.
type PDPVerifierProofSetEmpty struct {
	SetId *big.Int
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterProofSetEmpty is a free log retrieval operation binding the contract event 0x323c29bc8d678a5d987b90a321982d10b9a91bcad071a9e445879497bf0e68e7.
//
// Solidity: event ProofSetEmpty(uint256 indexed setId)
func (_PDPVerifier *PDPVerifierFilterer) FilterProofSetEmpty(opts *bind.FilterOpts, setId []*big.Int) (*PDPVerifierProofSetEmptyIterator, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "ProofSetEmpty", setIdRule)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierProofSetEmptyIterator{contract: _PDPVerifier.contract, event: "ProofSetEmpty", logs: logs, sub: sub}, nil
}

// WatchProofSetEmpty is a free log subscription operation binding the contract event 0x323c29bc8d678a5d987b90a321982d10b9a91bcad071a9e445879497bf0e68e7.
//
// Solidity: event ProofSetEmpty(uint256 indexed setId)
func (_PDPVerifier *PDPVerifierFilterer) WatchProofSetEmpty(opts *bind.WatchOpts, sink chan<- *PDPVerifierProofSetEmpty, setId []*big.Int) (event.Subscription, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "ProofSetEmpty", setIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierProofSetEmpty)
				if err := _PDPVerifier.contract.UnpackLog(event, "ProofSetEmpty", log); err != nil {
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

// ParseProofSetEmpty is a log parse operation binding the contract event 0x323c29bc8d678a5d987b90a321982d10b9a91bcad071a9e445879497bf0e68e7.
//
// Solidity: event ProofSetEmpty(uint256 indexed setId)
func (_PDPVerifier *PDPVerifierFilterer) ParseProofSetEmpty(log types.Log) (*PDPVerifierProofSetEmpty, error) {
	event := new(PDPVerifierProofSetEmpty)
	if err := _PDPVerifier.contract.UnpackLog(event, "ProofSetEmpty", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierRootsAddedIterator is returned from FilterRootsAdded and is used to iterate over the raw logs and unpacked data for RootsAdded events raised by the PDPVerifier contract.
type PDPVerifierRootsAddedIterator struct {
	Event *PDPVerifierRootsAdded // Event containing the contract specifics and raw log

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
func (it *PDPVerifierRootsAddedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierRootsAdded)
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
		it.Event = new(PDPVerifierRootsAdded)
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
func (it *PDPVerifierRootsAddedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierRootsAddedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierRootsAdded represents a RootsAdded event raised by the PDPVerifier contract.
type PDPVerifierRootsAdded struct {
	FirstAdded *big.Int
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterRootsAdded is a free log retrieval operation binding the contract event 0xde16f8a35fc44238b21a493b7267f7a5064f2bc1b3a1af51deeb4b5a47281d47.
//
// Solidity: event RootsAdded(uint256 indexed firstAdded)
func (_PDPVerifier *PDPVerifierFilterer) FilterRootsAdded(opts *bind.FilterOpts, firstAdded []*big.Int) (*PDPVerifierRootsAddedIterator, error) {

	var firstAddedRule []interface{}
	for _, firstAddedItem := range firstAdded {
		firstAddedRule = append(firstAddedRule, firstAddedItem)
	}

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "RootsAdded", firstAddedRule)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierRootsAddedIterator{contract: _PDPVerifier.contract, event: "RootsAdded", logs: logs, sub: sub}, nil
}

// WatchRootsAdded is a free log subscription operation binding the contract event 0xde16f8a35fc44238b21a493b7267f7a5064f2bc1b3a1af51deeb4b5a47281d47.
//
// Solidity: event RootsAdded(uint256 indexed firstAdded)
func (_PDPVerifier *PDPVerifierFilterer) WatchRootsAdded(opts *bind.WatchOpts, sink chan<- *PDPVerifierRootsAdded, firstAdded []*big.Int) (event.Subscription, error) {

	var firstAddedRule []interface{}
	for _, firstAddedItem := range firstAdded {
		firstAddedRule = append(firstAddedRule, firstAddedItem)
	}

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "RootsAdded", firstAddedRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierRootsAdded)
				if err := _PDPVerifier.contract.UnpackLog(event, "RootsAdded", log); err != nil {
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

// ParseRootsAdded is a log parse operation binding the contract event 0xde16f8a35fc44238b21a493b7267f7a5064f2bc1b3a1af51deeb4b5a47281d47.
//
// Solidity: event RootsAdded(uint256 indexed firstAdded)
func (_PDPVerifier *PDPVerifierFilterer) ParseRootsAdded(log types.Log) (*PDPVerifierRootsAdded, error) {
	event := new(PDPVerifierRootsAdded)
	if err := _PDPVerifier.contract.UnpackLog(event, "RootsAdded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPVerifierRootsRemovedIterator is returned from FilterRootsRemoved and is used to iterate over the raw logs and unpacked data for RootsRemoved events raised by the PDPVerifier contract.
type PDPVerifierRootsRemovedIterator struct {
	Event *PDPVerifierRootsRemoved // Event containing the contract specifics and raw log

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
func (it *PDPVerifierRootsRemovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPVerifierRootsRemoved)
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
		it.Event = new(PDPVerifierRootsRemoved)
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
func (it *PDPVerifierRootsRemovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPVerifierRootsRemovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPVerifierRootsRemoved represents a RootsRemoved event raised by the PDPVerifier contract.
type PDPVerifierRootsRemoved struct {
	RootIds []*big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRootsRemoved is a free log retrieval operation binding the contract event 0x27acad209a3635943e55118deed44ace417c09eff8fb78836bcfa1b3e01e0d6b.
//
// Solidity: event RootsRemoved(uint256[] indexed rootIds)
func (_PDPVerifier *PDPVerifierFilterer) FilterRootsRemoved(opts *bind.FilterOpts, rootIds [][]*big.Int) (*PDPVerifierRootsRemovedIterator, error) {

	var rootIdsRule []interface{}
	for _, rootIdsItem := range rootIds {
		rootIdsRule = append(rootIdsRule, rootIdsItem)
	}

	logs, sub, err := _PDPVerifier.contract.FilterLogs(opts, "RootsRemoved", rootIdsRule)
	if err != nil {
		return nil, err
	}
	return &PDPVerifierRootsRemovedIterator{contract: _PDPVerifier.contract, event: "RootsRemoved", logs: logs, sub: sub}, nil
}

// WatchRootsRemoved is a free log subscription operation binding the contract event 0x27acad209a3635943e55118deed44ace417c09eff8fb78836bcfa1b3e01e0d6b.
//
// Solidity: event RootsRemoved(uint256[] indexed rootIds)
func (_PDPVerifier *PDPVerifierFilterer) WatchRootsRemoved(opts *bind.WatchOpts, sink chan<- *PDPVerifierRootsRemoved, rootIds [][]*big.Int) (event.Subscription, error) {

	var rootIdsRule []interface{}
	for _, rootIdsItem := range rootIds {
		rootIdsRule = append(rootIdsRule, rootIdsItem)
	}

	logs, sub, err := _PDPVerifier.contract.WatchLogs(opts, "RootsRemoved", rootIdsRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPVerifierRootsRemoved)
				if err := _PDPVerifier.contract.UnpackLog(event, "RootsRemoved", log); err != nil {
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

// ParseRootsRemoved is a log parse operation binding the contract event 0x27acad209a3635943e55118deed44ace417c09eff8fb78836bcfa1b3e01e0d6b.
//
// Solidity: event RootsRemoved(uint256[] indexed rootIds)
func (_PDPVerifier *PDPVerifierFilterer) ParseRootsRemoved(log types.Log) (*PDPVerifierRootsRemoved, error) {
	event := new(PDPVerifierRootsRemoved)
	if err := _PDPVerifier.contract.UnpackLog(event, "RootsRemoved", log); err != nil {
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
