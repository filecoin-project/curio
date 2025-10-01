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

// FilecoinWarmStorageServiceDataSetInfo is an auto generated low-level Go binding around an user-defined struct.
type FilecoinWarmStorageServiceDataSetInfo struct {
	PdpRailId       *big.Int
	CacheMissRailId *big.Int
	CdnRailId       *big.Int
	Payer           common.Address
	Payee           common.Address
	ServiceProvider common.Address
	CommissionBps   *big.Int
	ClientDataSetId *big.Int
	PdpEndEpoch     *big.Int
	ProviderId      *big.Int
	CdnEndEpoch     *big.Int
}

// FilecoinWarmStorageServiceStateViewMetaData contains all meta data concerning the FilecoinWarmStorageServiceStateView contract.
var FilecoinWarmStorageServiceStateViewMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[{\"name\":\"_service\",\"type\":\"address\",\"internalType\":\"contractFilecoinWarmStorageService\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"challengeWindow\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"clientDataSetIDs\",\"inputs\":[{\"name\":\"payer\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"clientDataSets\",\"inputs\":[{\"name\":\"payer\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"dataSetIds\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"filCDNControllerAddress\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getAllDataSetMetadata\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"keys\",\"type\":\"string[]\",\"internalType\":\"string[]\"},{\"name\":\"values\",\"type\":\"string[]\",\"internalType\":\"string[]\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getAllPieceMetadata\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"pieceId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"keys\",\"type\":\"string[]\",\"internalType\":\"string[]\"},{\"name\":\"values\",\"type\":\"string[]\",\"internalType\":\"string[]\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getApprovedProviders\",\"inputs\":[],\"outputs\":[{\"name\":\"providerIds\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getChallengesPerProof\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"pure\"},{\"type\":\"function\",\"name\":\"getClientDataSets\",\"inputs\":[{\"name\":\"client\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"infos\",\"type\":\"tuple[]\",\"internalType\":\"structFilecoinWarmStorageService.DataSetInfo[]\",\"components\":[{\"name\":\"pdpRailId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"cacheMissRailId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"cdnRailId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"payer\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"payee\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"serviceProvider\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"commissionBps\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"clientDataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"pdpEndEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"cdnEndEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getDataSet\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"info\",\"type\":\"tuple\",\"internalType\":\"structFilecoinWarmStorageService.DataSetInfo\",\"components\":[{\"name\":\"pdpRailId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"cacheMissRailId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"cdnRailId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"payer\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"payee\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"serviceProvider\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"commissionBps\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"clientDataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"pdpEndEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"cdnEndEpoch\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getDataSetMetadata\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"key\",\"type\":\"string\",\"internalType\":\"string\"}],\"outputs\":[{\"name\":\"exists\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"value\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getDataSetSizeInBytes\",\"inputs\":[{\"name\":\"leafCount\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"pure\"},{\"type\":\"function\",\"name\":\"getMaxProvingPeriod\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint64\",\"internalType\":\"uint64\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getPDPConfig\",\"inputs\":[],\"outputs\":[{\"name\":\"maxProvingPeriod\",\"type\":\"uint64\",\"internalType\":\"uint64\"},{\"name\":\"challengeWindowSize\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"challengesPerProof\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"initChallengeWindowStart\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getPieceMetadata\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"pieceId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"key\",\"type\":\"string\",\"internalType\":\"string\"}],\"outputs\":[{\"name\":\"exists\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"value\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"isProviderApproved\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"nextPDPChallengeWindowStart\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"provenPeriods\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"periodId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"provenThisPeriod\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"provingActivationEpoch\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"provingDeadline\",\"inputs\":[{\"name\":\"setId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"railToDataSet\",\"inputs\":[{\"name\":\"railId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"service\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"contractFilecoinWarmStorageService\"}],\"stateMutability\":\"view\"},{\"type\":\"error\",\"name\":\"ProvingPeriodNotInitialized\",\"inputs\":[{\"name\":\"dataSetId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}]",
}

// FilecoinWarmStorageServiceStateViewABI is the input ABI used to generate the binding from.
// Deprecated: Use FilecoinWarmStorageServiceStateViewMetaData.ABI instead.
var FilecoinWarmStorageServiceStateViewABI = FilecoinWarmStorageServiceStateViewMetaData.ABI

// FilecoinWarmStorageServiceStateView is an auto generated Go binding around an Ethereum contract.
type FilecoinWarmStorageServiceStateView struct {
	FilecoinWarmStorageServiceStateViewCaller     // Read-only binding to the contract
	FilecoinWarmStorageServiceStateViewTransactor // Write-only binding to the contract
	FilecoinWarmStorageServiceStateViewFilterer   // Log filterer for contract events
}

// FilecoinWarmStorageServiceStateViewCaller is an auto generated read-only Go binding around an Ethereum contract.
type FilecoinWarmStorageServiceStateViewCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FilecoinWarmStorageServiceStateViewTransactor is an auto generated write-only Go binding around an Ethereum contract.
type FilecoinWarmStorageServiceStateViewTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FilecoinWarmStorageServiceStateViewFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type FilecoinWarmStorageServiceStateViewFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FilecoinWarmStorageServiceStateViewSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type FilecoinWarmStorageServiceStateViewSession struct {
	Contract     *FilecoinWarmStorageServiceStateView // Generic contract binding to set the session for
	CallOpts     bind.CallOpts                        // Call options to use throughout this session
	TransactOpts bind.TransactOpts                    // Transaction auth options to use throughout this session
}

// FilecoinWarmStorageServiceStateViewCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type FilecoinWarmStorageServiceStateViewCallerSession struct {
	Contract *FilecoinWarmStorageServiceStateViewCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts                              // Call options to use throughout this session
}

// FilecoinWarmStorageServiceStateViewTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type FilecoinWarmStorageServiceStateViewTransactorSession struct {
	Contract     *FilecoinWarmStorageServiceStateViewTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts                              // Transaction auth options to use throughout this session
}

// FilecoinWarmStorageServiceStateViewRaw is an auto generated low-level Go binding around an Ethereum contract.
type FilecoinWarmStorageServiceStateViewRaw struct {
	Contract *FilecoinWarmStorageServiceStateView // Generic contract binding to access the raw methods on
}

// FilecoinWarmStorageServiceStateViewCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type FilecoinWarmStorageServiceStateViewCallerRaw struct {
	Contract *FilecoinWarmStorageServiceStateViewCaller // Generic read-only contract binding to access the raw methods on
}

// FilecoinWarmStorageServiceStateViewTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type FilecoinWarmStorageServiceStateViewTransactorRaw struct {
	Contract *FilecoinWarmStorageServiceStateViewTransactor // Generic write-only contract binding to access the raw methods on
}

// NewFilecoinWarmStorageServiceStateView creates a new instance of FilecoinWarmStorageServiceStateView, bound to a specific deployed contract.
func NewFilecoinWarmStorageServiceStateView(address common.Address, backend bind.ContractBackend) (*FilecoinWarmStorageServiceStateView, error) {
	contract, err := bindFilecoinWarmStorageServiceStateView(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceStateView{FilecoinWarmStorageServiceStateViewCaller: FilecoinWarmStorageServiceStateViewCaller{contract: contract}, FilecoinWarmStorageServiceStateViewTransactor: FilecoinWarmStorageServiceStateViewTransactor{contract: contract}, FilecoinWarmStorageServiceStateViewFilterer: FilecoinWarmStorageServiceStateViewFilterer{contract: contract}}, nil
}

// NewFilecoinWarmStorageServiceStateViewCaller creates a new read-only instance of FilecoinWarmStorageServiceStateView, bound to a specific deployed contract.
func NewFilecoinWarmStorageServiceStateViewCaller(address common.Address, caller bind.ContractCaller) (*FilecoinWarmStorageServiceStateViewCaller, error) {
	contract, err := bindFilecoinWarmStorageServiceStateView(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceStateViewCaller{contract: contract}, nil
}

// NewFilecoinWarmStorageServiceStateViewTransactor creates a new write-only instance of FilecoinWarmStorageServiceStateView, bound to a specific deployed contract.
func NewFilecoinWarmStorageServiceStateViewTransactor(address common.Address, transactor bind.ContractTransactor) (*FilecoinWarmStorageServiceStateViewTransactor, error) {
	contract, err := bindFilecoinWarmStorageServiceStateView(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceStateViewTransactor{contract: contract}, nil
}

// NewFilecoinWarmStorageServiceStateViewFilterer creates a new log filterer instance of FilecoinWarmStorageServiceStateView, bound to a specific deployed contract.
func NewFilecoinWarmStorageServiceStateViewFilterer(address common.Address, filterer bind.ContractFilterer) (*FilecoinWarmStorageServiceStateViewFilterer, error) {
	contract, err := bindFilecoinWarmStorageServiceStateView(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &FilecoinWarmStorageServiceStateViewFilterer{contract: contract}, nil
}

// bindFilecoinWarmStorageServiceStateView binds a generic wrapper to an already deployed contract.
func bindFilecoinWarmStorageServiceStateView(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := FilecoinWarmStorageServiceStateViewMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FilecoinWarmStorageServiceStateView.Contract.FilecoinWarmStorageServiceStateViewCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.FilecoinWarmStorageServiceStateViewTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.FilecoinWarmStorageServiceStateViewTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FilecoinWarmStorageServiceStateView.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.contract.Transact(opts, method, params...)
}

// ChallengeWindow is a free data retrieval call binding the contract method 0x861a1412.
//
// Solidity: function challengeWindow() view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) ChallengeWindow(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "challengeWindow")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ChallengeWindow is a free data retrieval call binding the contract method 0x861a1412.
//
// Solidity: function challengeWindow() view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) ChallengeWindow() (*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.ChallengeWindow(&_FilecoinWarmStorageServiceStateView.CallOpts)
}

// ChallengeWindow is a free data retrieval call binding the contract method 0x861a1412.
//
// Solidity: function challengeWindow() view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) ChallengeWindow() (*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.ChallengeWindow(&_FilecoinWarmStorageServiceStateView.CallOpts)
}

// ClientDataSetIDs is a free data retrieval call binding the contract method 0x196ed89b.
//
// Solidity: function clientDataSetIDs(address payer) view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) ClientDataSetIDs(opts *bind.CallOpts, payer common.Address) (*big.Int, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "clientDataSetIDs", payer)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ClientDataSetIDs is a free data retrieval call binding the contract method 0x196ed89b.
//
// Solidity: function clientDataSetIDs(address payer) view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) ClientDataSetIDs(payer common.Address) (*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.ClientDataSetIDs(&_FilecoinWarmStorageServiceStateView.CallOpts, payer)
}

// ClientDataSetIDs is a free data retrieval call binding the contract method 0x196ed89b.
//
// Solidity: function clientDataSetIDs(address payer) view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) ClientDataSetIDs(payer common.Address) (*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.ClientDataSetIDs(&_FilecoinWarmStorageServiceStateView.CallOpts, payer)
}

// ClientDataSets is a free data retrieval call binding the contract method 0x7dab7c40.
//
// Solidity: function clientDataSets(address payer) view returns(uint256[] dataSetIds)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) ClientDataSets(opts *bind.CallOpts, payer common.Address) ([]*big.Int, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "clientDataSets", payer)

	if err != nil {
		return *new([]*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new([]*big.Int)).(*[]*big.Int)

	return out0, err

}

// ClientDataSets is a free data retrieval call binding the contract method 0x7dab7c40.
//
// Solidity: function clientDataSets(address payer) view returns(uint256[] dataSetIds)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) ClientDataSets(payer common.Address) ([]*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.ClientDataSets(&_FilecoinWarmStorageServiceStateView.CallOpts, payer)
}

// ClientDataSets is a free data retrieval call binding the contract method 0x7dab7c40.
//
// Solidity: function clientDataSets(address payer) view returns(uint256[] dataSetIds)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) ClientDataSets(payer common.Address) ([]*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.ClientDataSets(&_FilecoinWarmStorageServiceStateView.CallOpts, payer)
}

// FilCDNControllerAddress is a free data retrieval call binding the contract method 0x1b5f2b8f.
//
// Solidity: function filCDNControllerAddress() view returns(address)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) FilCDNControllerAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "filCDNControllerAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// FilCDNControllerAddress is a free data retrieval call binding the contract method 0x1b5f2b8f.
//
// Solidity: function filCDNControllerAddress() view returns(address)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) FilCDNControllerAddress() (common.Address, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.FilCDNControllerAddress(&_FilecoinWarmStorageServiceStateView.CallOpts)
}

// FilCDNControllerAddress is a free data retrieval call binding the contract method 0x1b5f2b8f.
//
// Solidity: function filCDNControllerAddress() view returns(address)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) FilCDNControllerAddress() (common.Address, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.FilCDNControllerAddress(&_FilecoinWarmStorageServiceStateView.CallOpts)
}

// GetAllDataSetMetadata is a free data retrieval call binding the contract method 0xf417c13f.
//
// Solidity: function getAllDataSetMetadata(uint256 dataSetId) view returns(string[] keys, string[] values)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) GetAllDataSetMetadata(opts *bind.CallOpts, dataSetId *big.Int) (struct {
	Keys   []string
	Values []string
}, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "getAllDataSetMetadata", dataSetId)

	outstruct := new(struct {
		Keys   []string
		Values []string
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Keys = *abi.ConvertType(out[0], new([]string)).(*[]string)
	outstruct.Values = *abi.ConvertType(out[1], new([]string)).(*[]string)

	return *outstruct, err

}

// GetAllDataSetMetadata is a free data retrieval call binding the contract method 0xf417c13f.
//
// Solidity: function getAllDataSetMetadata(uint256 dataSetId) view returns(string[] keys, string[] values)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) GetAllDataSetMetadata(dataSetId *big.Int) (struct {
	Keys   []string
	Values []string
}, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetAllDataSetMetadata(&_FilecoinWarmStorageServiceStateView.CallOpts, dataSetId)
}

// GetAllDataSetMetadata is a free data retrieval call binding the contract method 0xf417c13f.
//
// Solidity: function getAllDataSetMetadata(uint256 dataSetId) view returns(string[] keys, string[] values)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) GetAllDataSetMetadata(dataSetId *big.Int) (struct {
	Keys   []string
	Values []string
}, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetAllDataSetMetadata(&_FilecoinWarmStorageServiceStateView.CallOpts, dataSetId)
}

// GetAllPieceMetadata is a free data retrieval call binding the contract method 0x3c0bd253.
//
// Solidity: function getAllPieceMetadata(uint256 dataSetId, uint256 pieceId) view returns(string[] keys, string[] values)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) GetAllPieceMetadata(opts *bind.CallOpts, dataSetId *big.Int, pieceId *big.Int) (struct {
	Keys   []string
	Values []string
}, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "getAllPieceMetadata", dataSetId, pieceId)

	outstruct := new(struct {
		Keys   []string
		Values []string
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Keys = *abi.ConvertType(out[0], new([]string)).(*[]string)
	outstruct.Values = *abi.ConvertType(out[1], new([]string)).(*[]string)

	return *outstruct, err

}

// GetAllPieceMetadata is a free data retrieval call binding the contract method 0x3c0bd253.
//
// Solidity: function getAllPieceMetadata(uint256 dataSetId, uint256 pieceId) view returns(string[] keys, string[] values)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) GetAllPieceMetadata(dataSetId *big.Int, pieceId *big.Int) (struct {
	Keys   []string
	Values []string
}, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetAllPieceMetadata(&_FilecoinWarmStorageServiceStateView.CallOpts, dataSetId, pieceId)
}

// GetAllPieceMetadata is a free data retrieval call binding the contract method 0x3c0bd253.
//
// Solidity: function getAllPieceMetadata(uint256 dataSetId, uint256 pieceId) view returns(string[] keys, string[] values)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) GetAllPieceMetadata(dataSetId *big.Int, pieceId *big.Int) (struct {
	Keys   []string
	Values []string
}, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetAllPieceMetadata(&_FilecoinWarmStorageServiceStateView.CallOpts, dataSetId, pieceId)
}

// GetApprovedProviders is a free data retrieval call binding the contract method 0x266afe1b.
//
// Solidity: function getApprovedProviders() view returns(uint256[] providerIds)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) GetApprovedProviders(opts *bind.CallOpts) ([]*big.Int, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "getApprovedProviders")

	if err != nil {
		return *new([]*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new([]*big.Int)).(*[]*big.Int)

	return out0, err

}

// GetApprovedProviders is a free data retrieval call binding the contract method 0x266afe1b.
//
// Solidity: function getApprovedProviders() view returns(uint256[] providerIds)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) GetApprovedProviders() ([]*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetApprovedProviders(&_FilecoinWarmStorageServiceStateView.CallOpts)
}

// GetApprovedProviders is a free data retrieval call binding the contract method 0x266afe1b.
//
// Solidity: function getApprovedProviders() view returns(uint256[] providerIds)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) GetApprovedProviders() ([]*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetApprovedProviders(&_FilecoinWarmStorageServiceStateView.CallOpts)
}

// GetChallengesPerProof is a free data retrieval call binding the contract method 0x47d3dfe7.
//
// Solidity: function getChallengesPerProof() pure returns(uint64)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) GetChallengesPerProof(opts *bind.CallOpts) (uint64, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "getChallengesPerProof")

	if err != nil {
		return *new(uint64), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)

	return out0, err

}

// GetChallengesPerProof is a free data retrieval call binding the contract method 0x47d3dfe7.
//
// Solidity: function getChallengesPerProof() pure returns(uint64)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) GetChallengesPerProof() (uint64, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetChallengesPerProof(&_FilecoinWarmStorageServiceStateView.CallOpts)
}

// GetChallengesPerProof is a free data retrieval call binding the contract method 0x47d3dfe7.
//
// Solidity: function getChallengesPerProof() pure returns(uint64)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) GetChallengesPerProof() (uint64, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetChallengesPerProof(&_FilecoinWarmStorageServiceStateView.CallOpts)
}

// GetClientDataSets is a free data retrieval call binding the contract method 0x967c6f21.
//
// Solidity: function getClientDataSets(address client) view returns((uint256,uint256,uint256,address,address,address,uint256,uint256,uint256,uint256,uint256)[] infos)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) GetClientDataSets(opts *bind.CallOpts, client common.Address) ([]FilecoinWarmStorageServiceDataSetInfo, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "getClientDataSets", client)

	if err != nil {
		return *new([]FilecoinWarmStorageServiceDataSetInfo), err
	}

	out0 := *abi.ConvertType(out[0], new([]FilecoinWarmStorageServiceDataSetInfo)).(*[]FilecoinWarmStorageServiceDataSetInfo)

	return out0, err

}

// GetClientDataSets is a free data retrieval call binding the contract method 0x967c6f21.
//
// Solidity: function getClientDataSets(address client) view returns((uint256,uint256,uint256,address,address,address,uint256,uint256,uint256,uint256,uint256)[] infos)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) GetClientDataSets(client common.Address) ([]FilecoinWarmStorageServiceDataSetInfo, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetClientDataSets(&_FilecoinWarmStorageServiceStateView.CallOpts, client)
}

// GetClientDataSets is a free data retrieval call binding the contract method 0x967c6f21.
//
// Solidity: function getClientDataSets(address client) view returns((uint256,uint256,uint256,address,address,address,uint256,uint256,uint256,uint256,uint256)[] infos)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) GetClientDataSets(client common.Address) ([]FilecoinWarmStorageServiceDataSetInfo, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetClientDataSets(&_FilecoinWarmStorageServiceStateView.CallOpts, client)
}

// GetDataSet is a free data retrieval call binding the contract method 0xbdaac056.
//
// Solidity: function getDataSet(uint256 dataSetId) view returns((uint256,uint256,uint256,address,address,address,uint256,uint256,uint256,uint256,uint256) info)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) GetDataSet(opts *bind.CallOpts, dataSetId *big.Int) (FilecoinWarmStorageServiceDataSetInfo, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "getDataSet", dataSetId)

	if err != nil {
		return *new(FilecoinWarmStorageServiceDataSetInfo), err
	}

	out0 := *abi.ConvertType(out[0], new(FilecoinWarmStorageServiceDataSetInfo)).(*FilecoinWarmStorageServiceDataSetInfo)

	return out0, err

}

// GetDataSet is a free data retrieval call binding the contract method 0xbdaac056.
//
// Solidity: function getDataSet(uint256 dataSetId) view returns((uint256,uint256,uint256,address,address,address,uint256,uint256,uint256,uint256,uint256) info)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) GetDataSet(dataSetId *big.Int) (FilecoinWarmStorageServiceDataSetInfo, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetDataSet(&_FilecoinWarmStorageServiceStateView.CallOpts, dataSetId)
}

// GetDataSet is a free data retrieval call binding the contract method 0xbdaac056.
//
// Solidity: function getDataSet(uint256 dataSetId) view returns((uint256,uint256,uint256,address,address,address,uint256,uint256,uint256,uint256,uint256) info)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) GetDataSet(dataSetId *big.Int) (FilecoinWarmStorageServiceDataSetInfo, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetDataSet(&_FilecoinWarmStorageServiceStateView.CallOpts, dataSetId)
}

// GetDataSetMetadata is a free data retrieval call binding the contract method 0x4dc17df1.
//
// Solidity: function getDataSetMetadata(uint256 dataSetId, string key) view returns(bool exists, string value)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) GetDataSetMetadata(opts *bind.CallOpts, dataSetId *big.Int, key string) (struct {
	Exists bool
	Value  string
}, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "getDataSetMetadata", dataSetId, key)

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
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) GetDataSetMetadata(dataSetId *big.Int, key string) (struct {
	Exists bool
	Value  string
}, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetDataSetMetadata(&_FilecoinWarmStorageServiceStateView.CallOpts, dataSetId, key)
}

// GetDataSetMetadata is a free data retrieval call binding the contract method 0x4dc17df1.
//
// Solidity: function getDataSetMetadata(uint256 dataSetId, string key) view returns(bool exists, string value)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) GetDataSetMetadata(dataSetId *big.Int, key string) (struct {
	Exists bool
	Value  string
}, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetDataSetMetadata(&_FilecoinWarmStorageServiceStateView.CallOpts, dataSetId, key)
}

// GetDataSetSizeInBytes is a free data retrieval call binding the contract method 0xfe295953.
//
// Solidity: function getDataSetSizeInBytes(uint256 leafCount) pure returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) GetDataSetSizeInBytes(opts *bind.CallOpts, leafCount *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "getDataSetSizeInBytes", leafCount)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetDataSetSizeInBytes is a free data retrieval call binding the contract method 0xfe295953.
//
// Solidity: function getDataSetSizeInBytes(uint256 leafCount) pure returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) GetDataSetSizeInBytes(leafCount *big.Int) (*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetDataSetSizeInBytes(&_FilecoinWarmStorageServiceStateView.CallOpts, leafCount)
}

// GetDataSetSizeInBytes is a free data retrieval call binding the contract method 0xfe295953.
//
// Solidity: function getDataSetSizeInBytes(uint256 leafCount) pure returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) GetDataSetSizeInBytes(leafCount *big.Int) (*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetDataSetSizeInBytes(&_FilecoinWarmStorageServiceStateView.CallOpts, leafCount)
}

// GetMaxProvingPeriod is a free data retrieval call binding the contract method 0xf2f12333.
//
// Solidity: function getMaxProvingPeriod() view returns(uint64)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) GetMaxProvingPeriod(opts *bind.CallOpts) (uint64, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "getMaxProvingPeriod")

	if err != nil {
		return *new(uint64), err
	}

	out0 := *abi.ConvertType(out[0], new(uint64)).(*uint64)

	return out0, err

}

// GetMaxProvingPeriod is a free data retrieval call binding the contract method 0xf2f12333.
//
// Solidity: function getMaxProvingPeriod() view returns(uint64)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) GetMaxProvingPeriod() (uint64, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetMaxProvingPeriod(&_FilecoinWarmStorageServiceStateView.CallOpts)
}

// GetMaxProvingPeriod is a free data retrieval call binding the contract method 0xf2f12333.
//
// Solidity: function getMaxProvingPeriod() view returns(uint64)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) GetMaxProvingPeriod() (uint64, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetMaxProvingPeriod(&_FilecoinWarmStorageServiceStateView.CallOpts)
}

// GetPDPConfig is a free data retrieval call binding the contract method 0xea0f9354.
//
// Solidity: function getPDPConfig() view returns(uint64 maxProvingPeriod, uint256 challengeWindowSize, uint256 challengesPerProof, uint256 initChallengeWindowStart)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) GetPDPConfig(opts *bind.CallOpts) (struct {
	MaxProvingPeriod         uint64
	ChallengeWindowSize      *big.Int
	ChallengesPerProof       *big.Int
	InitChallengeWindowStart *big.Int
}, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "getPDPConfig")

	outstruct := new(struct {
		MaxProvingPeriod         uint64
		ChallengeWindowSize      *big.Int
		ChallengesPerProof       *big.Int
		InitChallengeWindowStart *big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.MaxProvingPeriod = *abi.ConvertType(out[0], new(uint64)).(*uint64)
	outstruct.ChallengeWindowSize = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	outstruct.ChallengesPerProof = *abi.ConvertType(out[2], new(*big.Int)).(**big.Int)
	outstruct.InitChallengeWindowStart = *abi.ConvertType(out[3], new(*big.Int)).(**big.Int)

	return *outstruct, err

}

// GetPDPConfig is a free data retrieval call binding the contract method 0xea0f9354.
//
// Solidity: function getPDPConfig() view returns(uint64 maxProvingPeriod, uint256 challengeWindowSize, uint256 challengesPerProof, uint256 initChallengeWindowStart)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) GetPDPConfig() (struct {
	MaxProvingPeriod         uint64
	ChallengeWindowSize      *big.Int
	ChallengesPerProof       *big.Int
	InitChallengeWindowStart *big.Int
}, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetPDPConfig(&_FilecoinWarmStorageServiceStateView.CallOpts)
}

// GetPDPConfig is a free data retrieval call binding the contract method 0xea0f9354.
//
// Solidity: function getPDPConfig() view returns(uint64 maxProvingPeriod, uint256 challengeWindowSize, uint256 challengesPerProof, uint256 initChallengeWindowStart)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) GetPDPConfig() (struct {
	MaxProvingPeriod         uint64
	ChallengeWindowSize      *big.Int
	ChallengesPerProof       *big.Int
	InitChallengeWindowStart *big.Int
}, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetPDPConfig(&_FilecoinWarmStorageServiceStateView.CallOpts)
}

// GetPieceMetadata is a free data retrieval call binding the contract method 0x837a7f49.
//
// Solidity: function getPieceMetadata(uint256 dataSetId, uint256 pieceId, string key) view returns(bool exists, string value)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) GetPieceMetadata(opts *bind.CallOpts, dataSetId *big.Int, pieceId *big.Int, key string) (struct {
	Exists bool
	Value  string
}, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "getPieceMetadata", dataSetId, pieceId, key)

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

// GetPieceMetadata is a free data retrieval call binding the contract method 0x837a7f49.
//
// Solidity: function getPieceMetadata(uint256 dataSetId, uint256 pieceId, string key) view returns(bool exists, string value)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) GetPieceMetadata(dataSetId *big.Int, pieceId *big.Int, key string) (struct {
	Exists bool
	Value  string
}, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetPieceMetadata(&_FilecoinWarmStorageServiceStateView.CallOpts, dataSetId, pieceId, key)
}

// GetPieceMetadata is a free data retrieval call binding the contract method 0x837a7f49.
//
// Solidity: function getPieceMetadata(uint256 dataSetId, uint256 pieceId, string key) view returns(bool exists, string value)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) GetPieceMetadata(dataSetId *big.Int, pieceId *big.Int, key string) (struct {
	Exists bool
	Value  string
}, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.GetPieceMetadata(&_FilecoinWarmStorageServiceStateView.CallOpts, dataSetId, pieceId, key)
}

// IsProviderApproved is a free data retrieval call binding the contract method 0xb6133b7a.
//
// Solidity: function isProviderApproved(uint256 providerId) view returns(bool)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) IsProviderApproved(opts *bind.CallOpts, providerId *big.Int) (bool, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "isProviderApproved", providerId)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsProviderApproved is a free data retrieval call binding the contract method 0xb6133b7a.
//
// Solidity: function isProviderApproved(uint256 providerId) view returns(bool)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) IsProviderApproved(providerId *big.Int) (bool, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.IsProviderApproved(&_FilecoinWarmStorageServiceStateView.CallOpts, providerId)
}

// IsProviderApproved is a free data retrieval call binding the contract method 0xb6133b7a.
//
// Solidity: function isProviderApproved(uint256 providerId) view returns(bool)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) IsProviderApproved(providerId *big.Int) (bool, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.IsProviderApproved(&_FilecoinWarmStorageServiceStateView.CallOpts, providerId)
}

// NextPDPChallengeWindowStart is a free data retrieval call binding the contract method 0x11d41294.
//
// Solidity: function nextPDPChallengeWindowStart(uint256 setId) view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) NextPDPChallengeWindowStart(opts *bind.CallOpts, setId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "nextPDPChallengeWindowStart", setId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// NextPDPChallengeWindowStart is a free data retrieval call binding the contract method 0x11d41294.
//
// Solidity: function nextPDPChallengeWindowStart(uint256 setId) view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) NextPDPChallengeWindowStart(setId *big.Int) (*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.NextPDPChallengeWindowStart(&_FilecoinWarmStorageServiceStateView.CallOpts, setId)
}

// NextPDPChallengeWindowStart is a free data retrieval call binding the contract method 0x11d41294.
//
// Solidity: function nextPDPChallengeWindowStart(uint256 setId) view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) NextPDPChallengeWindowStart(setId *big.Int) (*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.NextPDPChallengeWindowStart(&_FilecoinWarmStorageServiceStateView.CallOpts, setId)
}

// ProvenPeriods is a free data retrieval call binding the contract method 0x698762cb.
//
// Solidity: function provenPeriods(uint256 dataSetId, uint256 periodId) view returns(bool)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) ProvenPeriods(opts *bind.CallOpts, dataSetId *big.Int, periodId *big.Int) (bool, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "provenPeriods", dataSetId, periodId)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// ProvenPeriods is a free data retrieval call binding the contract method 0x698762cb.
//
// Solidity: function provenPeriods(uint256 dataSetId, uint256 periodId) view returns(bool)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) ProvenPeriods(dataSetId *big.Int, periodId *big.Int) (bool, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.ProvenPeriods(&_FilecoinWarmStorageServiceStateView.CallOpts, dataSetId, periodId)
}

// ProvenPeriods is a free data retrieval call binding the contract method 0x698762cb.
//
// Solidity: function provenPeriods(uint256 dataSetId, uint256 periodId) view returns(bool)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) ProvenPeriods(dataSetId *big.Int, periodId *big.Int) (bool, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.ProvenPeriods(&_FilecoinWarmStorageServiceStateView.CallOpts, dataSetId, periodId)
}

// ProvenThisPeriod is a free data retrieval call binding the contract method 0x7598a1cd.
//
// Solidity: function provenThisPeriod(uint256 dataSetId) view returns(bool)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) ProvenThisPeriod(opts *bind.CallOpts, dataSetId *big.Int) (bool, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "provenThisPeriod", dataSetId)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// ProvenThisPeriod is a free data retrieval call binding the contract method 0x7598a1cd.
//
// Solidity: function provenThisPeriod(uint256 dataSetId) view returns(bool)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) ProvenThisPeriod(dataSetId *big.Int) (bool, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.ProvenThisPeriod(&_FilecoinWarmStorageServiceStateView.CallOpts, dataSetId)
}

// ProvenThisPeriod is a free data retrieval call binding the contract method 0x7598a1cd.
//
// Solidity: function provenThisPeriod(uint256 dataSetId) view returns(bool)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) ProvenThisPeriod(dataSetId *big.Int) (bool, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.ProvenThisPeriod(&_FilecoinWarmStorageServiceStateView.CallOpts, dataSetId)
}

// ProvingActivationEpoch is a free data retrieval call binding the contract method 0x725e3216.
//
// Solidity: function provingActivationEpoch(uint256 dataSetId) view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) ProvingActivationEpoch(opts *bind.CallOpts, dataSetId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "provingActivationEpoch", dataSetId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ProvingActivationEpoch is a free data retrieval call binding the contract method 0x725e3216.
//
// Solidity: function provingActivationEpoch(uint256 dataSetId) view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) ProvingActivationEpoch(dataSetId *big.Int) (*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.ProvingActivationEpoch(&_FilecoinWarmStorageServiceStateView.CallOpts, dataSetId)
}

// ProvingActivationEpoch is a free data retrieval call binding the contract method 0x725e3216.
//
// Solidity: function provingActivationEpoch(uint256 dataSetId) view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) ProvingActivationEpoch(dataSetId *big.Int) (*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.ProvingActivationEpoch(&_FilecoinWarmStorageServiceStateView.CallOpts, dataSetId)
}

// ProvingDeadline is a free data retrieval call binding the contract method 0x149ac5cc.
//
// Solidity: function provingDeadline(uint256 setId) view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) ProvingDeadline(opts *bind.CallOpts, setId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "provingDeadline", setId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ProvingDeadline is a free data retrieval call binding the contract method 0x149ac5cc.
//
// Solidity: function provingDeadline(uint256 setId) view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) ProvingDeadline(setId *big.Int) (*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.ProvingDeadline(&_FilecoinWarmStorageServiceStateView.CallOpts, setId)
}

// ProvingDeadline is a free data retrieval call binding the contract method 0x149ac5cc.
//
// Solidity: function provingDeadline(uint256 setId) view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) ProvingDeadline(setId *big.Int) (*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.ProvingDeadline(&_FilecoinWarmStorageServiceStateView.CallOpts, setId)
}

// RailToDataSet is a free data retrieval call binding the contract method 0x2ad6e6b5.
//
// Solidity: function railToDataSet(uint256 railId) view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) RailToDataSet(opts *bind.CallOpts, railId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "railToDataSet", railId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// RailToDataSet is a free data retrieval call binding the contract method 0x2ad6e6b5.
//
// Solidity: function railToDataSet(uint256 railId) view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) RailToDataSet(railId *big.Int) (*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.RailToDataSet(&_FilecoinWarmStorageServiceStateView.CallOpts, railId)
}

// RailToDataSet is a free data retrieval call binding the contract method 0x2ad6e6b5.
//
// Solidity: function railToDataSet(uint256 railId) view returns(uint256)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) RailToDataSet(railId *big.Int) (*big.Int, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.RailToDataSet(&_FilecoinWarmStorageServiceStateView.CallOpts, railId)
}

// Service is a free data retrieval call binding the contract method 0xd598d4c9.
//
// Solidity: function service() view returns(address)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCaller) Service(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FilecoinWarmStorageServiceStateView.contract.Call(opts, &out, "service")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Service is a free data retrieval call binding the contract method 0xd598d4c9.
//
// Solidity: function service() view returns(address)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewSession) Service() (common.Address, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.Service(&_FilecoinWarmStorageServiceStateView.CallOpts)
}

// Service is a free data retrieval call binding the contract method 0xd598d4c9.
//
// Solidity: function service() view returns(address)
func (_FilecoinWarmStorageServiceStateView *FilecoinWarmStorageServiceStateViewCallerSession) Service() (common.Address, error) {
	return _FilecoinWarmStorageServiceStateView.Contract.Service(&_FilecoinWarmStorageServiceStateView.CallOpts)
}
