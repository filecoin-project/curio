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

// ContractWithViewMetaData contains all meta data concerning the ContractWithView contract.
var ContractWithViewMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"function\",\"name\":\"viewContractAddress\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"}]",
}

// ContractWithViewABI is the input ABI used to generate the binding from.
// Deprecated: Use ContractWithViewMetaData.ABI instead.
var ContractWithViewABI = ContractWithViewMetaData.ABI

// ContractWithView is an auto generated Go binding around an Ethereum contract.
type ContractWithView struct {
	ContractWithViewCaller     // Read-only binding to the contract
	ContractWithViewTransactor // Write-only binding to the contract
	ContractWithViewFilterer   // Log filterer for contract events
}

// ContractWithViewCaller is an auto generated read-only Go binding around an Ethereum contract.
type ContractWithViewCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ContractWithViewTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ContractWithViewTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ContractWithViewFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ContractWithViewFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ContractWithViewSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ContractWithViewSession struct {
	Contract     *ContractWithView // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// ContractWithViewCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ContractWithViewCallerSession struct {
	Contract *ContractWithViewCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts           // Call options to use throughout this session
}

// ContractWithViewTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ContractWithViewTransactorSession struct {
	Contract     *ContractWithViewTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts           // Transaction auth options to use throughout this session
}

// ContractWithViewRaw is an auto generated low-level Go binding around an Ethereum contract.
type ContractWithViewRaw struct {
	Contract *ContractWithView // Generic contract binding to access the raw methods on
}

// ContractWithViewCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ContractWithViewCallerRaw struct {
	Contract *ContractWithViewCaller // Generic read-only contract binding to access the raw methods on
}

// ContractWithViewTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ContractWithViewTransactorRaw struct {
	Contract *ContractWithViewTransactor // Generic write-only contract binding to access the raw methods on
}

// NewContractWithView creates a new instance of ContractWithView, bound to a specific deployed contract.
func NewContractWithView(address common.Address, backend bind.ContractBackend) (*ContractWithView, error) {
	contract, err := bindContractWithView(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ContractWithView{ContractWithViewCaller: ContractWithViewCaller{contract: contract}, ContractWithViewTransactor: ContractWithViewTransactor{contract: contract}, ContractWithViewFilterer: ContractWithViewFilterer{contract: contract}}, nil
}

// NewContractWithViewCaller creates a new read-only instance of ContractWithView, bound to a specific deployed contract.
func NewContractWithViewCaller(address common.Address, caller bind.ContractCaller) (*ContractWithViewCaller, error) {
	contract, err := bindContractWithView(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ContractWithViewCaller{contract: contract}, nil
}

// NewContractWithViewTransactor creates a new write-only instance of ContractWithView, bound to a specific deployed contract.
func NewContractWithViewTransactor(address common.Address, transactor bind.ContractTransactor) (*ContractWithViewTransactor, error) {
	contract, err := bindContractWithView(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ContractWithViewTransactor{contract: contract}, nil
}

// NewContractWithViewFilterer creates a new log filterer instance of ContractWithView, bound to a specific deployed contract.
func NewContractWithViewFilterer(address common.Address, filterer bind.ContractFilterer) (*ContractWithViewFilterer, error) {
	contract, err := bindContractWithView(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ContractWithViewFilterer{contract: contract}, nil
}

// bindContractWithView binds a generic wrapper to an already deployed contract.
func bindContractWithView(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ContractWithViewMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ContractWithView *ContractWithViewRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ContractWithView.Contract.ContractWithViewCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ContractWithView *ContractWithViewRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ContractWithView.Contract.ContractWithViewTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ContractWithView *ContractWithViewRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ContractWithView.Contract.ContractWithViewTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ContractWithView *ContractWithViewCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ContractWithView.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ContractWithView *ContractWithViewTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ContractWithView.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ContractWithView *ContractWithViewTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ContractWithView.Contract.contract.Transact(opts, method, params...)
}

// ViewContractAddress is a free data retrieval call binding the contract method 0x7a9ebc15.
//
// Solidity: function viewContractAddress() view returns(address)
func (_ContractWithView *ContractWithViewCaller) ViewContractAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ContractWithView.contract.Call(opts, &out, "viewContractAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// ViewContractAddress is a free data retrieval call binding the contract method 0x7a9ebc15.
//
// Solidity: function viewContractAddress() view returns(address)
func (_ContractWithView *ContractWithViewSession) ViewContractAddress() (common.Address, error) {
	return _ContractWithView.Contract.ViewContractAddress(&_ContractWithView.CallOpts)
}

// ViewContractAddress is a free data retrieval call binding the contract method 0x7a9ebc15.
//
// Solidity: function viewContractAddress() view returns(address)
func (_ContractWithView *ContractWithViewCallerSession) ViewContractAddress() (common.Address, error) {
	return _ContractWithView.Contract.ViewContractAddress(&_ContractWithView.CallOpts)
}
