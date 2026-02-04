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

// IPDPProvingScheduleMetaData contains all meta data concerning the IPDPProvingSchedule contract.
var IPDPProvingScheduleMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"function\",\"name\":\"getPDPConfig\",\"inputs\":[],\"outputs\":[{\"name\":\"maxProvingPeriod\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"challengeWindow\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"challengesPerProof\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"initChallengeWindowStart\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"nextPDPChallengeWindowStart\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"provingDeadline\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"}]",
}

// IPDPProvingScheduleABI is the input ABI used to generate the binding from.
// Deprecated: Use IPDPProvingScheduleMetaData.ABI instead.
var IPDPProvingScheduleABI = IPDPProvingScheduleMetaData.ABI

// IPDPProvingSchedule is an auto generated Go binding around an Ethereum contract.
type IPDPProvingSchedule struct {
	IPDPProvingScheduleCaller     // Read-only binding to the contract
	IPDPProvingScheduleTransactor // Write-only binding to the contract
	IPDPProvingScheduleFilterer   // Log filterer for contract events
}

// IPDPProvingScheduleCaller is an auto generated read-only Go binding around an Ethereum contract.
type IPDPProvingScheduleCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// IPDPProvingScheduleTransactor is an auto generated write-only Go binding around an Ethereum contract.
type IPDPProvingScheduleTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// IPDPProvingScheduleFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type IPDPProvingScheduleFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// IPDPProvingScheduleSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type IPDPProvingScheduleSession struct {
	Contract     *IPDPProvingSchedule // Generic contract binding to set the session for
	CallOpts     bind.CallOpts        // Call options to use throughout this session
	TransactOpts bind.TransactOpts    // Transaction auth options to use throughout this session
}

// IPDPProvingScheduleCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type IPDPProvingScheduleCallerSession struct {
	Contract *IPDPProvingScheduleCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts              // Call options to use throughout this session
}

// IPDPProvingScheduleTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type IPDPProvingScheduleTransactorSession struct {
	Contract     *IPDPProvingScheduleTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts              // Transaction auth options to use throughout this session
}

// IPDPProvingScheduleRaw is an auto generated low-level Go binding around an Ethereum contract.
type IPDPProvingScheduleRaw struct {
	Contract *IPDPProvingSchedule // Generic contract binding to access the raw methods on
}

// IPDPProvingScheduleCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type IPDPProvingScheduleCallerRaw struct {
	Contract *IPDPProvingScheduleCaller // Generic read-only contract binding to access the raw methods on
}

// IPDPProvingScheduleTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type IPDPProvingScheduleTransactorRaw struct {
	Contract *IPDPProvingScheduleTransactor // Generic write-only contract binding to access the raw methods on
}

// NewIPDPProvingSchedule creates a new instance of IPDPProvingSchedule, bound to a specific deployed contract.
func NewIPDPProvingSchedule(address common.Address, backend bind.ContractBackend) (*IPDPProvingSchedule, error) {
	contract, err := bindIPDPProvingSchedule(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &IPDPProvingSchedule{IPDPProvingScheduleCaller: IPDPProvingScheduleCaller{contract: contract}, IPDPProvingScheduleTransactor: IPDPProvingScheduleTransactor{contract: contract}, IPDPProvingScheduleFilterer: IPDPProvingScheduleFilterer{contract: contract}}, nil
}

// NewIPDPProvingScheduleCaller creates a new read-only instance of IPDPProvingSchedule, bound to a specific deployed contract.
func NewIPDPProvingScheduleCaller(address common.Address, caller bind.ContractCaller) (*IPDPProvingScheduleCaller, error) {
	contract, err := bindIPDPProvingSchedule(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &IPDPProvingScheduleCaller{contract: contract}, nil
}

// NewIPDPProvingScheduleTransactor creates a new write-only instance of IPDPProvingSchedule, bound to a specific deployed contract.
func NewIPDPProvingScheduleTransactor(address common.Address, transactor bind.ContractTransactor) (*IPDPProvingScheduleTransactor, error) {
	contract, err := bindIPDPProvingSchedule(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &IPDPProvingScheduleTransactor{contract: contract}, nil
}

// NewIPDPProvingScheduleFilterer creates a new log filterer instance of IPDPProvingSchedule, bound to a specific deployed contract.
func NewIPDPProvingScheduleFilterer(address common.Address, filterer bind.ContractFilterer) (*IPDPProvingScheduleFilterer, error) {
	contract, err := bindIPDPProvingSchedule(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &IPDPProvingScheduleFilterer{contract: contract}, nil
}

// bindIPDPProvingSchedule binds a generic wrapper to an already deployed contract.
func bindIPDPProvingSchedule(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := IPDPProvingScheduleMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_IPDPProvingSchedule *IPDPProvingScheduleRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _IPDPProvingSchedule.Contract.IPDPProvingScheduleCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_IPDPProvingSchedule *IPDPProvingScheduleRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _IPDPProvingSchedule.Contract.IPDPProvingScheduleTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_IPDPProvingSchedule *IPDPProvingScheduleRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _IPDPProvingSchedule.Contract.IPDPProvingScheduleTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_IPDPProvingSchedule *IPDPProvingScheduleCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _IPDPProvingSchedule.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_IPDPProvingSchedule *IPDPProvingScheduleTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _IPDPProvingSchedule.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_IPDPProvingSchedule *IPDPProvingScheduleTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _IPDPProvingSchedule.Contract.contract.Transact(opts, method, params...)
}

// GetPDPConfig is a free data retrieval call binding the contract method 0xea0f9354.
//
// Solidity: function getPDPConfig() view returns(uint64 maxProvingPeriod, uint256 challengeWindow, uint256 challengesPerProof, uint256 initChallengeWindowStart)
func (_IPDPProvingSchedule *IPDPProvingScheduleCaller) GetPDPConfig(opts *bind.CallOpts) (struct {
	MaxProvingPeriod         uint64
	ChallengeWindow          *big.Int
	ChallengesPerProof       *big.Int
	InitChallengeWindowStart *big.Int
}, error) {
	var out []interface{}
	err := _IPDPProvingSchedule.contract.Call(opts, &out, "getPDPConfig")

	outstruct := new(struct {
		MaxProvingPeriod         uint64
		ChallengeWindow          *big.Int
		ChallengesPerProof       *big.Int
		InitChallengeWindowStart *big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.MaxProvingPeriod = *abi.ConvertType(out[0], new(uint64)).(*uint64)
	outstruct.ChallengeWindow = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	outstruct.ChallengesPerProof = *abi.ConvertType(out[2], new(*big.Int)).(**big.Int)
	outstruct.InitChallengeWindowStart = *abi.ConvertType(out[3], new(*big.Int)).(**big.Int)

	return *outstruct, err

}

// GetPDPConfig is a free data retrieval call binding the contract method 0xea0f9354.
//
// Solidity: function getPDPConfig() view returns(uint64 maxProvingPeriod, uint256 challengeWindow, uint256 challengesPerProof, uint256 initChallengeWindowStart)
func (_IPDPProvingSchedule *IPDPProvingScheduleSession) GetPDPConfig() (struct {
	MaxProvingPeriod         uint64
	ChallengeWindow          *big.Int
	ChallengesPerProof       *big.Int
	InitChallengeWindowStart *big.Int
}, error) {
	return _IPDPProvingSchedule.Contract.GetPDPConfig(&_IPDPProvingSchedule.CallOpts)
}

// GetPDPConfig is a free data retrieval call binding the contract method 0xea0f9354.
//
// Solidity: function getPDPConfig() view returns(uint64 maxProvingPeriod, uint256 challengeWindow, uint256 challengesPerProof, uint256 initChallengeWindowStart)
func (_IPDPProvingSchedule *IPDPProvingScheduleCallerSession) GetPDPConfig() (struct {
	MaxProvingPeriod         uint64
	ChallengeWindow          *big.Int
	ChallengesPerProof       *big.Int
	InitChallengeWindowStart *big.Int
}, error) {
	return _IPDPProvingSchedule.Contract.GetPDPConfig(&_IPDPProvingSchedule.CallOpts)
}

// NextPDPChallengeWindowStart is a free data retrieval call binding the contract method 0x11d41294.
//
// Solidity: function nextPDPChallengeWindowStart(uint256 setId) view returns(uint256)
func (_IPDPProvingSchedule *IPDPProvingScheduleCaller) NextPDPChallengeWindowStart(opts *bind.CallOpts, setId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _IPDPProvingSchedule.contract.Call(opts, &out, "nextPDPChallengeWindowStart", setId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// NextPDPChallengeWindowStart is a free data retrieval call binding the contract method 0x11d41294.
//
// Solidity: function nextPDPChallengeWindowStart(uint256 setId) view returns(uint256)
func (_IPDPProvingSchedule *IPDPProvingScheduleSession) NextPDPChallengeWindowStart(setId *big.Int) (*big.Int, error) {
	return _IPDPProvingSchedule.Contract.NextPDPChallengeWindowStart(&_IPDPProvingSchedule.CallOpts, setId)
}

// NextPDPChallengeWindowStart is a free data retrieval call binding the contract method 0x11d41294.
//
// Solidity: function nextPDPChallengeWindowStart(uint256 setId) view returns(uint256)
func (_IPDPProvingSchedule *IPDPProvingScheduleCallerSession) NextPDPChallengeWindowStart(setId *big.Int) (*big.Int, error) {
	return _IPDPProvingSchedule.Contract.NextPDPChallengeWindowStart(&_IPDPProvingSchedule.CallOpts, setId)
}

// ProvingDeadline is a free data retrieval call binding the contract method 0x149ac5cc.
//
// Solidity: function provingDeadline(uint256 setId) view returns(uint256)
func (_IPDPProvingSchedule *IPDPProvingScheduleCaller) ProvingDeadline(opts *bind.CallOpts, setId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _IPDPProvingSchedule.contract.Call(opts, &out, "provingDeadline", setId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ProvingDeadline is a free data retrieval call binding the contract method 0x149ac5cc.
//
// Solidity: function provingDeadline(uint256 setId) view returns(uint256)
func (_IPDPProvingSchedule *IPDPProvingScheduleSession) ProvingDeadline(setId *big.Int) (*big.Int, error) {
	return _IPDPProvingSchedule.Contract.ProvingDeadline(&_IPDPProvingSchedule.CallOpts, setId)
}

// ProvingDeadline is a free data retrieval call binding the contract method 0x149ac5cc.
//
// Solidity: function provingDeadline(uint256 setId) view returns(uint256)
func (_IPDPProvingSchedule *IPDPProvingScheduleCallerSession) ProvingDeadline(setId *big.Int) (*big.Int, error) {
	return _IPDPProvingSchedule.Contract.ProvingDeadline(&_IPDPProvingSchedule.CallOpts, setId)
}
