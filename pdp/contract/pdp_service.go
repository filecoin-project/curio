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

// PDPServiceProof is an auto generated low-level Go binding around an user-defined struct.
type PDPServiceProof struct {
	Leaf  [32]byte
	Proof [][32]byte
}

// PDPServiceRootData is an auto generated low-level Go binding around an user-defined struct.
type PDPServiceRootData struct {
	Root    CidsCid
	RawSize *big.Int
}

// PDPServiceRootIdAndOffset is an auto generated low-level Go binding around an user-defined struct.
type PDPServiceRootIdAndOffset struct {
	RootId *big.Int
	Offset *big.Int
}

// PDPServiceMetaData contains all meta data concerning the PDPService contract.
var PDPServiceMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"_challengeFinality\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"idx\",\"type\":\"uint256\"},{\"internalType\":\"string\",\"name\":\"msg\",\"type\":\"string\"}],\"name\":\"IndexedError\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"setId\",\"type\":\"uint256\"}],\"name\":\"ProofSetCreated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"firstAdded\",\"type\":\"uint256\"}],\"name\":\"RootsAdded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"totalDelta\",\"type\":\"uint256\"}],\"name\":\"RootsRemoved\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"LEAF_SIZE\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"MAX_ROOT_SIZE\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"setId\",\"type\":\"uint256\"},{\"components\":[{\"components\":[{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"internalType\":\"structCids.Cid\",\"name\":\"root\",\"type\":\"tuple\"},{\"internalType\":\"uint256\",\"name\":\"rawSize\",\"type\":\"uint256\"}],\"internalType\":\"structPDPService.RootData[]\",\"name\":\"rootData\",\"type\":\"tuple[]\"}],\"name\":\"addRoots\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"setId\",\"type\":\"uint256\"}],\"name\":\"claimProofSetOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"recordKeeper\",\"type\":\"address\"}],\"name\":\"createProofSet\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"setId\",\"type\":\"uint256\"}],\"name\":\"deleteProofSet\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"setId\",\"type\":\"uint256\"},{\"internalType\":\"uint256[]\",\"name\":\"leafIndexs\",\"type\":\"uint256[]\"}],\"name\":\"findRootIds\",\"outputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"rootId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"offset\",\"type\":\"uint256\"}],\"internalType\":\"structPDPService.RootIdAndOffset[]\",\"name\":\"\",\"type\":\"tuple[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getChallengeFinality\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"setId\",\"type\":\"uint256\"}],\"name\":\"getNextChallengeEpoch\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getNextProofSetId\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"\",\"type\":\"uint64\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"setId\",\"type\":\"uint256\"}],\"name\":\"getNextRootId\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"setId\",\"type\":\"uint256\"}],\"name\":\"getProofSetLeafCount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"setId\",\"type\":\"uint256\"}],\"name\":\"getProofSetOwner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"setId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"rootId\",\"type\":\"uint256\"}],\"name\":\"getRootCid\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"internalType\":\"structCids.Cid\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"setId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"rootId\",\"type\":\"uint256\"}],\"name\":\"getRootLeafCount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"setId\",\"type\":\"uint256\"}],\"name\":\"proofSetLive\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"setId\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"proposeProofSetOwner\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"setId\",\"type\":\"uint256\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"leaf\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32[]\",\"name\":\"proof\",\"type\":\"bytes32[]\"}],\"internalType\":\"structPDPService.Proof[]\",\"name\":\"proofs\",\"type\":\"tuple[]\"}],\"name\":\"provePossession\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"setId\",\"type\":\"uint256\"},{\"internalType\":\"uint256[]\",\"name\":\"rootIds\",\"type\":\"uint256[]\"}],\"name\":\"removeRoots\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"setId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"rootId\",\"type\":\"uint256\"}],\"name\":\"rootLive\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
}

// PDPServiceABI is the input ABI used to generate the binding from.
// Deprecated: Use PDPServiceMetaData.ABI instead.
var PDPServiceABI = PDPServiceMetaData.ABI

// PDPService is an auto generated Go binding around an Ethereum contract.
type PDPService struct {
	PDPServiceCaller     // Read-only binding to the contract
	PDPServiceTransactor // Write-only binding to the contract
	PDPServiceFilterer   // Log filterer for contract events
}

// PDPServiceCaller is an auto generated read-only Go binding around an Ethereum contract.
type PDPServiceCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PDPServiceTransactor is an auto generated write-only Go binding around an Ethereum contract.
type PDPServiceTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PDPServiceFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type PDPServiceFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PDPServiceSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type PDPServiceSession struct {
	Contract     *PDPService       // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// PDPServiceCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type PDPServiceCallerSession struct {
	Contract *PDPServiceCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts     // Call options to use throughout this session
}

// PDPServiceTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type PDPServiceTransactorSession struct {
	Contract     *PDPServiceTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts     // Transaction auth options to use throughout this session
}

// PDPServiceRaw is an auto generated low-level Go binding around an Ethereum contract.
type PDPServiceRaw struct {
	Contract *PDPService // Generic contract binding to access the raw methods on
}

// PDPServiceCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type PDPServiceCallerRaw struct {
	Contract *PDPServiceCaller // Generic read-only contract binding to access the raw methods on
}

// PDPServiceTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type PDPServiceTransactorRaw struct {
	Contract *PDPServiceTransactor // Generic write-only contract binding to access the raw methods on
}

// NewPDPService creates a new instance of PDPService, bound to a specific deployed contract.
func NewPDPService(address common.Address, backend bind.ContractBackend) (*PDPService, error) {
	contract, err := bindPDPService(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &PDPService{PDPServiceCaller: PDPServiceCaller{contract: contract}, PDPServiceTransactor: PDPServiceTransactor{contract: contract}, PDPServiceFilterer: PDPServiceFilterer{contract: contract}}, nil
}

// NewPDPServiceCaller creates a new read-only instance of PDPService, bound to a specific deployed contract.
func NewPDPServiceCaller(address common.Address, caller bind.ContractCaller) (*PDPServiceCaller, error) {
	contract, err := bindPDPService(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &PDPServiceCaller{contract: contract}, nil
}

// NewPDPServiceTransactor creates a new write-only instance of PDPService, bound to a specific deployed contract.
func NewPDPServiceTransactor(address common.Address, transactor bind.ContractTransactor) (*PDPServiceTransactor, error) {
	contract, err := bindPDPService(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &PDPServiceTransactor{contract: contract}, nil
}

// NewPDPServiceFilterer creates a new log filterer instance of PDPService, bound to a specific deployed contract.
func NewPDPServiceFilterer(address common.Address, filterer bind.ContractFilterer) (*PDPServiceFilterer, error) {
	contract, err := bindPDPService(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &PDPServiceFilterer{contract: contract}, nil
}

// bindPDPService binds a generic wrapper to an already deployed contract.
func bindPDPService(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := PDPServiceMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_PDPService *PDPServiceRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _PDPService.Contract.PDPServiceCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_PDPService *PDPServiceRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _PDPService.Contract.PDPServiceTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_PDPService *PDPServiceRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _PDPService.Contract.PDPServiceTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_PDPService *PDPServiceCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _PDPService.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_PDPService *PDPServiceTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _PDPService.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_PDPService *PDPServiceTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _PDPService.Contract.contract.Transact(opts, method, params...)
}

// LEAFSIZE is a free data retrieval call binding the contract method 0xc0e15949.
//
// Solidity: function LEAF_SIZE() view returns(uint256)
func (_PDPService *PDPServiceCaller) LEAFSIZE(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _PDPService.contract.Call(opts, &out, "LEAF_SIZE")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// LEAFSIZE is a free data retrieval call binding the contract method 0xc0e15949.
//
// Solidity: function LEAF_SIZE() view returns(uint256)
func (_PDPService *PDPServiceSession) LEAFSIZE() (*big.Int, error) {
	return _PDPService.Contract.LEAFSIZE(&_PDPService.CallOpts)
}

// LEAFSIZE is a free data retrieval call binding the contract method 0xc0e15949.
//
// Solidity: function LEAF_SIZE() view returns(uint256)
func (_PDPService *PDPServiceCallerSession) LEAFSIZE() (*big.Int, error) {
	return _PDPService.Contract.LEAFSIZE(&_PDPService.CallOpts)
}

// MAXROOTSIZE is a free data retrieval call binding the contract method 0x16e2bcd5.
//
// Solidity: function MAX_ROOT_SIZE() view returns(uint256)
func (_PDPService *PDPServiceCaller) MAXROOTSIZE(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _PDPService.contract.Call(opts, &out, "MAX_ROOT_SIZE")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MAXROOTSIZE is a free data retrieval call binding the contract method 0x16e2bcd5.
//
// Solidity: function MAX_ROOT_SIZE() view returns(uint256)
func (_PDPService *PDPServiceSession) MAXROOTSIZE() (*big.Int, error) {
	return _PDPService.Contract.MAXROOTSIZE(&_PDPService.CallOpts)
}

// MAXROOTSIZE is a free data retrieval call binding the contract method 0x16e2bcd5.
//
// Solidity: function MAX_ROOT_SIZE() view returns(uint256)
func (_PDPService *PDPServiceCallerSession) MAXROOTSIZE() (*big.Int, error) {
	return _PDPService.Contract.MAXROOTSIZE(&_PDPService.CallOpts)
}

// FindRootIds is a free data retrieval call binding the contract method 0x0528a55b.
//
// Solidity: function findRootIds(uint256 setId, uint256[] leafIndexs) view returns((uint256,uint256)[])
func (_PDPService *PDPServiceCaller) FindRootIds(opts *bind.CallOpts, setId *big.Int, leafIndexs []*big.Int) ([]PDPServiceRootIdAndOffset, error) {
	var out []interface{}
	err := _PDPService.contract.Call(opts, &out, "findRootIds", setId, leafIndexs)

	if err != nil {
		return *new([]PDPServiceRootIdAndOffset), err
	}

	out0 := *abi.ConvertType(out[0], new([]PDPServiceRootIdAndOffset)).(*[]PDPServiceRootIdAndOffset)

	return out0, err

}

// FindRootIds is a free data retrieval call binding the contract method 0x0528a55b.
//
// Solidity: function findRootIds(uint256 setId, uint256[] leafIndexs) view returns((uint256,uint256)[])
func (_PDPService *PDPServiceSession) FindRootIds(setId *big.Int, leafIndexs []*big.Int) ([]PDPServiceRootIdAndOffset, error) {
	return _PDPService.Contract.FindRootIds(&_PDPService.CallOpts, setId, leafIndexs)
}

// FindRootIds is a free data retrieval call binding the contract method 0x0528a55b.
//
// Solidity: function findRootIds(uint256 setId, uint256[] leafIndexs) view returns((uint256,uint256)[])
func (_PDPService *PDPServiceCallerSession) FindRootIds(setId *big.Int, leafIndexs []*big.Int) ([]PDPServiceRootIdAndOffset, error) {
	return _PDPService.Contract.FindRootIds(&_PDPService.CallOpts, setId, leafIndexs)
}

// GetChallengeFinality is a free data retrieval call binding the contract method 0xf83758fe.
//
// Solidity: function getChallengeFinality() view returns(uint256)
func (_PDPService *PDPServiceCaller) GetChallengeFinality(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _PDPService.contract.Call(opts, &out, "getChallengeFinality")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetChallengeFinality is a free data retrieval call binding the contract method 0xf83758fe.
//
// Solidity: function getChallengeFinality() view returns(uint256)
func (_PDPService *PDPServiceSession) GetChallengeFinality() (*big.Int, error) {
	return _PDPService.Contract.GetChallengeFinality(&_PDPService.CallOpts)
}

// GetChallengeFinality is a free data retrieval call binding the contract method 0xf83758fe.
//
// Solidity: function getChallengeFinality() view returns(uint256)
func (_PDPService *PDPServiceCallerSession) GetChallengeFinality() (*big.Int, error) {
	return _PDPService.Contract.GetChallengeFinality(&_PDPService.CallOpts)
}

// GetNextChallengeEpoch is a free data retrieval call binding the contract method 0x6ba4608f.
//
// Solidity: function getNextChallengeEpoch(uint256 setId) view returns(uint256)
func (_PDPService *PDPServiceCaller) GetNextChallengeEpoch(opts *bind.CallOpts, setId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPService.contract.Call(opts, &out, "getNextChallengeEpoch", setId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetNextChallengeEpoch is a free data retrieval call binding the contract method 0x6ba4608f.
//
// Solidity: function getNextChallengeEpoch(uint256 setId) view returns(uint256)
func (_PDPService *PDPServiceSession) GetNextChallengeEpoch(setId *big.Int) (*big.Int, error) {
	return _PDPService.Contract.GetNextChallengeEpoch(&_PDPService.CallOpts, setId)
}

// GetNextChallengeEpoch is a free data retrieval call binding the contract method 0x6ba4608f.
//
// Solidity: function getNextChallengeEpoch(uint256 setId) view returns(uint256)
func (_PDPService *PDPServiceCallerSession) GetNextChallengeEpoch(setId *big.Int) (*big.Int, error) {
	return _PDPService.Contract.GetNextChallengeEpoch(&_PDPService.CallOpts, setId)
}

// GetNextProofSetId is a free data retrieval call binding the contract method 0x8ea417e5.
//
// Solidity: function getNextProofSetId() view returns(uint64)
func (_PDPService *PDPServiceCaller) GetNextProofSetId(opts *bind.CallOpts) (uint64, error) {
	var out []interface{}
	err := _PDPService.contract.Call(opts, &out, "getNextProofSetId")

	if err != nil {
		return *new(uint64), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)

	return out0, err

}

// GetNextProofSetId is a free data retrieval call binding the contract method 0x8ea417e5.
//
// Solidity: function getNextProofSetId() view returns(uint64)
func (_PDPService *PDPServiceSession) GetNextProofSetId() (uint64, error) {
	return _PDPService.Contract.GetNextProofSetId(&_PDPService.CallOpts)
}

// GetNextProofSetId is a free data retrieval call binding the contract method 0x8ea417e5.
//
// Solidity: function getNextProofSetId() view returns(uint64)
func (_PDPService *PDPServiceCallerSession) GetNextProofSetId() (uint64, error) {
	return _PDPService.Contract.GetNextProofSetId(&_PDPService.CallOpts)
}

// GetNextRootId is a free data retrieval call binding the contract method 0xd49245c1.
//
// Solidity: function getNextRootId(uint256 setId) view returns(uint256)
func (_PDPService *PDPServiceCaller) GetNextRootId(opts *bind.CallOpts, setId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPService.contract.Call(opts, &out, "getNextRootId", setId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetNextRootId is a free data retrieval call binding the contract method 0xd49245c1.
//
// Solidity: function getNextRootId(uint256 setId) view returns(uint256)
func (_PDPService *PDPServiceSession) GetNextRootId(setId *big.Int) (*big.Int, error) {
	return _PDPService.Contract.GetNextRootId(&_PDPService.CallOpts, setId)
}

// GetNextRootId is a free data retrieval call binding the contract method 0xd49245c1.
//
// Solidity: function getNextRootId(uint256 setId) view returns(uint256)
func (_PDPService *PDPServiceCallerSession) GetNextRootId(setId *big.Int) (*big.Int, error) {
	return _PDPService.Contract.GetNextRootId(&_PDPService.CallOpts, setId)
}

// GetProofSetLeafCount is a free data retrieval call binding the contract method 0x3f84135f.
//
// Solidity: function getProofSetLeafCount(uint256 setId) view returns(uint256)
func (_PDPService *PDPServiceCaller) GetProofSetLeafCount(opts *bind.CallOpts, setId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPService.contract.Call(opts, &out, "getProofSetLeafCount", setId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetProofSetLeafCount is a free data retrieval call binding the contract method 0x3f84135f.
//
// Solidity: function getProofSetLeafCount(uint256 setId) view returns(uint256)
func (_PDPService *PDPServiceSession) GetProofSetLeafCount(setId *big.Int) (*big.Int, error) {
	return _PDPService.Contract.GetProofSetLeafCount(&_PDPService.CallOpts, setId)
}

// GetProofSetLeafCount is a free data retrieval call binding the contract method 0x3f84135f.
//
// Solidity: function getProofSetLeafCount(uint256 setId) view returns(uint256)
func (_PDPService *PDPServiceCallerSession) GetProofSetLeafCount(setId *big.Int) (*big.Int, error) {
	return _PDPService.Contract.GetProofSetLeafCount(&_PDPService.CallOpts, setId)
}

// GetProofSetOwner is a free data retrieval call binding the contract method 0x4726075b.
//
// Solidity: function getProofSetOwner(uint256 setId) view returns(address, address)
func (_PDPService *PDPServiceCaller) GetProofSetOwner(opts *bind.CallOpts, setId *big.Int) (common.Address, common.Address, error) {
	var out []interface{}
	err := _PDPService.contract.Call(opts, &out, "getProofSetOwner", setId)

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
func (_PDPService *PDPServiceSession) GetProofSetOwner(setId *big.Int) (common.Address, common.Address, error) {
	return _PDPService.Contract.GetProofSetOwner(&_PDPService.CallOpts, setId)
}

// GetProofSetOwner is a free data retrieval call binding the contract method 0x4726075b.
//
// Solidity: function getProofSetOwner(uint256 setId) view returns(address, address)
func (_PDPService *PDPServiceCallerSession) GetProofSetOwner(setId *big.Int) (common.Address, common.Address, error) {
	return _PDPService.Contract.GetProofSetOwner(&_PDPService.CallOpts, setId)
}

// GetRootCid is a free data retrieval call binding the contract method 0x3b7ae913.
//
// Solidity: function getRootCid(uint256 setId, uint256 rootId) view returns((bytes))
func (_PDPService *PDPServiceCaller) GetRootCid(opts *bind.CallOpts, setId *big.Int, rootId *big.Int) (CidsCid, error) {
	var out []interface{}
	err := _PDPService.contract.Call(opts, &out, "getRootCid", setId, rootId)

	if err != nil {
		return *new(CidsCid), err
	}

	out0 := *abi.ConvertType(out[0], new(CidsCid)).(*CidsCid)

	return out0, err

}

// GetRootCid is a free data retrieval call binding the contract method 0x3b7ae913.
//
// Solidity: function getRootCid(uint256 setId, uint256 rootId) view returns((bytes))
func (_PDPService *PDPServiceSession) GetRootCid(setId *big.Int, rootId *big.Int) (CidsCid, error) {
	return _PDPService.Contract.GetRootCid(&_PDPService.CallOpts, setId, rootId)
}

// GetRootCid is a free data retrieval call binding the contract method 0x3b7ae913.
//
// Solidity: function getRootCid(uint256 setId, uint256 rootId) view returns((bytes))
func (_PDPService *PDPServiceCallerSession) GetRootCid(setId *big.Int, rootId *big.Int) (CidsCid, error) {
	return _PDPService.Contract.GetRootCid(&_PDPService.CallOpts, setId, rootId)
}

// GetRootLeafCount is a free data retrieval call binding the contract method 0x9153e64b.
//
// Solidity: function getRootLeafCount(uint256 setId, uint256 rootId) view returns(uint256)
func (_PDPService *PDPServiceCaller) GetRootLeafCount(opts *bind.CallOpts, setId *big.Int, rootId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPService.contract.Call(opts, &out, "getRootLeafCount", setId, rootId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetRootLeafCount is a free data retrieval call binding the contract method 0x9153e64b.
//
// Solidity: function getRootLeafCount(uint256 setId, uint256 rootId) view returns(uint256)
func (_PDPService *PDPServiceSession) GetRootLeafCount(setId *big.Int, rootId *big.Int) (*big.Int, error) {
	return _PDPService.Contract.GetRootLeafCount(&_PDPService.CallOpts, setId, rootId)
}

// GetRootLeafCount is a free data retrieval call binding the contract method 0x9153e64b.
//
// Solidity: function getRootLeafCount(uint256 setId, uint256 rootId) view returns(uint256)
func (_PDPService *PDPServiceCallerSession) GetRootLeafCount(setId *big.Int, rootId *big.Int) (*big.Int, error) {
	return _PDPService.Contract.GetRootLeafCount(&_PDPService.CallOpts, setId, rootId)
}

// ProofSetLive is a free data retrieval call binding the contract method 0xf5cac1ba.
//
// Solidity: function proofSetLive(uint256 setId) view returns(bool)
func (_PDPService *PDPServiceCaller) ProofSetLive(opts *bind.CallOpts, setId *big.Int) (bool, error) {
	var out []interface{}
	err := _PDPService.contract.Call(opts, &out, "proofSetLive", setId)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// ProofSetLive is a free data retrieval call binding the contract method 0xf5cac1ba.
//
// Solidity: function proofSetLive(uint256 setId) view returns(bool)
func (_PDPService *PDPServiceSession) ProofSetLive(setId *big.Int) (bool, error) {
	return _PDPService.Contract.ProofSetLive(&_PDPService.CallOpts, setId)
}

// ProofSetLive is a free data retrieval call binding the contract method 0xf5cac1ba.
//
// Solidity: function proofSetLive(uint256 setId) view returns(bool)
func (_PDPService *PDPServiceCallerSession) ProofSetLive(setId *big.Int) (bool, error) {
	return _PDPService.Contract.ProofSetLive(&_PDPService.CallOpts, setId)
}

// RootLive is a free data retrieval call binding the contract method 0x47331050.
//
// Solidity: function rootLive(uint256 setId, uint256 rootId) view returns(bool)
func (_PDPService *PDPServiceCaller) RootLive(opts *bind.CallOpts, setId *big.Int, rootId *big.Int) (bool, error) {
	var out []interface{}
	err := _PDPService.contract.Call(opts, &out, "rootLive", setId, rootId)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// RootLive is a free data retrieval call binding the contract method 0x47331050.
//
// Solidity: function rootLive(uint256 setId, uint256 rootId) view returns(bool)
func (_PDPService *PDPServiceSession) RootLive(setId *big.Int, rootId *big.Int) (bool, error) {
	return _PDPService.Contract.RootLive(&_PDPService.CallOpts, setId, rootId)
}

// RootLive is a free data retrieval call binding the contract method 0x47331050.
//
// Solidity: function rootLive(uint256 setId, uint256 rootId) view returns(bool)
func (_PDPService *PDPServiceCallerSession) RootLive(setId *big.Int, rootId *big.Int) (bool, error) {
	return _PDPService.Contract.RootLive(&_PDPService.CallOpts, setId, rootId)
}

// AddRoots is a paid mutator transaction binding the contract method 0x32a87252.
//
// Solidity: function addRoots(uint256 setId, ((bytes),uint256)[] rootData) returns(uint256)
func (_PDPService *PDPServiceTransactor) AddRoots(opts *bind.TransactOpts, setId *big.Int, rootData []PDPServiceRootData) (*types.Transaction, error) {
	return _PDPService.contract.Transact(opts, "addRoots", setId, rootData)
}

// AddRoots is a paid mutator transaction binding the contract method 0x32a87252.
//
// Solidity: function addRoots(uint256 setId, ((bytes),uint256)[] rootData) returns(uint256)
func (_PDPService *PDPServiceSession) AddRoots(setId *big.Int, rootData []PDPServiceRootData) (*types.Transaction, error) {
	return _PDPService.Contract.AddRoots(&_PDPService.TransactOpts, setId, rootData)
}

// AddRoots is a paid mutator transaction binding the contract method 0x32a87252.
//
// Solidity: function addRoots(uint256 setId, ((bytes),uint256)[] rootData) returns(uint256)
func (_PDPService *PDPServiceTransactorSession) AddRoots(setId *big.Int, rootData []PDPServiceRootData) (*types.Transaction, error) {
	return _PDPService.Contract.AddRoots(&_PDPService.TransactOpts, setId, rootData)
}

// ClaimProofSetOwnership is a paid mutator transaction binding the contract method 0xee3dac65.
//
// Solidity: function claimProofSetOwnership(uint256 setId) returns()
func (_PDPService *PDPServiceTransactor) ClaimProofSetOwnership(opts *bind.TransactOpts, setId *big.Int) (*types.Transaction, error) {
	return _PDPService.contract.Transact(opts, "claimProofSetOwnership", setId)
}

// ClaimProofSetOwnership is a paid mutator transaction binding the contract method 0xee3dac65.
//
// Solidity: function claimProofSetOwnership(uint256 setId) returns()
func (_PDPService *PDPServiceSession) ClaimProofSetOwnership(setId *big.Int) (*types.Transaction, error) {
	return _PDPService.Contract.ClaimProofSetOwnership(&_PDPService.TransactOpts, setId)
}

// ClaimProofSetOwnership is a paid mutator transaction binding the contract method 0xee3dac65.
//
// Solidity: function claimProofSetOwnership(uint256 setId) returns()
func (_PDPService *PDPServiceTransactorSession) ClaimProofSetOwnership(setId *big.Int) (*types.Transaction, error) {
	return _PDPService.Contract.ClaimProofSetOwnership(&_PDPService.TransactOpts, setId)
}

// CreateProofSet is a paid mutator transaction binding the contract method 0x96984338.
//
// Solidity: function createProofSet(address recordKeeper) returns(uint256)
func (_PDPService *PDPServiceTransactor) CreateProofSet(opts *bind.TransactOpts, recordKeeper common.Address) (*types.Transaction, error) {
	return _PDPService.contract.Transact(opts, "createProofSet", recordKeeper)
}

// CreateProofSet is a paid mutator transaction binding the contract method 0x96984338.
//
// Solidity: function createProofSet(address recordKeeper) returns(uint256)
func (_PDPService *PDPServiceSession) CreateProofSet(recordKeeper common.Address) (*types.Transaction, error) {
	return _PDPService.Contract.CreateProofSet(&_PDPService.TransactOpts, recordKeeper)
}

// CreateProofSet is a paid mutator transaction binding the contract method 0x96984338.
//
// Solidity: function createProofSet(address recordKeeper) returns(uint256)
func (_PDPService *PDPServiceTransactorSession) CreateProofSet(recordKeeper common.Address) (*types.Transaction, error) {
	return _PDPService.Contract.CreateProofSet(&_PDPService.TransactOpts, recordKeeper)
}

// DeleteProofSet is a paid mutator transaction binding the contract method 0xd6dadec8.
//
// Solidity: function deleteProofSet(uint256 setId) returns()
func (_PDPService *PDPServiceTransactor) DeleteProofSet(opts *bind.TransactOpts, setId *big.Int) (*types.Transaction, error) {
	return _PDPService.contract.Transact(opts, "deleteProofSet", setId)
}

// DeleteProofSet is a paid mutator transaction binding the contract method 0xd6dadec8.
//
// Solidity: function deleteProofSet(uint256 setId) returns()
func (_PDPService *PDPServiceSession) DeleteProofSet(setId *big.Int) (*types.Transaction, error) {
	return _PDPService.Contract.DeleteProofSet(&_PDPService.TransactOpts, setId)
}

// DeleteProofSet is a paid mutator transaction binding the contract method 0xd6dadec8.
//
// Solidity: function deleteProofSet(uint256 setId) returns()
func (_PDPService *PDPServiceTransactorSession) DeleteProofSet(setId *big.Int) (*types.Transaction, error) {
	return _PDPService.Contract.DeleteProofSet(&_PDPService.TransactOpts, setId)
}

// ProposeProofSetOwner is a paid mutator transaction binding the contract method 0x6cb55c16.
//
// Solidity: function proposeProofSetOwner(uint256 setId, address newOwner) returns()
func (_PDPService *PDPServiceTransactor) ProposeProofSetOwner(opts *bind.TransactOpts, setId *big.Int, newOwner common.Address) (*types.Transaction, error) {
	return _PDPService.contract.Transact(opts, "proposeProofSetOwner", setId, newOwner)
}

// ProposeProofSetOwner is a paid mutator transaction binding the contract method 0x6cb55c16.
//
// Solidity: function proposeProofSetOwner(uint256 setId, address newOwner) returns()
func (_PDPService *PDPServiceSession) ProposeProofSetOwner(setId *big.Int, newOwner common.Address) (*types.Transaction, error) {
	return _PDPService.Contract.ProposeProofSetOwner(&_PDPService.TransactOpts, setId, newOwner)
}

// ProposeProofSetOwner is a paid mutator transaction binding the contract method 0x6cb55c16.
//
// Solidity: function proposeProofSetOwner(uint256 setId, address newOwner) returns()
func (_PDPService *PDPServiceTransactorSession) ProposeProofSetOwner(setId *big.Int, newOwner common.Address) (*types.Transaction, error) {
	return _PDPService.Contract.ProposeProofSetOwner(&_PDPService.TransactOpts, setId, newOwner)
}

// ProvePossession is a paid mutator transaction binding the contract method 0xf58f952b.
//
// Solidity: function provePossession(uint256 setId, (bytes32,bytes32[])[] proofs) returns()
func (_PDPService *PDPServiceTransactor) ProvePossession(opts *bind.TransactOpts, setId *big.Int, proofs []PDPServiceProof) (*types.Transaction, error) {
	return _PDPService.contract.Transact(opts, "provePossession", setId, proofs)
}

// ProvePossession is a paid mutator transaction binding the contract method 0xf58f952b.
//
// Solidity: function provePossession(uint256 setId, (bytes32,bytes32[])[] proofs) returns()
func (_PDPService *PDPServiceSession) ProvePossession(setId *big.Int, proofs []PDPServiceProof) (*types.Transaction, error) {
	return _PDPService.Contract.ProvePossession(&_PDPService.TransactOpts, setId, proofs)
}

// ProvePossession is a paid mutator transaction binding the contract method 0xf58f952b.
//
// Solidity: function provePossession(uint256 setId, (bytes32,bytes32[])[] proofs) returns()
func (_PDPService *PDPServiceTransactorSession) ProvePossession(setId *big.Int, proofs []PDPServiceProof) (*types.Transaction, error) {
	return _PDPService.Contract.ProvePossession(&_PDPService.TransactOpts, setId, proofs)
}

// RemoveRoots is a paid mutator transaction binding the contract method 0x316bf10e.
//
// Solidity: function removeRoots(uint256 setId, uint256[] rootIds) returns(uint256)
func (_PDPService *PDPServiceTransactor) RemoveRoots(opts *bind.TransactOpts, setId *big.Int, rootIds []*big.Int) (*types.Transaction, error) {
	return _PDPService.contract.Transact(opts, "removeRoots", setId, rootIds)
}

// RemoveRoots is a paid mutator transaction binding the contract method 0x316bf10e.
//
// Solidity: function removeRoots(uint256 setId, uint256[] rootIds) returns(uint256)
func (_PDPService *PDPServiceSession) RemoveRoots(setId *big.Int, rootIds []*big.Int) (*types.Transaction, error) {
	return _PDPService.Contract.RemoveRoots(&_PDPService.TransactOpts, setId, rootIds)
}

// RemoveRoots is a paid mutator transaction binding the contract method 0x316bf10e.
//
// Solidity: function removeRoots(uint256 setId, uint256[] rootIds) returns(uint256)
func (_PDPService *PDPServiceTransactorSession) RemoveRoots(setId *big.Int, rootIds []*big.Int) (*types.Transaction, error) {
	return _PDPService.Contract.RemoveRoots(&_PDPService.TransactOpts, setId, rootIds)
}

// PDPServiceProofSetCreatedIterator is returned from FilterProofSetCreated and is used to iterate over the raw logs and unpacked data for ProofSetCreated events raised by the PDPService contract.
type PDPServiceProofSetCreatedIterator struct {
	Event *PDPServiceProofSetCreated // Event containing the contract specifics and raw log

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
func (it *PDPServiceProofSetCreatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPServiceProofSetCreated)
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
		it.Event = new(PDPServiceProofSetCreated)
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
func (it *PDPServiceProofSetCreatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPServiceProofSetCreatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPServiceProofSetCreated represents a ProofSetCreated event raised by the PDPService contract.
type PDPServiceProofSetCreated struct {
	SetId *big.Int
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterProofSetCreated is a free log retrieval operation binding the contract event 0x5979d495e336598dba8459e44f8eb2a1c957ce30fcc10cabea4bb0ffe969df6a.
//
// Solidity: event ProofSetCreated(uint256 indexed setId)
func (_PDPService *PDPServiceFilterer) FilterProofSetCreated(opts *bind.FilterOpts, setId []*big.Int) (*PDPServiceProofSetCreatedIterator, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPService.contract.FilterLogs(opts, "ProofSetCreated", setIdRule)
	if err != nil {
		return nil, err
	}
	return &PDPServiceProofSetCreatedIterator{contract: _PDPService.contract, event: "ProofSetCreated", logs: logs, sub: sub}, nil
}

// WatchProofSetCreated is a free log subscription operation binding the contract event 0x5979d495e336598dba8459e44f8eb2a1c957ce30fcc10cabea4bb0ffe969df6a.
//
// Solidity: event ProofSetCreated(uint256 indexed setId)
func (_PDPService *PDPServiceFilterer) WatchProofSetCreated(opts *bind.WatchOpts, sink chan<- *PDPServiceProofSetCreated, setId []*big.Int) (event.Subscription, error) {

	var setIdRule []interface{}
	for _, setIdItem := range setId {
		setIdRule = append(setIdRule, setIdItem)
	}

	logs, sub, err := _PDPService.contract.WatchLogs(opts, "ProofSetCreated", setIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPServiceProofSetCreated)
				if err := _PDPService.contract.UnpackLog(event, "ProofSetCreated", log); err != nil {
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
func (_PDPService *PDPServiceFilterer) ParseProofSetCreated(log types.Log) (*PDPServiceProofSetCreated, error) {
	event := new(PDPServiceProofSetCreated)
	if err := _PDPService.contract.UnpackLog(event, "ProofSetCreated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPServiceRootsAddedIterator is returned from FilterRootsAdded and is used to iterate over the raw logs and unpacked data for RootsAdded events raised by the PDPService contract.
type PDPServiceRootsAddedIterator struct {
	Event *PDPServiceRootsAdded // Event containing the contract specifics and raw log

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
func (it *PDPServiceRootsAddedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPServiceRootsAdded)
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
		it.Event = new(PDPServiceRootsAdded)
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
func (it *PDPServiceRootsAddedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPServiceRootsAddedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPServiceRootsAdded represents a RootsAdded event raised by the PDPService contract.
type PDPServiceRootsAdded struct {
	FirstAdded *big.Int
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterRootsAdded is a free log retrieval operation binding the contract event 0xde16f8a35fc44238b21a493b7267f7a5064f2bc1b3a1af51deeb4b5a47281d47.
//
// Solidity: event RootsAdded(uint256 indexed firstAdded)
func (_PDPService *PDPServiceFilterer) FilterRootsAdded(opts *bind.FilterOpts, firstAdded []*big.Int) (*PDPServiceRootsAddedIterator, error) {

	var firstAddedRule []interface{}
	for _, firstAddedItem := range firstAdded {
		firstAddedRule = append(firstAddedRule, firstAddedItem)
	}

	logs, sub, err := _PDPService.contract.FilterLogs(opts, "RootsAdded", firstAddedRule)
	if err != nil {
		return nil, err
	}
	return &PDPServiceRootsAddedIterator{contract: _PDPService.contract, event: "RootsAdded", logs: logs, sub: sub}, nil
}

// WatchRootsAdded is a free log subscription operation binding the contract event 0xde16f8a35fc44238b21a493b7267f7a5064f2bc1b3a1af51deeb4b5a47281d47.
//
// Solidity: event RootsAdded(uint256 indexed firstAdded)
func (_PDPService *PDPServiceFilterer) WatchRootsAdded(opts *bind.WatchOpts, sink chan<- *PDPServiceRootsAdded, firstAdded []*big.Int) (event.Subscription, error) {

	var firstAddedRule []interface{}
	for _, firstAddedItem := range firstAdded {
		firstAddedRule = append(firstAddedRule, firstAddedItem)
	}

	logs, sub, err := _PDPService.contract.WatchLogs(opts, "RootsAdded", firstAddedRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPServiceRootsAdded)
				if err := _PDPService.contract.UnpackLog(event, "RootsAdded", log); err != nil {
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
func (_PDPService *PDPServiceFilterer) ParseRootsAdded(log types.Log) (*PDPServiceRootsAdded, error) {
	event := new(PDPServiceRootsAdded)
	if err := _PDPService.contract.UnpackLog(event, "RootsAdded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PDPServiceRootsRemovedIterator is returned from FilterRootsRemoved and is used to iterate over the raw logs and unpacked data for RootsRemoved events raised by the PDPService contract.
type PDPServiceRootsRemovedIterator struct {
	Event *PDPServiceRootsRemoved // Event containing the contract specifics and raw log

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
func (it *PDPServiceRootsRemovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPServiceRootsRemoved)
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
		it.Event = new(PDPServiceRootsRemoved)
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
func (it *PDPServiceRootsRemovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPServiceRootsRemovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPServiceRootsRemoved represents a RootsRemoved event raised by the PDPService contract.
type PDPServiceRootsRemoved struct {
	TotalDelta *big.Int
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterRootsRemoved is a free log retrieval operation binding the contract event 0xc104a368a3d37b85a4936902c8de9d9efc075994124c76c0173e370bc244cda1.
//
// Solidity: event RootsRemoved(uint256 indexed totalDelta)
func (_PDPService *PDPServiceFilterer) FilterRootsRemoved(opts *bind.FilterOpts, totalDelta []*big.Int) (*PDPServiceRootsRemovedIterator, error) {

	var totalDeltaRule []interface{}
	for _, totalDeltaItem := range totalDelta {
		totalDeltaRule = append(totalDeltaRule, totalDeltaItem)
	}

	logs, sub, err := _PDPService.contract.FilterLogs(opts, "RootsRemoved", totalDeltaRule)
	if err != nil {
		return nil, err
	}
	return &PDPServiceRootsRemovedIterator{contract: _PDPService.contract, event: "RootsRemoved", logs: logs, sub: sub}, nil
}

// WatchRootsRemoved is a free log subscription operation binding the contract event 0xc104a368a3d37b85a4936902c8de9d9efc075994124c76c0173e370bc244cda1.
//
// Solidity: event RootsRemoved(uint256 indexed totalDelta)
func (_PDPService *PDPServiceFilterer) WatchRootsRemoved(opts *bind.WatchOpts, sink chan<- *PDPServiceRootsRemoved, totalDelta []*big.Int) (event.Subscription, error) {

	var totalDeltaRule []interface{}
	for _, totalDeltaItem := range totalDelta {
		totalDeltaRule = append(totalDeltaRule, totalDeltaItem)
	}

	logs, sub, err := _PDPService.contract.WatchLogs(opts, "RootsRemoved", totalDeltaRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPServiceRootsRemoved)
				if err := _PDPService.contract.UnpackLog(event, "RootsRemoved", log); err != nil {
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

// ParseRootsRemoved is a log parse operation binding the contract event 0xc104a368a3d37b85a4936902c8de9d9efc075994124c76c0173e370bc244cda1.
//
// Solidity: event RootsRemoved(uint256 indexed totalDelta)
func (_PDPService *PDPServiceFilterer) ParseRootsRemoved(log types.Log) (*PDPServiceRootsRemoved, error) {
	event := new(PDPServiceRootsRemoved)
	if err := _PDPService.contract.UnpackLog(event, "RootsRemoved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
