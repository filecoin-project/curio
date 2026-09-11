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

// ICurioDealViewV2CurioDealView is an auto generated low-level Go binding around an user-defined struct.
type ICurioDealViewV2CurioDealView struct {
	DealId          *big.Int
	State           uint8
	ProviderActorId *big.Int
	ClientId        []byte
	PieceCidV2      []byte
	StartEpoch      *big.Int
	Duration        *big.Int
	FinalizedEpoch  *big.Int
}

// CurioDealViewV2MetaData contains all meta data concerning the CurioDealViewV2 contract.
var CurioDealViewV2MetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"dealId\",\"type\":\"uint256\"}],\"name\":\"DealNotFound\",\"type\":\"error\"},{\"inputs\":[{\"components\":[{\"internalType\":\"uint256\",\"name\":\"dealId\",\"type\":\"uint256\"},{\"internalType\":\"enumICurioDealViewV2.DealState\",\"name\":\"state\",\"type\":\"uint8\"},{\"internalType\":\"uint256\",\"name\":\"providerActorId\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"clientId\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"pieceCidV2\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"startEpoch\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"duration\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"finalizedEpoch\",\"type\":\"uint256\"}],\"internalType\":\"structICurioDealViewV2.CurioDealView\",\"name\":\"deal\",\"type\":\"tuple\"}],\"name\":\"verifyDeal\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"version\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"dealId\",\"type\":\"uint256\"}],\"name\":\"getDealState\",\"outputs\":[{\"internalType\":\"enumICurioDealViewV2.DealState\",\"name\":\"\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
}

// CurioDealViewV2ABI is the input ABI used to generate the binding from.
// Deprecated: Use CurioDealViewV2MetaData.ABI instead.
var CurioDealViewV2ABI = CurioDealViewV2MetaData.ABI

// CurioDealViewV2 is an auto generated Go binding around an Ethereum contract.
type CurioDealViewV2 struct {
	CurioDealViewV2Caller     // Read-only binding to the contract
	CurioDealViewV2Transactor // Write-only binding to the contract
	CurioDealViewV2Filterer   // Log filterer for contract events
}

// CurioDealViewV2Caller is an auto generated read-only Go binding around an Ethereum contract.
type CurioDealViewV2Caller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// CurioDealViewV2Transactor is an auto generated write-only Go binding around an Ethereum contract.
type CurioDealViewV2Transactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// CurioDealViewV2Filterer is an auto generated log filtering Go binding around an Ethereum contract events.
type CurioDealViewV2Filterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// CurioDealViewV2Session is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type CurioDealViewV2Session struct {
	Contract     *CurioDealViewV2  // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// CurioDealViewV2CallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type CurioDealViewV2CallerSession struct {
	Contract *CurioDealViewV2Caller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts          // Call options to use throughout this session
}

// CurioDealViewV2TransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type CurioDealViewV2TransactorSession struct {
	Contract     *CurioDealViewV2Transactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts          // Transaction auth options to use throughout this session
}

// CurioDealViewV2Raw is an auto generated low-level Go binding around an Ethereum contract.
type CurioDealViewV2Raw struct {
	Contract *CurioDealViewV2 // Generic contract binding to access the raw methods on
}

// CurioDealViewV2CallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type CurioDealViewV2CallerRaw struct {
	Contract *CurioDealViewV2Caller // Generic read-only contract binding to access the raw methods on
}

// CurioDealViewV2TransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type CurioDealViewV2TransactorRaw struct {
	Contract *CurioDealViewV2Transactor // Generic write-only contract binding to access the raw methods on
}

// NewCurioDealViewV2 creates a new instance of CurioDealViewV2, bound to a specific deployed contract.
func NewCurioDealViewV2(address common.Address, backend bind.ContractBackend) (*CurioDealViewV2, error) {
	contract, err := bindCurioDealViewV2(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &CurioDealViewV2{CurioDealViewV2Caller: CurioDealViewV2Caller{contract: contract}, CurioDealViewV2Transactor: CurioDealViewV2Transactor{contract: contract}, CurioDealViewV2Filterer: CurioDealViewV2Filterer{contract: contract}}, nil
}

// NewCurioDealViewV2Caller creates a new read-only instance of CurioDealViewV2, bound to a specific deployed contract.
func NewCurioDealViewV2Caller(address common.Address, caller bind.ContractCaller) (*CurioDealViewV2Caller, error) {
	contract, err := bindCurioDealViewV2(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &CurioDealViewV2Caller{contract: contract}, nil
}

// NewCurioDealViewV2Transactor creates a new write-only instance of CurioDealViewV2, bound to a specific deployed contract.
func NewCurioDealViewV2Transactor(address common.Address, transactor bind.ContractTransactor) (*CurioDealViewV2Transactor, error) {
	contract, err := bindCurioDealViewV2(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &CurioDealViewV2Transactor{contract: contract}, nil
}

// NewCurioDealViewV2Filterer creates a new log filterer instance of CurioDealViewV2, bound to a specific deployed contract.
func NewCurioDealViewV2Filterer(address common.Address, filterer bind.ContractFilterer) (*CurioDealViewV2Filterer, error) {
	contract, err := bindCurioDealViewV2(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &CurioDealViewV2Filterer{contract: contract}, nil
}

// bindCurioDealViewV2 binds a generic wrapper to an already deployed contract.
func bindCurioDealViewV2(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := CurioDealViewV2MetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_CurioDealViewV2 *CurioDealViewV2Raw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _CurioDealViewV2.Contract.CurioDealViewV2Caller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_CurioDealViewV2 *CurioDealViewV2Raw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _CurioDealViewV2.Contract.CurioDealViewV2Transactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_CurioDealViewV2 *CurioDealViewV2Raw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _CurioDealViewV2.Contract.CurioDealViewV2Transactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_CurioDealViewV2 *CurioDealViewV2CallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _CurioDealViewV2.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_CurioDealViewV2 *CurioDealViewV2TransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _CurioDealViewV2.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_CurioDealViewV2 *CurioDealViewV2TransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _CurioDealViewV2.Contract.contract.Transact(opts, method, params...)
}

// GetDealState is a free data retrieval call binding the contract method 0xee84880d.
//
// Solidity: function getDealState(uint256 dealId) view returns(uint8)
func (_CurioDealViewV2 *CurioDealViewV2Caller) GetDealState(opts *bind.CallOpts, dealId *big.Int) (uint8, error) {
	var out []interface{}
	err := _CurioDealViewV2.contract.Call(opts, &out, "getDealState", dealId)

	if err != nil {
		return *new(uint8), err
	}

	out0 := *abi.ConvertType(out[0], new(uint8)).(*uint8)

	return out0, err

}

// GetDealState is a free data retrieval call binding the contract method 0xee84880d.
//
// Solidity: function getDealState(uint256 dealId) view returns(uint8)
func (_CurioDealViewV2 *CurioDealViewV2Session) GetDealState(dealId *big.Int) (uint8, error) {
	return _CurioDealViewV2.Contract.GetDealState(&_CurioDealViewV2.CallOpts, dealId)
}

// GetDealState is a free data retrieval call binding the contract method 0xee84880d.
//
// Solidity: function getDealState(uint256 dealId) view returns(uint8)
func (_CurioDealViewV2 *CurioDealViewV2CallerSession) GetDealState(dealId *big.Int) (uint8, error) {
	return _CurioDealViewV2.Contract.GetDealState(&_CurioDealViewV2.CallOpts, dealId)
}

// VerifyDeal is a free data retrieval call binding the contract method 0x3f7fa154.
//
// Solidity: function verifyDeal((uint256,uint8,uint256,bytes,bytes,uint256,uint256,uint256) deal) view returns(bool)
func (_CurioDealViewV2 *CurioDealViewV2Caller) VerifyDeal(opts *bind.CallOpts, deal ICurioDealViewV2CurioDealView) (bool, error) {
	var out []interface{}
	err := _CurioDealViewV2.contract.Call(opts, &out, "verifyDeal", deal)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// VerifyDeal is a free data retrieval call binding the contract method 0x3f7fa154.
//
// Solidity: function verifyDeal((uint256,uint8,uint256,bytes,bytes,uint256,uint256,uint256) deal) view returns(bool)
func (_CurioDealViewV2 *CurioDealViewV2Session) VerifyDeal(deal ICurioDealViewV2CurioDealView) (bool, error) {
	return _CurioDealViewV2.Contract.VerifyDeal(&_CurioDealViewV2.CallOpts, deal)
}

// VerifyDeal is a free data retrieval call binding the contract method 0x3f7fa154.
//
// Solidity: function verifyDeal((uint256,uint8,uint256,bytes,bytes,uint256,uint256,uint256) deal) view returns(bool)
func (_CurioDealViewV2 *CurioDealViewV2CallerSession) VerifyDeal(deal ICurioDealViewV2CurioDealView) (bool, error) {
	return _CurioDealViewV2.Contract.VerifyDeal(&_CurioDealViewV2.CallOpts, deal)
}

// Version is a free data retrieval call binding the contract method 0x54fd4d50.
//
// Solidity: function version() pure returns(uint256)
func (_CurioDealViewV2 *CurioDealViewV2Caller) Version(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _CurioDealViewV2.contract.Call(opts, &out, "version")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Version is a free data retrieval call binding the contract method 0x54fd4d50.
//
// Solidity: function version() pure returns(uint256)
func (_CurioDealViewV2 *CurioDealViewV2Session) Version() (*big.Int, error) {
	return _CurioDealViewV2.Contract.Version(&_CurioDealViewV2.CallOpts)
}

// Version is a free data retrieval call binding the contract method 0x54fd4d50.
//
// Solidity: function version() pure returns(uint256)
func (_CurioDealViewV2 *CurioDealViewV2CallerSession) Version() (*big.Int, error) {
	return _CurioDealViewV2.Contract.Version(&_CurioDealViewV2.CallOpts)
}
