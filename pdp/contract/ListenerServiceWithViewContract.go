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

// ListenerServiceWithViewContractMetaData contains all meta data concerning the ListenerServiceWithViewContract contract.
var ListenerServiceWithViewContractMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"function\",\"name\":\"viewContractAddress\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"}]",
}

// ListenerServiceWithViewContractABI is the input ABI used to generate the binding from.
// Deprecated: Use ListenerServiceWithViewContractMetaData.ABI instead.
var ListenerServiceWithViewContractABI = ListenerServiceWithViewContractMetaData.ABI

// ListenerServiceWithViewContract is an auto generated Go binding around an Ethereum contract.
type ListenerServiceWithViewContract struct {
	ListenerServiceWithViewContractCaller     // Read-only binding to the contract
	ListenerServiceWithViewContractTransactor // Write-only binding to the contract
	ListenerServiceWithViewContractFilterer   // Log filterer for contract events
}

// ListenerServiceWithViewContractCaller is an auto generated read-only Go binding around an Ethereum contract.
type ListenerServiceWithViewContractCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ListenerServiceWithViewContractTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ListenerServiceWithViewContractTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ListenerServiceWithViewContractFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ListenerServiceWithViewContractFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ListenerServiceWithViewContractSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ListenerServiceWithViewContractSession struct {
	Contract     *ListenerServiceWithViewContract // Generic contract binding to set the session for
	CallOpts     bind.CallOpts                    // Call options to use throughout this session
	TransactOpts bind.TransactOpts                // Transaction auth options to use throughout this session
}

// ListenerServiceWithViewContractCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ListenerServiceWithViewContractCallerSession struct {
	Contract *ListenerServiceWithViewContractCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts                          // Call options to use throughout this session
}

// ListenerServiceWithViewContractTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ListenerServiceWithViewContractTransactorSession struct {
	Contract     *ListenerServiceWithViewContractTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts                          // Transaction auth options to use throughout this session
}

// ListenerServiceWithViewContractRaw is an auto generated low-level Go binding around an Ethereum contract.
type ListenerServiceWithViewContractRaw struct {
	Contract *ListenerServiceWithViewContract // Generic contract binding to access the raw methods on
}

// ListenerServiceWithViewContractCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ListenerServiceWithViewContractCallerRaw struct {
	Contract *ListenerServiceWithViewContractCaller // Generic read-only contract binding to access the raw methods on
}

// ListenerServiceWithViewContractTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ListenerServiceWithViewContractTransactorRaw struct {
	Contract *ListenerServiceWithViewContractTransactor // Generic write-only contract binding to access the raw methods on
}

// NewListenerServiceWithViewContract creates a new instance of ListenerServiceWithViewContract, bound to a specific deployed contract.
func NewListenerServiceWithViewContract(address common.Address, backend bind.ContractBackend) (*ListenerServiceWithViewContract, error) {
	contract, err := bindListenerServiceWithViewContract(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ListenerServiceWithViewContract{ListenerServiceWithViewContractCaller: ListenerServiceWithViewContractCaller{contract: contract}, ListenerServiceWithViewContractTransactor: ListenerServiceWithViewContractTransactor{contract: contract}, ListenerServiceWithViewContractFilterer: ListenerServiceWithViewContractFilterer{contract: contract}}, nil
}

// NewListenerServiceWithViewContractCaller creates a new read-only instance of ListenerServiceWithViewContract, bound to a specific deployed contract.
func NewListenerServiceWithViewContractCaller(address common.Address, caller bind.ContractCaller) (*ListenerServiceWithViewContractCaller, error) {
	contract, err := bindListenerServiceWithViewContract(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ListenerServiceWithViewContractCaller{contract: contract}, nil
}

// NewListenerServiceWithViewContractTransactor creates a new write-only instance of ListenerServiceWithViewContract, bound to a specific deployed contract.
func NewListenerServiceWithViewContractTransactor(address common.Address, transactor bind.ContractTransactor) (*ListenerServiceWithViewContractTransactor, error) {
	contract, err := bindListenerServiceWithViewContract(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ListenerServiceWithViewContractTransactor{contract: contract}, nil
}

// NewListenerServiceWithViewContractFilterer creates a new log filterer instance of ListenerServiceWithViewContract, bound to a specific deployed contract.
func NewListenerServiceWithViewContractFilterer(address common.Address, filterer bind.ContractFilterer) (*ListenerServiceWithViewContractFilterer, error) {
	contract, err := bindListenerServiceWithViewContract(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ListenerServiceWithViewContractFilterer{contract: contract}, nil
}

// bindListenerServiceWithViewContract binds a generic wrapper to an already deployed contract.
func bindListenerServiceWithViewContract(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ListenerServiceWithViewContractMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ListenerServiceWithViewContract *ListenerServiceWithViewContractRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ListenerServiceWithViewContract.Contract.ListenerServiceWithViewContractCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ListenerServiceWithViewContract *ListenerServiceWithViewContractRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ListenerServiceWithViewContract.Contract.ListenerServiceWithViewContractTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ListenerServiceWithViewContract *ListenerServiceWithViewContractRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ListenerServiceWithViewContract.Contract.ListenerServiceWithViewContractTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ListenerServiceWithViewContract *ListenerServiceWithViewContractCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ListenerServiceWithViewContract.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ListenerServiceWithViewContract *ListenerServiceWithViewContractTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ListenerServiceWithViewContract.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ListenerServiceWithViewContract *ListenerServiceWithViewContractTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ListenerServiceWithViewContract.Contract.contract.Transact(opts, method, params...)
}

// ViewContractAddress is a free data retrieval call binding the contract method 0x7a9ebc15.
//
// Solidity: function viewContractAddress() view returns(address)
func (_ListenerServiceWithViewContract *ListenerServiceWithViewContractCaller) ViewContractAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ListenerServiceWithViewContract.contract.Call(opts, &out, "viewContractAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// ViewContractAddress is a free data retrieval call binding the contract method 0x7a9ebc15.
//
// Solidity: function viewContractAddress() view returns(address)
func (_ListenerServiceWithViewContract *ListenerServiceWithViewContractSession) ViewContractAddress() (common.Address, error) {
	return _ListenerServiceWithViewContract.Contract.ViewContractAddress(&_ListenerServiceWithViewContract.CallOpts)
}

// ViewContractAddress is a free data retrieval call binding the contract method 0x7a9ebc15.
//
// Solidity: function viewContractAddress() view returns(address)
func (_ListenerServiceWithViewContract *ListenerServiceWithViewContractCallerSession) ViewContractAddress() (common.Address, error) {
	return _ListenerServiceWithViewContract.Contract.ViewContractAddress(&_ListenerServiceWithViewContract.CallOpts)
}
