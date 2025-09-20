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

// ServiceProviderRegistryStorageMetaData contains all meta data concerning the ServiceProviderRegistryStorage contract.
var ServiceProviderRegistryStorageMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"function\",\"name\":\"activeProductTypeProviderCount\",\"inputs\":[{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"}],\"outputs\":[{\"name\":\"count\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"activeProviderCount\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"addressToProviderId\",\"inputs\":[{\"name\":\"providerAddress\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"productCapabilities\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"},{\"name\":\"key\",\"type\":\"string\",\"internalType\":\"string\"}],\"outputs\":[{\"name\":\"value\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"productTypeProviderCount\",\"inputs\":[{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"}],\"outputs\":[{\"name\":\"count\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"providerProducts\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"}],\"outputs\":[{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"},{\"name\":\"productData\",\"type\":\"bytes\",\"internalType\":\"bytes\"},{\"name\":\"isActive\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"providers\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"serviceProvider\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"payee\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"name\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"description\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"isActive\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"}]",
}

// ServiceProviderRegistryStorageABI is the input ABI used to generate the binding from.
// Deprecated: Use ServiceProviderRegistryStorageMetaData.ABI instead.
var ServiceProviderRegistryStorageABI = ServiceProviderRegistryStorageMetaData.ABI

// ServiceProviderRegistryStorage is an auto generated Go binding around an Ethereum contract.
type ServiceProviderRegistryStorage struct {
	ServiceProviderRegistryStorageCaller     // Read-only binding to the contract
	ServiceProviderRegistryStorageTransactor // Write-only binding to the contract
	ServiceProviderRegistryStorageFilterer   // Log filterer for contract events
}

// ServiceProviderRegistryStorageCaller is an auto generated read-only Go binding around an Ethereum contract.
type ServiceProviderRegistryStorageCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ServiceProviderRegistryStorageTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ServiceProviderRegistryStorageTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ServiceProviderRegistryStorageFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ServiceProviderRegistryStorageFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ServiceProviderRegistryStorageSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ServiceProviderRegistryStorageSession struct {
	Contract     *ServiceProviderRegistryStorage // Generic contract binding to set the session for
	CallOpts     bind.CallOpts                   // Call options to use throughout this session
	TransactOpts bind.TransactOpts               // Transaction auth options to use throughout this session
}

// ServiceProviderRegistryStorageCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ServiceProviderRegistryStorageCallerSession struct {
	Contract *ServiceProviderRegistryStorageCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts                         // Call options to use throughout this session
}

// ServiceProviderRegistryStorageTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ServiceProviderRegistryStorageTransactorSession struct {
	Contract     *ServiceProviderRegistryStorageTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts                         // Transaction auth options to use throughout this session
}

// ServiceProviderRegistryStorageRaw is an auto generated low-level Go binding around an Ethereum contract.
type ServiceProviderRegistryStorageRaw struct {
	Contract *ServiceProviderRegistryStorage // Generic contract binding to access the raw methods on
}

// ServiceProviderRegistryStorageCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ServiceProviderRegistryStorageCallerRaw struct {
	Contract *ServiceProviderRegistryStorageCaller // Generic read-only contract binding to access the raw methods on
}

// ServiceProviderRegistryStorageTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ServiceProviderRegistryStorageTransactorRaw struct {
	Contract *ServiceProviderRegistryStorageTransactor // Generic write-only contract binding to access the raw methods on
}

// NewServiceProviderRegistryStorage creates a new instance of ServiceProviderRegistryStorage, bound to a specific deployed contract.
func NewServiceProviderRegistryStorage(address common.Address, backend bind.ContractBackend) (*ServiceProviderRegistryStorage, error) {
	contract, err := bindServiceProviderRegistryStorage(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryStorage{ServiceProviderRegistryStorageCaller: ServiceProviderRegistryStorageCaller{contract: contract}, ServiceProviderRegistryStorageTransactor: ServiceProviderRegistryStorageTransactor{contract: contract}, ServiceProviderRegistryStorageFilterer: ServiceProviderRegistryStorageFilterer{contract: contract}}, nil
}

// NewServiceProviderRegistryStorageCaller creates a new read-only instance of ServiceProviderRegistryStorage, bound to a specific deployed contract.
func NewServiceProviderRegistryStorageCaller(address common.Address, caller bind.ContractCaller) (*ServiceProviderRegistryStorageCaller, error) {
	contract, err := bindServiceProviderRegistryStorage(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryStorageCaller{contract: contract}, nil
}

// NewServiceProviderRegistryStorageTransactor creates a new write-only instance of ServiceProviderRegistryStorage, bound to a specific deployed contract.
func NewServiceProviderRegistryStorageTransactor(address common.Address, transactor bind.ContractTransactor) (*ServiceProviderRegistryStorageTransactor, error) {
	contract, err := bindServiceProviderRegistryStorage(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryStorageTransactor{contract: contract}, nil
}

// NewServiceProviderRegistryStorageFilterer creates a new log filterer instance of ServiceProviderRegistryStorage, bound to a specific deployed contract.
func NewServiceProviderRegistryStorageFilterer(address common.Address, filterer bind.ContractFilterer) (*ServiceProviderRegistryStorageFilterer, error) {
	contract, err := bindServiceProviderRegistryStorage(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryStorageFilterer{contract: contract}, nil
}

// bindServiceProviderRegistryStorage binds a generic wrapper to an already deployed contract.
func bindServiceProviderRegistryStorage(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ServiceProviderRegistryStorageMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ServiceProviderRegistryStorage.Contract.ServiceProviderRegistryStorageCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ServiceProviderRegistryStorage.Contract.ServiceProviderRegistryStorageTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ServiceProviderRegistryStorage.Contract.ServiceProviderRegistryStorageTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ServiceProviderRegistryStorage.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ServiceProviderRegistryStorage.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ServiceProviderRegistryStorage.Contract.contract.Transact(opts, method, params...)
}

// ActiveProductTypeProviderCount is a free data retrieval call binding the contract method 0x8bdc7747.
//
// Solidity: function activeProductTypeProviderCount(uint8 productType) view returns(uint256 count)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageCaller) ActiveProductTypeProviderCount(opts *bind.CallOpts, productType uint8) (*big.Int, error) {
	var out []interface{}
	err := _ServiceProviderRegistryStorage.contract.Call(opts, &out, "activeProductTypeProviderCount", productType)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ActiveProductTypeProviderCount is a free data retrieval call binding the contract method 0x8bdc7747.
//
// Solidity: function activeProductTypeProviderCount(uint8 productType) view returns(uint256 count)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageSession) ActiveProductTypeProviderCount(productType uint8) (*big.Int, error) {
	return _ServiceProviderRegistryStorage.Contract.ActiveProductTypeProviderCount(&_ServiceProviderRegistryStorage.CallOpts, productType)
}

// ActiveProductTypeProviderCount is a free data retrieval call binding the contract method 0x8bdc7747.
//
// Solidity: function activeProductTypeProviderCount(uint8 productType) view returns(uint256 count)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageCallerSession) ActiveProductTypeProviderCount(productType uint8) (*big.Int, error) {
	return _ServiceProviderRegistryStorage.Contract.ActiveProductTypeProviderCount(&_ServiceProviderRegistryStorage.CallOpts, productType)
}

// ActiveProviderCount is a free data retrieval call binding the contract method 0xf08bbda0.
//
// Solidity: function activeProviderCount() view returns(uint256)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageCaller) ActiveProviderCount(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ServiceProviderRegistryStorage.contract.Call(opts, &out, "activeProviderCount")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ActiveProviderCount is a free data retrieval call binding the contract method 0xf08bbda0.
//
// Solidity: function activeProviderCount() view returns(uint256)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageSession) ActiveProviderCount() (*big.Int, error) {
	return _ServiceProviderRegistryStorage.Contract.ActiveProviderCount(&_ServiceProviderRegistryStorage.CallOpts)
}

// ActiveProviderCount is a free data retrieval call binding the contract method 0xf08bbda0.
//
// Solidity: function activeProviderCount() view returns(uint256)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageCallerSession) ActiveProviderCount() (*big.Int, error) {
	return _ServiceProviderRegistryStorage.Contract.ActiveProviderCount(&_ServiceProviderRegistryStorage.CallOpts)
}

// AddressToProviderId is a free data retrieval call binding the contract method 0xe835440e.
//
// Solidity: function addressToProviderId(address providerAddress) view returns(uint256 providerId)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageCaller) AddressToProviderId(opts *bind.CallOpts, providerAddress common.Address) (*big.Int, error) {
	var out []interface{}
	err := _ServiceProviderRegistryStorage.contract.Call(opts, &out, "addressToProviderId", providerAddress)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// AddressToProviderId is a free data retrieval call binding the contract method 0xe835440e.
//
// Solidity: function addressToProviderId(address providerAddress) view returns(uint256 providerId)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageSession) AddressToProviderId(providerAddress common.Address) (*big.Int, error) {
	return _ServiceProviderRegistryStorage.Contract.AddressToProviderId(&_ServiceProviderRegistryStorage.CallOpts, providerAddress)
}

// AddressToProviderId is a free data retrieval call binding the contract method 0xe835440e.
//
// Solidity: function addressToProviderId(address providerAddress) view returns(uint256 providerId)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageCallerSession) AddressToProviderId(providerAddress common.Address) (*big.Int, error) {
	return _ServiceProviderRegistryStorage.Contract.AddressToProviderId(&_ServiceProviderRegistryStorage.CallOpts, providerAddress)
}

// ProductCapabilities is a free data retrieval call binding the contract method 0x4368bafb.
//
// Solidity: function productCapabilities(uint256 providerId, uint8 productType, string key) view returns(string value)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageCaller) ProductCapabilities(opts *bind.CallOpts, providerId *big.Int, productType uint8, key string) (string, error) {
	var out []interface{}
	err := _ServiceProviderRegistryStorage.contract.Call(opts, &out, "productCapabilities", providerId, productType, key)

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// ProductCapabilities is a free data retrieval call binding the contract method 0x4368bafb.
//
// Solidity: function productCapabilities(uint256 providerId, uint8 productType, string key) view returns(string value)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageSession) ProductCapabilities(providerId *big.Int, productType uint8, key string) (string, error) {
	return _ServiceProviderRegistryStorage.Contract.ProductCapabilities(&_ServiceProviderRegistryStorage.CallOpts, providerId, productType, key)
}

// ProductCapabilities is a free data retrieval call binding the contract method 0x4368bafb.
//
// Solidity: function productCapabilities(uint256 providerId, uint8 productType, string key) view returns(string value)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageCallerSession) ProductCapabilities(providerId *big.Int, productType uint8, key string) (string, error) {
	return _ServiceProviderRegistryStorage.Contract.ProductCapabilities(&_ServiceProviderRegistryStorage.CallOpts, providerId, productType, key)
}

// ProductTypeProviderCount is a free data retrieval call binding the contract method 0xe459382f.
//
// Solidity: function productTypeProviderCount(uint8 productType) view returns(uint256 count)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageCaller) ProductTypeProviderCount(opts *bind.CallOpts, productType uint8) (*big.Int, error) {
	var out []interface{}
	err := _ServiceProviderRegistryStorage.contract.Call(opts, &out, "productTypeProviderCount", productType)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ProductTypeProviderCount is a free data retrieval call binding the contract method 0xe459382f.
//
// Solidity: function productTypeProviderCount(uint8 productType) view returns(uint256 count)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageSession) ProductTypeProviderCount(productType uint8) (*big.Int, error) {
	return _ServiceProviderRegistryStorage.Contract.ProductTypeProviderCount(&_ServiceProviderRegistryStorage.CallOpts, productType)
}

// ProductTypeProviderCount is a free data retrieval call binding the contract method 0xe459382f.
//
// Solidity: function productTypeProviderCount(uint8 productType) view returns(uint256 count)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageCallerSession) ProductTypeProviderCount(productType uint8) (*big.Int, error) {
	return _ServiceProviderRegistryStorage.Contract.ProductTypeProviderCount(&_ServiceProviderRegistryStorage.CallOpts, productType)
}

// ProviderProducts is a free data retrieval call binding the contract method 0x6bf6d74f.
//
// Solidity: function providerProducts(uint256 providerId, uint8 productType) view returns(uint8 productType, bytes productData, bool isActive)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageCaller) ProviderProducts(opts *bind.CallOpts, providerId *big.Int, productType uint8) (struct {
	ProductType uint8
	ProductData []byte
	IsActive    bool
}, error) {
	var out []interface{}
	err := _ServiceProviderRegistryStorage.contract.Call(opts, &out, "providerProducts", providerId, productType)

	outstruct := new(struct {
		ProductType uint8
		ProductData []byte
		IsActive    bool
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.ProductType = *abi.ConvertType(out[0], new(uint8)).(*uint8)
	outstruct.ProductData = *abi.ConvertType(out[1], new([]byte)).(*[]byte)
	outstruct.IsActive = *abi.ConvertType(out[2], new(bool)).(*bool)

	return *outstruct, err

}

// ProviderProducts is a free data retrieval call binding the contract method 0x6bf6d74f.
//
// Solidity: function providerProducts(uint256 providerId, uint8 productType) view returns(uint8 productType, bytes productData, bool isActive)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageSession) ProviderProducts(providerId *big.Int, productType uint8) (struct {
	ProductType uint8
	ProductData []byte
	IsActive    bool
}, error) {
	return _ServiceProviderRegistryStorage.Contract.ProviderProducts(&_ServiceProviderRegistryStorage.CallOpts, providerId, productType)
}

// ProviderProducts is a free data retrieval call binding the contract method 0x6bf6d74f.
//
// Solidity: function providerProducts(uint256 providerId, uint8 productType) view returns(uint8 productType, bytes productData, bool isActive)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageCallerSession) ProviderProducts(providerId *big.Int, productType uint8) (struct {
	ProductType uint8
	ProductData []byte
	IsActive    bool
}, error) {
	return _ServiceProviderRegistryStorage.Contract.ProviderProducts(&_ServiceProviderRegistryStorage.CallOpts, providerId, productType)
}

// Providers is a free data retrieval call binding the contract method 0x50f3fc81.
//
// Solidity: function providers(uint256 providerId) view returns(address serviceProvider, address payee, string name, string description, bool isActive, uint256 providerId)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageCaller) Providers(opts *bind.CallOpts, providerId *big.Int) (struct {
	ServiceProvider common.Address
	Payee           common.Address
	Name            string
	Description     string
	IsActive        bool
	ProviderId      *big.Int
}, error) {
	var out []interface{}
	err := _ServiceProviderRegistryStorage.contract.Call(opts, &out, "providers", providerId)

	outstruct := new(struct {
		ServiceProvider common.Address
		Payee           common.Address
		Name            string
		Description     string
		IsActive        bool
		ProviderId      *big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.ServiceProvider = *abi.ConvertType(out[0], new(common.Address)).(*common.Address)
	outstruct.Payee = *abi.ConvertType(out[1], new(common.Address)).(*common.Address)
	outstruct.Name = *abi.ConvertType(out[2], new(string)).(*string)
	outstruct.Description = *abi.ConvertType(out[3], new(string)).(*string)
	outstruct.IsActive = *abi.ConvertType(out[4], new(bool)).(*bool)
	outstruct.ProviderId = *abi.ConvertType(out[5], new(*big.Int)).(**big.Int)

	return *outstruct, err

}

// Providers is a free data retrieval call binding the contract method 0x50f3fc81.
//
// Solidity: function providers(uint256 providerId) view returns(address serviceProvider, address payee, string name, string description, bool isActive, uint256 providerId)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageSession) Providers(providerId *big.Int) (struct {
	ServiceProvider common.Address
	Payee           common.Address
	Name            string
	Description     string
	IsActive        bool
	ProviderId      *big.Int
}, error) {
	return _ServiceProviderRegistryStorage.Contract.Providers(&_ServiceProviderRegistryStorage.CallOpts, providerId)
}

// Providers is a free data retrieval call binding the contract method 0x50f3fc81.
//
// Solidity: function providers(uint256 providerId) view returns(address serviceProvider, address payee, string name, string description, bool isActive, uint256 providerId)
func (_ServiceProviderRegistryStorage *ServiceProviderRegistryStorageCallerSession) Providers(providerId *big.Int) (struct {
	ServiceProvider common.Address
	Payee           common.Address
	Name            string
	Description     string
	IsActive        bool
	ProviderId      *big.Int
}, error) {
	return _ServiceProviderRegistryStorage.Contract.Providers(&_ServiceProviderRegistryStorage.CallOpts, providerId)
}
