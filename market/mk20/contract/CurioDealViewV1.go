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

// ICurioDealViewV1DealView is an auto generated low-level Go binding around an user-defined struct.
type ICurioDealViewV1DealView struct {
	State           uint8
	ProviderActorId *big.Int
	ClientId        []byte
	PieceCidV2      []byte
	StartEpoch      *big.Int
	Duration        *big.Int
	AllocationId    *big.Int
	FinalizedEpoch  *big.Int
}

// CurioDealViewV1MetaData contains all meta data concerning the CurioDealViewV1 contract.
var CurioDealViewV1MetaData = &bind.MetaData{
	ABI: "[{\"type\":\"function\",\"name\":\"getDeal\",\"inputs\":[{\"name\":\"dealId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"tuple\",\"internalType\":\"structICurioDealViewV1.DealView\",\"components\":[{\"name\":\"state\",\"type\":\"uint8\",\"internalType\":\"enumICurioDealViewV1.DealState\"},{\"name\":\"providerActorId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"clientId\",\"type\":\"bytes\",\"internalType\":\"bytes\"},{\"name\":\"pieceCidV2\",\"type\":\"bytes\",\"internalType\":\"bytes\"},{\"name\":\"startEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"duration\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"allocationId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"finalizedEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"version\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"pure\"},{\"type\":\"error\",\"name\":\"DealNotFound\",\"inputs\":[{\"name\":\"dealId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}]",
}

// CurioDealViewV1ABI is the input ABI used to generate the binding from.
// Deprecated: Use CurioDealViewV1MetaData.ABI instead.
var CurioDealViewV1ABI = CurioDealViewV1MetaData.ABI

// CurioDealViewV1 is an auto generated Go binding around an Ethereum contract.
type CurioDealViewV1 struct {
	CurioDealViewV1Caller     // Read-only binding to the contract
	CurioDealViewV1Transactor // Write-only binding to the contract
	CurioDealViewV1Filterer   // Log filterer for contract events
}

// CurioDealViewV1Caller is an auto generated read-only Go binding around an Ethereum contract.
type CurioDealViewV1Caller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// CurioDealViewV1Transactor is an auto generated write-only Go binding around an Ethereum contract.
type CurioDealViewV1Transactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// CurioDealViewV1Filterer is an auto generated log filtering Go binding around an Ethereum contract events.
type CurioDealViewV1Filterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// CurioDealViewV1Session is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type CurioDealViewV1Session struct {
	Contract     *CurioDealViewV1  // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// CurioDealViewV1CallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type CurioDealViewV1CallerSession struct {
	Contract *CurioDealViewV1Caller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts          // Call options to use throughout this session
}

// CurioDealViewV1TransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type CurioDealViewV1TransactorSession struct {
	Contract     *CurioDealViewV1Transactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts          // Transaction auth options to use throughout this session
}

// CurioDealViewV1Raw is an auto generated low-level Go binding around an Ethereum contract.
type CurioDealViewV1Raw struct {
	Contract *CurioDealViewV1 // Generic contract binding to access the raw methods on
}

// CurioDealViewV1CallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type CurioDealViewV1CallerRaw struct {
	Contract *CurioDealViewV1Caller // Generic read-only contract binding to access the raw methods on
}

// CurioDealViewV1TransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type CurioDealViewV1TransactorRaw struct {
	Contract *CurioDealViewV1Transactor // Generic write-only contract binding to access the raw methods on
}

// NewCurioDealViewV1 creates a new instance of CurioDealViewV1, bound to a specific deployed contract.
func NewCurioDealViewV1(address common.Address, backend bind.ContractBackend) (*CurioDealViewV1, error) {
	contract, err := bindCurioDealViewV1(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &CurioDealViewV1{CurioDealViewV1Caller: CurioDealViewV1Caller{contract: contract}, CurioDealViewV1Transactor: CurioDealViewV1Transactor{contract: contract}, CurioDealViewV1Filterer: CurioDealViewV1Filterer{contract: contract}}, nil
}

// NewCurioDealViewV1Caller creates a new read-only instance of CurioDealViewV1, bound to a specific deployed contract.
func NewCurioDealViewV1Caller(address common.Address, caller bind.ContractCaller) (*CurioDealViewV1Caller, error) {
	contract, err := bindCurioDealViewV1(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &CurioDealViewV1Caller{contract: contract}, nil
}

// NewCurioDealViewV1Transactor creates a new write-only instance of CurioDealViewV1, bound to a specific deployed contract.
func NewCurioDealViewV1Transactor(address common.Address, transactor bind.ContractTransactor) (*CurioDealViewV1Transactor, error) {
	contract, err := bindCurioDealViewV1(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &CurioDealViewV1Transactor{contract: contract}, nil
}

// NewCurioDealViewV1Filterer creates a new log filterer instance of CurioDealViewV1, bound to a specific deployed contract.
func NewCurioDealViewV1Filterer(address common.Address, filterer bind.ContractFilterer) (*CurioDealViewV1Filterer, error) {
	contract, err := bindCurioDealViewV1(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &CurioDealViewV1Filterer{contract: contract}, nil
}

// bindCurioDealViewV1 binds a generic wrapper to an already deployed contract.
func bindCurioDealViewV1(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := CurioDealViewV1MetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_CurioDealViewV1 *CurioDealViewV1Raw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _CurioDealViewV1.Contract.CurioDealViewV1Caller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_CurioDealViewV1 *CurioDealViewV1Raw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _CurioDealViewV1.Contract.CurioDealViewV1Transactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_CurioDealViewV1 *CurioDealViewV1Raw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _CurioDealViewV1.Contract.CurioDealViewV1Transactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_CurioDealViewV1 *CurioDealViewV1CallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _CurioDealViewV1.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_CurioDealViewV1 *CurioDealViewV1TransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _CurioDealViewV1.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_CurioDealViewV1 *CurioDealViewV1TransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _CurioDealViewV1.Contract.contract.Transact(opts, method, params...)
}

// GetDeal is a free data retrieval call binding the contract method 0x82fd5bac.
//
// Solidity: function getDeal(uint256 dealId) view returns((uint8,uint256,bytes,bytes,uint256,uint256,uint256,uint256))
func (_CurioDealViewV1 *CurioDealViewV1Caller) GetDeal(opts *bind.CallOpts, dealId *big.Int) (ICurioDealViewV1DealView, error) {
	var out []interface{}
	err := _CurioDealViewV1.contract.Call(opts, &out, "getDeal", dealId)

	if err != nil {
		return *new(ICurioDealViewV1DealView), err
	}

	out0 := *abi.ConvertType(out[0], new(ICurioDealViewV1DealView)).(*ICurioDealViewV1DealView)

	return out0, err

}

// GetDeal is a free data retrieval call binding the contract method 0x82fd5bac.
//
// Solidity: function getDeal(uint256 dealId) view returns((uint8,uint256,bytes,bytes,uint256,uint256,uint256,uint256))
func (_CurioDealViewV1 *CurioDealViewV1Session) GetDeal(dealId *big.Int) (ICurioDealViewV1DealView, error) {
	return _CurioDealViewV1.Contract.GetDeal(&_CurioDealViewV1.CallOpts, dealId)
}

// GetDeal is a free data retrieval call binding the contract method 0x82fd5bac.
//
// Solidity: function getDeal(uint256 dealId) view returns((uint8,uint256,bytes,bytes,uint256,uint256,uint256,uint256))
func (_CurioDealViewV1 *CurioDealViewV1CallerSession) GetDeal(dealId *big.Int) (ICurioDealViewV1DealView, error) {
	return _CurioDealViewV1.Contract.GetDeal(&_CurioDealViewV1.CallOpts, dealId)
}

// Version is a free data retrieval call binding the contract method 0x54fd4d50.
//
// Solidity: function version() pure returns(uint256)
func (_CurioDealViewV1 *CurioDealViewV1Caller) Version(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _CurioDealViewV1.contract.Call(opts, &out, "version")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Version is a free data retrieval call binding the contract method 0x54fd4d50.
//
// Solidity: function version() pure returns(uint256)
func (_CurioDealViewV1 *CurioDealViewV1Session) Version() (*big.Int, error) {
	return _CurioDealViewV1.Contract.Version(&_CurioDealViewV1.CallOpts)
}

// Version is a free data retrieval call binding the contract method 0x54fd4d50.
//
// Solidity: function version() pure returns(uint256)
func (_CurioDealViewV1 *CurioDealViewV1CallerSession) Version() (*big.Int, error) {
	return _CurioDealViewV1.Contract.Version(&_CurioDealViewV1.CallOpts)
}
