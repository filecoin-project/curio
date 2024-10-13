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

// PDPRecordKeeperEventRecord is an auto generated low-level Go binding around an user-defined struct.
type PDPRecordKeeperEventRecord struct {
	Epoch         uint64
	OperationType uint8
	ExtraData     []byte
}

// PDPRecordKeeperMetaData contains all meta data concerning the PDPRecordKeeper contract.
var PDPRecordKeeperMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_pdpServiceAddress\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"proofSetId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"indexed\":false,\"internalType\":\"enumPDPRecordKeeper.OperationType\",\"name\":\"operationType\",\"type\":\"uint8\"}],\"name\":\"RecordAdded\",\"type\":\"event\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"proofSetId\",\"type\":\"uint256\"},{\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"internalType\":\"enumPDPRecordKeeper.OperationType\",\"name\":\"operationType\",\"type\":\"uint8\"},{\"internalType\":\"bytes\",\"name\":\"extraData\",\"type\":\"bytes\"}],\"name\":\"addRecord\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"proofSetId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"eventIndex\",\"type\":\"uint256\"}],\"name\":\"getEvent\",\"outputs\":[{\"components\":[{\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"internalType\":\"enumPDPRecordKeeper.OperationType\",\"name\":\"operationType\",\"type\":\"uint8\"},{\"internalType\":\"bytes\",\"name\":\"extraData\",\"type\":\"bytes\"}],\"internalType\":\"structPDPRecordKeeper.EventRecord\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"proofSetId\",\"type\":\"uint256\"}],\"name\":\"getEventCount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"proofSetId\",\"type\":\"uint256\"}],\"name\":\"listEvents\",\"outputs\":[{\"components\":[{\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"internalType\":\"enumPDPRecordKeeper.OperationType\",\"name\":\"operationType\",\"type\":\"uint8\"},{\"internalType\":\"bytes\",\"name\":\"extraData\",\"type\":\"bytes\"}],\"internalType\":\"structPDPRecordKeeper.EventRecord[]\",\"name\":\"\",\"type\":\"tuple[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"pdpServiceAddress\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"proofSetEvents\",\"outputs\":[{\"internalType\":\"uint64\",\"name\":\"epoch\",\"type\":\"uint64\"},{\"internalType\":\"enumPDPRecordKeeper.OperationType\",\"name\":\"operationType\",\"type\":\"uint8\"},{\"internalType\":\"bytes\",\"name\":\"extraData\",\"type\":\"bytes\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
}

// PDPRecordKeeperABI is the input ABI used to generate the binding from.
// Deprecated: Use PDPRecordKeeperMetaData.ABI instead.
var PDPRecordKeeperABI = PDPRecordKeeperMetaData.ABI

// PDPRecordKeeper is an auto generated Go binding around an Ethereum contract.
type PDPRecordKeeper struct {
	PDPRecordKeeperCaller     // Read-only binding to the contract
	PDPRecordKeeperTransactor // Write-only binding to the contract
	PDPRecordKeeperFilterer   // Log filterer for contract events
}

// PDPRecordKeeperCaller is an auto generated read-only Go binding around an Ethereum contract.
type PDPRecordKeeperCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PDPRecordKeeperTransactor is an auto generated write-only Go binding around an Ethereum contract.
type PDPRecordKeeperTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PDPRecordKeeperFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type PDPRecordKeeperFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PDPRecordKeeperSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type PDPRecordKeeperSession struct {
	Contract     *PDPRecordKeeper  // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// PDPRecordKeeperCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type PDPRecordKeeperCallerSession struct {
	Contract *PDPRecordKeeperCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts          // Call options to use throughout this session
}

// PDPRecordKeeperTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type PDPRecordKeeperTransactorSession struct {
	Contract     *PDPRecordKeeperTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts          // Transaction auth options to use throughout this session
}

// PDPRecordKeeperRaw is an auto generated low-level Go binding around an Ethereum contract.
type PDPRecordKeeperRaw struct {
	Contract *PDPRecordKeeper // Generic contract binding to access the raw methods on
}

// PDPRecordKeeperCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type PDPRecordKeeperCallerRaw struct {
	Contract *PDPRecordKeeperCaller // Generic read-only contract binding to access the raw methods on
}

// PDPRecordKeeperTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type PDPRecordKeeperTransactorRaw struct {
	Contract *PDPRecordKeeperTransactor // Generic write-only contract binding to access the raw methods on
}

// NewPDPRecordKeeper creates a new instance of PDPRecordKeeper, bound to a specific deployed contract.
func NewPDPRecordKeeper(address common.Address, backend bind.ContractBackend) (*PDPRecordKeeper, error) {
	contract, err := bindPDPRecordKeeper(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &PDPRecordKeeper{PDPRecordKeeperCaller: PDPRecordKeeperCaller{contract: contract}, PDPRecordKeeperTransactor: PDPRecordKeeperTransactor{contract: contract}, PDPRecordKeeperFilterer: PDPRecordKeeperFilterer{contract: contract}}, nil
}

// NewPDPRecordKeeperCaller creates a new read-only instance of PDPRecordKeeper, bound to a specific deployed contract.
func NewPDPRecordKeeperCaller(address common.Address, caller bind.ContractCaller) (*PDPRecordKeeperCaller, error) {
	contract, err := bindPDPRecordKeeper(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &PDPRecordKeeperCaller{contract: contract}, nil
}

// NewPDPRecordKeeperTransactor creates a new write-only instance of PDPRecordKeeper, bound to a specific deployed contract.
func NewPDPRecordKeeperTransactor(address common.Address, transactor bind.ContractTransactor) (*PDPRecordKeeperTransactor, error) {
	contract, err := bindPDPRecordKeeper(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &PDPRecordKeeperTransactor{contract: contract}, nil
}

// NewPDPRecordKeeperFilterer creates a new log filterer instance of PDPRecordKeeper, bound to a specific deployed contract.
func NewPDPRecordKeeperFilterer(address common.Address, filterer bind.ContractFilterer) (*PDPRecordKeeperFilterer, error) {
	contract, err := bindPDPRecordKeeper(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &PDPRecordKeeperFilterer{contract: contract}, nil
}

// bindPDPRecordKeeper binds a generic wrapper to an already deployed contract.
func bindPDPRecordKeeper(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := PDPRecordKeeperMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_PDPRecordKeeper *PDPRecordKeeperRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _PDPRecordKeeper.Contract.PDPRecordKeeperCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_PDPRecordKeeper *PDPRecordKeeperRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _PDPRecordKeeper.Contract.PDPRecordKeeperTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_PDPRecordKeeper *PDPRecordKeeperRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _PDPRecordKeeper.Contract.PDPRecordKeeperTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_PDPRecordKeeper *PDPRecordKeeperCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _PDPRecordKeeper.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_PDPRecordKeeper *PDPRecordKeeperTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _PDPRecordKeeper.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_PDPRecordKeeper *PDPRecordKeeperTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _PDPRecordKeeper.Contract.contract.Transact(opts, method, params...)
}

// GetEvent is a free data retrieval call binding the contract method 0xee11f1a0.
//
// Solidity: function getEvent(uint256 proofSetId, uint256 eventIndex) view returns((uint64,uint8,bytes))
func (_PDPRecordKeeper *PDPRecordKeeperCaller) GetEvent(opts *bind.CallOpts, proofSetId *big.Int, eventIndex *big.Int) (PDPRecordKeeperEventRecord, error) {
	var out []interface{}
	err := _PDPRecordKeeper.contract.Call(opts, &out, "getEvent", proofSetId, eventIndex)

	if err != nil {
		return *new(PDPRecordKeeperEventRecord), err
	}

	out0 := *abi.ConvertType(out[0], new(PDPRecordKeeperEventRecord)).(*PDPRecordKeeperEventRecord)

	return out0, err

}

// GetEvent is a free data retrieval call binding the contract method 0xee11f1a0.
//
// Solidity: function getEvent(uint256 proofSetId, uint256 eventIndex) view returns((uint64,uint8,bytes))
func (_PDPRecordKeeper *PDPRecordKeeperSession) GetEvent(proofSetId *big.Int, eventIndex *big.Int) (PDPRecordKeeperEventRecord, error) {
	return _PDPRecordKeeper.Contract.GetEvent(&_PDPRecordKeeper.CallOpts, proofSetId, eventIndex)
}

// GetEvent is a free data retrieval call binding the contract method 0xee11f1a0.
//
// Solidity: function getEvent(uint256 proofSetId, uint256 eventIndex) view returns((uint64,uint8,bytes))
func (_PDPRecordKeeper *PDPRecordKeeperCallerSession) GetEvent(proofSetId *big.Int, eventIndex *big.Int) (PDPRecordKeeperEventRecord, error) {
	return _PDPRecordKeeper.Contract.GetEvent(&_PDPRecordKeeper.CallOpts, proofSetId, eventIndex)
}

// GetEventCount is a free data retrieval call binding the contract method 0x1c2eaa9c.
//
// Solidity: function getEventCount(uint256 proofSetId) view returns(uint256)
func (_PDPRecordKeeper *PDPRecordKeeperCaller) GetEventCount(opts *bind.CallOpts, proofSetId *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _PDPRecordKeeper.contract.Call(opts, &out, "getEventCount", proofSetId)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetEventCount is a free data retrieval call binding the contract method 0x1c2eaa9c.
//
// Solidity: function getEventCount(uint256 proofSetId) view returns(uint256)
func (_PDPRecordKeeper *PDPRecordKeeperSession) GetEventCount(proofSetId *big.Int) (*big.Int, error) {
	return _PDPRecordKeeper.Contract.GetEventCount(&_PDPRecordKeeper.CallOpts, proofSetId)
}

// GetEventCount is a free data retrieval call binding the contract method 0x1c2eaa9c.
//
// Solidity: function getEventCount(uint256 proofSetId) view returns(uint256)
func (_PDPRecordKeeper *PDPRecordKeeperCallerSession) GetEventCount(proofSetId *big.Int) (*big.Int, error) {
	return _PDPRecordKeeper.Contract.GetEventCount(&_PDPRecordKeeper.CallOpts, proofSetId)
}

// ListEvents is a free data retrieval call binding the contract method 0xeaaa7cf0.
//
// Solidity: function listEvents(uint256 proofSetId) view returns((uint64,uint8,bytes)[])
func (_PDPRecordKeeper *PDPRecordKeeperCaller) ListEvents(opts *bind.CallOpts, proofSetId *big.Int) ([]PDPRecordKeeperEventRecord, error) {
	var out []interface{}
	err := _PDPRecordKeeper.contract.Call(opts, &out, "listEvents", proofSetId)

	if err != nil {
		return *new([]PDPRecordKeeperEventRecord), err
	}

	out0 := *abi.ConvertType(out[0], new([]PDPRecordKeeperEventRecord)).(*[]PDPRecordKeeperEventRecord)

	return out0, err

}

// ListEvents is a free data retrieval call binding the contract method 0xeaaa7cf0.
//
// Solidity: function listEvents(uint256 proofSetId) view returns((uint64,uint8,bytes)[])
func (_PDPRecordKeeper *PDPRecordKeeperSession) ListEvents(proofSetId *big.Int) ([]PDPRecordKeeperEventRecord, error) {
	return _PDPRecordKeeper.Contract.ListEvents(&_PDPRecordKeeper.CallOpts, proofSetId)
}

// ListEvents is a free data retrieval call binding the contract method 0xeaaa7cf0.
//
// Solidity: function listEvents(uint256 proofSetId) view returns((uint64,uint8,bytes)[])
func (_PDPRecordKeeper *PDPRecordKeeperCallerSession) ListEvents(proofSetId *big.Int) ([]PDPRecordKeeperEventRecord, error) {
	return _PDPRecordKeeper.Contract.ListEvents(&_PDPRecordKeeper.CallOpts, proofSetId)
}

// PdpServiceAddress is a free data retrieval call binding the contract method 0xeb6e5b9f.
//
// Solidity: function pdpServiceAddress() view returns(address)
func (_PDPRecordKeeper *PDPRecordKeeperCaller) PdpServiceAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _PDPRecordKeeper.contract.Call(opts, &out, "pdpServiceAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// PdpServiceAddress is a free data retrieval call binding the contract method 0xeb6e5b9f.
//
// Solidity: function pdpServiceAddress() view returns(address)
func (_PDPRecordKeeper *PDPRecordKeeperSession) PdpServiceAddress() (common.Address, error) {
	return _PDPRecordKeeper.Contract.PdpServiceAddress(&_PDPRecordKeeper.CallOpts)
}

// PdpServiceAddress is a free data retrieval call binding the contract method 0xeb6e5b9f.
//
// Solidity: function pdpServiceAddress() view returns(address)
func (_PDPRecordKeeper *PDPRecordKeeperCallerSession) PdpServiceAddress() (common.Address, error) {
	return _PDPRecordKeeper.Contract.PdpServiceAddress(&_PDPRecordKeeper.CallOpts)
}

// ProofSetEvents is a free data retrieval call binding the contract method 0xbea96933.
//
// Solidity: function proofSetEvents(uint256 , uint256 ) view returns(uint64 epoch, uint8 operationType, bytes extraData)
func (_PDPRecordKeeper *PDPRecordKeeperCaller) ProofSetEvents(opts *bind.CallOpts, arg0 *big.Int, arg1 *big.Int) (struct {
	Epoch         uint64
	OperationType uint8
	ExtraData     []byte
}, error) {
	var out []interface{}
	err := _PDPRecordKeeper.contract.Call(opts, &out, "proofSetEvents", arg0, arg1)

	outstruct := new(struct {
		Epoch         uint64
		OperationType uint8
		ExtraData     []byte
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Epoch = *abi.ConvertType(out[0], new(uint64)).(*uint64)
	outstruct.OperationType = *abi.ConvertType(out[1], new(uint8)).(*uint8)
	outstruct.ExtraData = *abi.ConvertType(out[2], new([]byte)).(*[]byte)

	return *outstruct, err

}

// ProofSetEvents is a free data retrieval call binding the contract method 0xbea96933.
//
// Solidity: function proofSetEvents(uint256 , uint256 ) view returns(uint64 epoch, uint8 operationType, bytes extraData)
func (_PDPRecordKeeper *PDPRecordKeeperSession) ProofSetEvents(arg0 *big.Int, arg1 *big.Int) (struct {
	Epoch         uint64
	OperationType uint8
	ExtraData     []byte
}, error) {
	return _PDPRecordKeeper.Contract.ProofSetEvents(&_PDPRecordKeeper.CallOpts, arg0, arg1)
}

// ProofSetEvents is a free data retrieval call binding the contract method 0xbea96933.
//
// Solidity: function proofSetEvents(uint256 , uint256 ) view returns(uint64 epoch, uint8 operationType, bytes extraData)
func (_PDPRecordKeeper *PDPRecordKeeperCallerSession) ProofSetEvents(arg0 *big.Int, arg1 *big.Int) (struct {
	Epoch         uint64
	OperationType uint8
	ExtraData     []byte
}, error) {
	return _PDPRecordKeeper.Contract.ProofSetEvents(&_PDPRecordKeeper.CallOpts, arg0, arg1)
}

// AddRecord is a paid mutator transaction binding the contract method 0x0fc9ec1a.
//
// Solidity: function addRecord(uint256 proofSetId, uint64 epoch, uint8 operationType, bytes extraData) returns()
func (_PDPRecordKeeper *PDPRecordKeeperTransactor) AddRecord(opts *bind.TransactOpts, proofSetId *big.Int, epoch uint64, operationType uint8, extraData []byte) (*types.Transaction, error) {
	return _PDPRecordKeeper.contract.Transact(opts, "addRecord", proofSetId, epoch, operationType, extraData)
}

// AddRecord is a paid mutator transaction binding the contract method 0x0fc9ec1a.
//
// Solidity: function addRecord(uint256 proofSetId, uint64 epoch, uint8 operationType, bytes extraData) returns()
func (_PDPRecordKeeper *PDPRecordKeeperSession) AddRecord(proofSetId *big.Int, epoch uint64, operationType uint8, extraData []byte) (*types.Transaction, error) {
	return _PDPRecordKeeper.Contract.AddRecord(&_PDPRecordKeeper.TransactOpts, proofSetId, epoch, operationType, extraData)
}

// AddRecord is a paid mutator transaction binding the contract method 0x0fc9ec1a.
//
// Solidity: function addRecord(uint256 proofSetId, uint64 epoch, uint8 operationType, bytes extraData) returns()
func (_PDPRecordKeeper *PDPRecordKeeperTransactorSession) AddRecord(proofSetId *big.Int, epoch uint64, operationType uint8, extraData []byte) (*types.Transaction, error) {
	return _PDPRecordKeeper.Contract.AddRecord(&_PDPRecordKeeper.TransactOpts, proofSetId, epoch, operationType, extraData)
}

// PDPRecordKeeperRecordAddedIterator is returned from FilterRecordAdded and is used to iterate over the raw logs and unpacked data for RecordAdded events raised by the PDPRecordKeeper contract.
type PDPRecordKeeperRecordAddedIterator struct {
	Event *PDPRecordKeeperRecordAdded // Event containing the contract specifics and raw log

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
func (it *PDPRecordKeeperRecordAddedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PDPRecordKeeperRecordAdded)
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
		it.Event = new(PDPRecordKeeperRecordAdded)
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
func (it *PDPRecordKeeperRecordAddedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PDPRecordKeeperRecordAddedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PDPRecordKeeperRecordAdded represents a RecordAdded event raised by the PDPRecordKeeper contract.
type PDPRecordKeeperRecordAdded struct {
	ProofSetId    *big.Int
	Epoch         uint64
	OperationType uint8
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterRecordAdded is a free log retrieval operation binding the contract event 0xb3bcc997e36eca7b534a754795c8c1277f79fb6b97ac09855fb2c3f58a83cd3a.
//
// Solidity: event RecordAdded(uint256 indexed proofSetId, uint64 epoch, uint8 operationType)
func (_PDPRecordKeeper *PDPRecordKeeperFilterer) FilterRecordAdded(opts *bind.FilterOpts, proofSetId []*big.Int) (*PDPRecordKeeperRecordAddedIterator, error) {

	var proofSetIdRule []interface{}
	for _, proofSetIdItem := range proofSetId {
		proofSetIdRule = append(proofSetIdRule, proofSetIdItem)
	}

	logs, sub, err := _PDPRecordKeeper.contract.FilterLogs(opts, "RecordAdded", proofSetIdRule)
	if err != nil {
		return nil, err
	}
	return &PDPRecordKeeperRecordAddedIterator{contract: _PDPRecordKeeper.contract, event: "RecordAdded", logs: logs, sub: sub}, nil
}

// WatchRecordAdded is a free log subscription operation binding the contract event 0xb3bcc997e36eca7b534a754795c8c1277f79fb6b97ac09855fb2c3f58a83cd3a.
//
// Solidity: event RecordAdded(uint256 indexed proofSetId, uint64 epoch, uint8 operationType)
func (_PDPRecordKeeper *PDPRecordKeeperFilterer) WatchRecordAdded(opts *bind.WatchOpts, sink chan<- *PDPRecordKeeperRecordAdded, proofSetId []*big.Int) (event.Subscription, error) {

	var proofSetIdRule []interface{}
	for _, proofSetIdItem := range proofSetId {
		proofSetIdRule = append(proofSetIdRule, proofSetIdItem)
	}

	logs, sub, err := _PDPRecordKeeper.contract.WatchLogs(opts, "RecordAdded", proofSetIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PDPRecordKeeperRecordAdded)
				if err := _PDPRecordKeeper.contract.UnpackLog(event, "RecordAdded", log); err != nil {
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

// ParseRecordAdded is a log parse operation binding the contract event 0xb3bcc997e36eca7b534a754795c8c1277f79fb6b97ac09855fb2c3f58a83cd3a.
//
// Solidity: event RecordAdded(uint256 indexed proofSetId, uint64 epoch, uint8 operationType)
func (_PDPRecordKeeper *PDPRecordKeeperFilterer) ParseRecordAdded(log types.Log) (*PDPRecordKeeperRecordAdded, error) {
	event := new(PDPRecordKeeperRecordAdded)
	if err := _PDPRecordKeeper.contract.UnpackLog(event, "RecordAdded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
