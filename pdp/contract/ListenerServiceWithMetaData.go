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

// ListenerServiceWithMetaDataMetaData contains all meta data concerning the ListenerServiceWithMetaData contract.
var ListenerServiceWithMetaDataMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"function\",\"name\":\"getDataSetMetadata\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"key\",\"type\":\"string\",\"internalType\":\"string\"}],\"outputs\":[{\"name\":\"exists\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"value\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"}]",
}

// ListenerServiceWithMetaDataABI is the input ABI used to generate the binding from.
// Deprecated: Use ListenerServiceWithMetaDataMetaData.ABI instead.
var ListenerServiceWithMetaDataABI = ListenerServiceWithMetaDataMetaData.ABI

// ListenerServiceWithMetaData is an auto generated Go binding around an Ethereum contract.
type ListenerServiceWithMetaData struct {
	ListenerServiceWithMetaDataCaller     // Read-only binding to the contract
	ListenerServiceWithMetaDataTransactor // Write-only binding to the contract
	ListenerServiceWithMetaDataFilterer   // Log filterer for contract events
}

// ListenerServiceWithMetaDataCaller is an auto generated read-only Go binding around an Ethereum contract.
type ListenerServiceWithMetaDataCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ListenerServiceWithMetaDataTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ListenerServiceWithMetaDataTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ListenerServiceWithMetaDataFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ListenerServiceWithMetaDataFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ListenerServiceWithMetaDataSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ListenerServiceWithMetaDataSession struct {
	Contract     *ListenerServiceWithMetaData // Generic contract binding to set the session for
	CallOpts     bind.CallOpts                // Call options to use throughout this session
	TransactOpts bind.TransactOpts            // Transaction auth options to use throughout this session
}

// ListenerServiceWithMetaDataCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ListenerServiceWithMetaDataCallerSession struct {
	Contract *ListenerServiceWithMetaDataCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts                      // Call options to use throughout this session
}

// ListenerServiceWithMetaDataTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ListenerServiceWithMetaDataTransactorSession struct {
	Contract     *ListenerServiceWithMetaDataTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts                      // Transaction auth options to use throughout this session
}

// ListenerServiceWithMetaDataRaw is an auto generated low-level Go binding around an Ethereum contract.
type ListenerServiceWithMetaDataRaw struct {
	Contract *ListenerServiceWithMetaData // Generic contract binding to access the raw methods on
}

// ListenerServiceWithMetaDataCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ListenerServiceWithMetaDataCallerRaw struct {
	Contract *ListenerServiceWithMetaDataCaller // Generic read-only contract binding to access the raw methods on
}

// ListenerServiceWithMetaDataTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ListenerServiceWithMetaDataTransactorRaw struct {
	Contract *ListenerServiceWithMetaDataTransactor // Generic write-only contract binding to access the raw methods on
}

// NewListenerServiceWithMetaData creates a new instance of ListenerServiceWithMetaData, bound to a specific deployed contract.
func NewListenerServiceWithMetaData(address common.Address, backend bind.ContractBackend) (*ListenerServiceWithMetaData, error) {
	contract, err := bindListenerServiceWithMetaData(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ListenerServiceWithMetaData{ListenerServiceWithMetaDataCaller: ListenerServiceWithMetaDataCaller{contract: contract}, ListenerServiceWithMetaDataTransactor: ListenerServiceWithMetaDataTransactor{contract: contract}, ListenerServiceWithMetaDataFilterer: ListenerServiceWithMetaDataFilterer{contract: contract}}, nil
}

// NewListenerServiceWithMetaDataCaller creates a new read-only instance of ListenerServiceWithMetaData, bound to a specific deployed contract.
func NewListenerServiceWithMetaDataCaller(address common.Address, caller bind.ContractCaller) (*ListenerServiceWithMetaDataCaller, error) {
	contract, err := bindListenerServiceWithMetaData(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ListenerServiceWithMetaDataCaller{contract: contract}, nil
}

// NewListenerServiceWithMetaDataTransactor creates a new write-only instance of ListenerServiceWithMetaData, bound to a specific deployed contract.
func NewListenerServiceWithMetaDataTransactor(address common.Address, transactor bind.ContractTransactor) (*ListenerServiceWithMetaDataTransactor, error) {
	contract, err := bindListenerServiceWithMetaData(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ListenerServiceWithMetaDataTransactor{contract: contract}, nil
}

// NewListenerServiceWithMetaDataFilterer creates a new log filterer instance of ListenerServiceWithMetaData, bound to a specific deployed contract.
func NewListenerServiceWithMetaDataFilterer(address common.Address, filterer bind.ContractFilterer) (*ListenerServiceWithMetaDataFilterer, error) {
	contract, err := bindListenerServiceWithMetaData(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ListenerServiceWithMetaDataFilterer{contract: contract}, nil
}

// bindListenerServiceWithMetaData binds a generic wrapper to an already deployed contract.
func bindListenerServiceWithMetaData(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ListenerServiceWithMetaDataMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ListenerServiceWithMetaData *ListenerServiceWithMetaDataRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ListenerServiceWithMetaData.Contract.ListenerServiceWithMetaDataCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ListenerServiceWithMetaData *ListenerServiceWithMetaDataRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ListenerServiceWithMetaData.Contract.ListenerServiceWithMetaDataTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ListenerServiceWithMetaData *ListenerServiceWithMetaDataRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ListenerServiceWithMetaData.Contract.ListenerServiceWithMetaDataTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ListenerServiceWithMetaData *ListenerServiceWithMetaDataCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ListenerServiceWithMetaData.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ListenerServiceWithMetaData *ListenerServiceWithMetaDataTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ListenerServiceWithMetaData.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ListenerServiceWithMetaData *ListenerServiceWithMetaDataTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ListenerServiceWithMetaData.Contract.contract.Transact(opts, method, params...)
}

// GetDataSetMetadata is a free data retrieval call binding the contract method 0x4dc17df1.
//
// Solidity: function getDataSetMetadata(uint256 dataSetId, string key) view returns(bool exists, string value)
func (_ListenerServiceWithMetaData *ListenerServiceWithMetaDataCaller) GetDataSetMetadata(opts *bind.CallOpts, dataSetId *big.Int, key string) (struct {
	Exists bool
	Value  string
}, error) {
	var out []interface{}
	err := _ListenerServiceWithMetaData.contract.Call(opts, &out, "getDataSetMetadata", dataSetId, key)

	outstruct := new(struct {
		Exists bool
		Value  string
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Exists = *abi.ConvertType(out[0], new(bool)).(*bool)
	outstruct.Value = *abi.ConvertType(out[1], new(string)).(*string)

	return *outstruct, err

}

// GetDataSetMetadata is a free data retrieval call binding the contract method 0x4dc17df1.
//
// Solidity: function getDataSetMetadata(uint256 dataSetId, string key) view returns(bool exists, string value)
func (_ListenerServiceWithMetaData *ListenerServiceWithMetaDataSession) GetDataSetMetadata(dataSetId *big.Int, key string) (struct {
	Exists bool
	Value  string
}, error) {
	return _ListenerServiceWithMetaData.Contract.GetDataSetMetadata(&_ListenerServiceWithMetaData.CallOpts, dataSetId, key)
}

// GetDataSetMetadata is a free data retrieval call binding the contract method 0x4dc17df1.
//
// Solidity: function getDataSetMetadata(uint256 dataSetId, string key) view returns(bool exists, string value)
func (_ListenerServiceWithMetaData *ListenerServiceWithMetaDataCallerSession) GetDataSetMetadata(dataSetId *big.Int, key string) (struct {
	Exists bool
	Value  string
}, error) {
	return _ListenerServiceWithMetaData.Contract.GetDataSetMetadata(&_ListenerServiceWithMetaData.CallOpts, dataSetId, key)
}
