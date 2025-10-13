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

// ServiceProviderRegistryStoragePDPOffering is an auto generated low-level Go binding around an user-defined struct.
type ServiceProviderRegistryStoragePDPOffering struct {
	ServiceURL                 string
	MinPieceSizeInBytes        *big.Int
	MaxPieceSizeInBytes        *big.Int
	IpniPiece                  bool
	IpniIpfs                   bool
	StoragePricePerTibPerMonth *big.Int
	MinProvingPeriodInEpochs   *big.Int
	Location                   string
	PaymentTokenAddress        common.Address
}

// ServiceProviderRegistryStoragePaginatedProviders is an auto generated low-level Go binding around an user-defined struct.
type ServiceProviderRegistryStoragePaginatedProviders struct {
	Providers []ServiceProviderRegistryStorageProviderWithProduct
	HasMore   bool
}

// ServiceProviderRegistryStorageProviderWithProduct is an auto generated low-level Go binding around an user-defined struct.
type ServiceProviderRegistryStorageProviderWithProduct struct {
	ProviderId   *big.Int
	ProviderInfo ServiceProviderRegistryStorageServiceProviderInfo
	Product      ServiceProviderRegistryStorageServiceProduct
}

// ServiceProviderRegistryStorageServiceProduct is an auto generated low-level Go binding around an user-defined struct.
type ServiceProviderRegistryStorageServiceProduct struct {
	ProductType    uint8
	ProductData    []byte
	CapabilityKeys []string
	IsActive       bool
}

// ServiceProviderRegistryStorageServiceProviderInfo is an auto generated low-level Go binding around an user-defined struct.
type ServiceProviderRegistryStorageServiceProviderInfo struct {
	ServiceProvider common.Address
	Payee           common.Address
	Name            string
	Description     string
	IsActive        bool
	ProviderId      *big.Int
}

// ServiceProviderRegistryMetaData contains all meta data concerning the ServiceProviderRegistry contract.
var ServiceProviderRegistryMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"BURN_ACTOR\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"MAX_CAPABILITIES\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"MAX_CAPABILITY_KEY_LENGTH\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"MAX_CAPABILITY_VALUE_LENGTH\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"REGISTRATION_FEE\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"UPGRADE_INTERFACE_VERSION\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"VERSION\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"activeProductTypeProviderCount\",\"inputs\":[{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"}],\"outputs\":[{\"name\":\"count\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"activeProviderCount\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"addProduct\",\"inputs\":[{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"},{\"name\":\"productData\",\"type\":\"bytes\",\"internalType\":\"bytes\"},{\"name\":\"capabilityKeys\",\"type\":\"string[]\",\"internalType\":\"string[]\"},{\"name\":\"capabilityValues\",\"type\":\"string[]\",\"internalType\":\"string[]\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"addressToProviderId\",\"inputs\":[{\"name\":\"providerAddress\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"decodePDPOffering\",\"inputs\":[{\"name\":\"data\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[{\"name\":\"\",\"type\":\"tuple\",\"internalType\":\"structServiceProviderRegistryStorage.PDPOffering\",\"components\":[{\"name\":\"serviceURL\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"minPieceSizeInBytes\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"maxPieceSizeInBytes\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"ipniPiece\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"ipniIpfs\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"storagePricePerTibPerMonth\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"minProvingPeriodInEpochs\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"location\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"paymentTokenAddress\",\"type\":\"address\",\"internalType\":\"contractIERC20\"}]}],\"stateMutability\":\"pure\"},{\"type\":\"function\",\"name\":\"eip712Domain\",\"inputs\":[],\"outputs\":[{\"name\":\"fields\",\"type\":\"bytes1\",\"internalType\":\"bytes1\"},{\"name\":\"name\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"version\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"chainId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"verifyingContract\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"salt\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"},{\"name\":\"extensions\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"encodePDPOffering\",\"inputs\":[{\"name\":\"pdpOffering\",\"type\":\"tuple\",\"internalType\":\"structServiceProviderRegistryStorage.PDPOffering\",\"components\":[{\"name\":\"serviceURL\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"minPieceSizeInBytes\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"maxPieceSizeInBytes\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"ipniPiece\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"ipniIpfs\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"storagePricePerTibPerMonth\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"minProvingPeriodInEpochs\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"location\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"paymentTokenAddress\",\"type\":\"address\",\"internalType\":\"contractIERC20\"}]}],\"outputs\":[{\"name\":\"\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"stateMutability\":\"pure\"},{\"type\":\"function\",\"name\":\"getActiveProvidersByProductType\",\"inputs\":[{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"},{\"name\":\"offset\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"limit\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"result\",\"type\":\"tuple\",\"internalType\":\"structServiceProviderRegistryStorage.PaginatedProviders\",\"components\":[{\"name\":\"providers\",\"type\":\"tuple[]\",\"internalType\":\"structServiceProviderRegistryStorage.ProviderWithProduct[]\",\"components\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"providerInfo\",\"type\":\"tuple\",\"internalType\":\"structServiceProviderRegistryStorage.ServiceProviderInfo\",\"components\":[{\"name\":\"serviceProvider\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"payee\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"name\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"description\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"isActive\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"name\":\"product\",\"type\":\"tuple\",\"internalType\":\"structServiceProviderRegistryStorage.ServiceProduct\",\"components\":[{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"},{\"name\":\"productData\",\"type\":\"bytes\",\"internalType\":\"bytes\"},{\"name\":\"capabilityKeys\",\"type\":\"string[]\",\"internalType\":\"string[]\"},{\"name\":\"isActive\",\"type\":\"bool\",\"internalType\":\"bool\"}]}]},{\"name\":\"hasMore\",\"type\":\"bool\",\"internalType\":\"bool\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getAllActiveProviders\",\"inputs\":[{\"name\":\"offset\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"limit\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"providerIds\",\"type\":\"uint256[]\",\"internalType\":\"uint256[]\"},{\"name\":\"hasMore\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getNextProviderId\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getPDPService\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"pdpOffering\",\"type\":\"tuple\",\"internalType\":\"structServiceProviderRegistryStorage.PDPOffering\",\"components\":[{\"name\":\"serviceURL\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"minPieceSizeInBytes\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"maxPieceSizeInBytes\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"ipniPiece\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"ipniIpfs\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"storagePricePerTibPerMonth\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"minProvingPeriodInEpochs\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"location\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"paymentTokenAddress\",\"type\":\"address\",\"internalType\":\"contractIERC20\"}]},{\"name\":\"capabilityKeys\",\"type\":\"string[]\",\"internalType\":\"string[]\"},{\"name\":\"isActive\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getProduct\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"}],\"outputs\":[{\"name\":\"productData\",\"type\":\"bytes\",\"internalType\":\"bytes\"},{\"name\":\"capabilityKeys\",\"type\":\"string[]\",\"internalType\":\"string[]\"},{\"name\":\"isActive\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getProductCapabilities\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"},{\"name\":\"keys\",\"type\":\"string[]\",\"internalType\":\"string[]\"}],\"outputs\":[{\"name\":\"exists\",\"type\":\"bool[]\",\"internalType\":\"bool[]\"},{\"name\":\"values\",\"type\":\"string[]\",\"internalType\":\"string[]\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getProductCapability\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"},{\"name\":\"key\",\"type\":\"string\",\"internalType\":\"string\"}],\"outputs\":[{\"name\":\"exists\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"value\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getProvider\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"info\",\"type\":\"tuple\",\"internalType\":\"structServiceProviderRegistryStorage.ServiceProviderInfo\",\"components\":[{\"name\":\"serviceProvider\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"payee\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"name\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"description\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"isActive\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getProviderByAddress\",\"inputs\":[{\"name\":\"providerAddress\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"info\",\"type\":\"tuple\",\"internalType\":\"structServiceProviderRegistryStorage.ServiceProviderInfo\",\"components\":[{\"name\":\"serviceProvider\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"payee\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"name\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"description\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"isActive\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getProviderCount\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getProviderIdByAddress\",\"inputs\":[{\"name\":\"providerAddress\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getProvidersByProductType\",\"inputs\":[{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"},{\"name\":\"offset\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"limit\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"result\",\"type\":\"tuple\",\"internalType\":\"structServiceProviderRegistryStorage.PaginatedProviders\",\"components\":[{\"name\":\"providers\",\"type\":\"tuple[]\",\"internalType\":\"structServiceProviderRegistryStorage.ProviderWithProduct[]\",\"components\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"providerInfo\",\"type\":\"tuple\",\"internalType\":\"structServiceProviderRegistryStorage.ServiceProviderInfo\",\"components\":[{\"name\":\"serviceProvider\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"payee\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"name\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"description\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"isActive\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"name\":\"product\",\"type\":\"tuple\",\"internalType\":\"structServiceProviderRegistryStorage.ServiceProduct\",\"components\":[{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"},{\"name\":\"productData\",\"type\":\"bytes\",\"internalType\":\"bytes\"},{\"name\":\"capabilityKeys\",\"type\":\"string[]\",\"internalType\":\"string[]\"},{\"name\":\"isActive\",\"type\":\"bool\",\"internalType\":\"bool\"}]}]},{\"name\":\"hasMore\",\"type\":\"bool\",\"internalType\":\"bool\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"initialize\",\"inputs\":[],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"isProviderActive\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"isRegisteredProvider\",\"inputs\":[{\"name\":\"provider\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"migrate\",\"inputs\":[{\"name\":\"newVersion\",\"type\":\"string\",\"internalType\":\"string\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"owner\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"productCapabilities\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"},{\"name\":\"key\",\"type\":\"string\",\"internalType\":\"string\"}],\"outputs\":[{\"name\":\"value\",\"type\":\"string\",\"internalType\":\"string\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"productTypeProviderCount\",\"inputs\":[{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"}],\"outputs\":[{\"name\":\"count\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"providerHasProduct\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"providerProducts\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"}],\"outputs\":[{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"},{\"name\":\"productData\",\"type\":\"bytes\",\"internalType\":\"bytes\"},{\"name\":\"isActive\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"providers\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"serviceProvider\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"payee\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"name\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"description\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"isActive\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"proxiableUUID\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"registerProvider\",\"inputs\":[{\"name\":\"payee\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"name\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"description\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"},{\"name\":\"productData\",\"type\":\"bytes\",\"internalType\":\"bytes\"},{\"name\":\"capabilityKeys\",\"type\":\"string[]\",\"internalType\":\"string[]\"},{\"name\":\"capabilityValues\",\"type\":\"string[]\",\"internalType\":\"string[]\"}],\"outputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"payable\"},{\"type\":\"function\",\"name\":\"removeProduct\",\"inputs\":[{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"removeProvider\",\"inputs\":[],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"renounceOwnership\",\"inputs\":[],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"transferOwnership\",\"inputs\":[{\"name\":\"newOwner\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"updatePDPServiceWithCapabilities\",\"inputs\":[{\"name\":\"pdpOffering\",\"type\":\"tuple\",\"internalType\":\"structServiceProviderRegistryStorage.PDPOffering\",\"components\":[{\"name\":\"serviceURL\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"minPieceSizeInBytes\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"maxPieceSizeInBytes\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"ipniPiece\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"ipniIpfs\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"storagePricePerTibPerMonth\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"minProvingPeriodInEpochs\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"location\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"paymentTokenAddress\",\"type\":\"address\",\"internalType\":\"contractIERC20\"}]},{\"name\":\"capabilityKeys\",\"type\":\"string[]\",\"internalType\":\"string[]\"},{\"name\":\"capabilityValues\",\"type\":\"string[]\",\"internalType\":\"string[]\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"updateProduct\",\"inputs\":[{\"name\":\"productType\",\"type\":\"uint8\",\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"},{\"name\":\"productData\",\"type\":\"bytes\",\"internalType\":\"bytes\"},{\"name\":\"capabilityKeys\",\"type\":\"string[]\",\"internalType\":\"string[]\"},{\"name\":\"capabilityValues\",\"type\":\"string[]\",\"internalType\":\"string[]\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"updateProviderInfo\",\"inputs\":[{\"name\":\"name\",\"type\":\"string\",\"internalType\":\"string\"},{\"name\":\"description\",\"type\":\"string\",\"internalType\":\"string\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"upgradeToAndCall\",\"inputs\":[{\"name\":\"newImplementation\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"data\",\"type\":\"bytes\",\"internalType\":\"bytes\"}],\"outputs\":[],\"stateMutability\":\"payable\"},{\"type\":\"event\",\"name\":\"ContractUpgraded\",\"inputs\":[{\"name\":\"version\",\"type\":\"string\",\"indexed\":false,\"internalType\":\"string\"},{\"name\":\"implementation\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"EIP712DomainChanged\",\"inputs\":[],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Initialized\",\"inputs\":[{\"name\":\"version\",\"type\":\"uint64\",\"indexed\":false,\"internalType\":\"uint64\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"OwnershipTransferred\",\"inputs\":[{\"name\":\"previousOwner\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"newOwner\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ProductAdded\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"productType\",\"type\":\"uint8\",\"indexed\":true,\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"},{\"name\":\"serviceUrl\",\"type\":\"string\",\"indexed\":false,\"internalType\":\"string\"},{\"name\":\"serviceProvider\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"},{\"name\":\"capabilityKeys\",\"type\":\"string[]\",\"indexed\":false,\"internalType\":\"string[]\"},{\"name\":\"capabilityValues\",\"type\":\"string[]\",\"indexed\":false,\"internalType\":\"string[]\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ProductRemoved\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"productType\",\"type\":\"uint8\",\"indexed\":true,\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ProductUpdated\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"productType\",\"type\":\"uint8\",\"indexed\":true,\"internalType\":\"enumServiceProviderRegistryStorage.ProductType\"},{\"name\":\"serviceUrl\",\"type\":\"string\",\"indexed\":false,\"internalType\":\"string\"},{\"name\":\"serviceProvider\",\"type\":\"address\",\"indexed\":false,\"internalType\":\"address\"},{\"name\":\"capabilityKeys\",\"type\":\"string[]\",\"indexed\":false,\"internalType\":\"string[]\"},{\"name\":\"capabilityValues\",\"type\":\"string[]\",\"indexed\":false,\"internalType\":\"string[]\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ProviderInfoUpdated\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ProviderRegistered\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"serviceProvider\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"payee\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ProviderRemoved\",\"inputs\":[{\"name\":\"providerId\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"Upgraded\",\"inputs\":[{\"name\":\"implementation\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"error\",\"name\":\"AddressEmptyCode\",\"inputs\":[{\"name\":\"target\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"ERC1967InvalidImplementation\",\"inputs\":[{\"name\":\"implementation\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"ERC1967NonPayable\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"FailedCall\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidInitialization\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"NotInitializing\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"OwnableInvalidOwner\",\"inputs\":[{\"name\":\"owner\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"OwnableUnauthorizedAccount\",\"inputs\":[{\"name\":\"account\",\"type\":\"address\",\"internalType\":\"address\"}]},{\"type\":\"error\",\"name\":\"UUPSUnauthorizedCallContext\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"UUPSUnsupportedProxiableUUID\",\"inputs\":[{\"name\":\"slot\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}]}]",
}

// ServiceProviderRegistryABI is the input ABI used to generate the binding from.
// Deprecated: Use ServiceProviderRegistryMetaData.ABI instead.
var ServiceProviderRegistryABI = ServiceProviderRegistryMetaData.ABI

// ServiceProviderRegistry is an auto generated Go binding around an Ethereum contract.
type ServiceProviderRegistry struct {
	ServiceProviderRegistryCaller     // Read-only binding to the contract
	ServiceProviderRegistryTransactor // Write-only binding to the contract
	ServiceProviderRegistryFilterer   // Log filterer for contract events
}

// ServiceProviderRegistryCaller is an auto generated read-only Go binding around an Ethereum contract.
type ServiceProviderRegistryCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ServiceProviderRegistryTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ServiceProviderRegistryTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ServiceProviderRegistryFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ServiceProviderRegistryFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ServiceProviderRegistrySession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ServiceProviderRegistrySession struct {
	Contract     *ServiceProviderRegistry // Generic contract binding to set the session for
	CallOpts     bind.CallOpts            // Call options to use throughout this session
	TransactOpts bind.TransactOpts        // Transaction auth options to use throughout this session
}

// ServiceProviderRegistryCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ServiceProviderRegistryCallerSession struct {
	Contract *ServiceProviderRegistryCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts                  // Call options to use throughout this session
}

// ServiceProviderRegistryTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ServiceProviderRegistryTransactorSession struct {
	Contract     *ServiceProviderRegistryTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts                  // Transaction auth options to use throughout this session
}

// ServiceProviderRegistryRaw is an auto generated low-level Go binding around an Ethereum contract.
type ServiceProviderRegistryRaw struct {
	Contract *ServiceProviderRegistry // Generic contract binding to access the raw methods on
}

// ServiceProviderRegistryCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ServiceProviderRegistryCallerRaw struct {
	Contract *ServiceProviderRegistryCaller // Generic read-only contract binding to access the raw methods on
}

// ServiceProviderRegistryTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ServiceProviderRegistryTransactorRaw struct {
	Contract *ServiceProviderRegistryTransactor // Generic write-only contract binding to access the raw methods on
}

// NewServiceProviderRegistry creates a new instance of ServiceProviderRegistry, bound to a specific deployed contract.
func NewServiceProviderRegistry(address common.Address, backend bind.ContractBackend) (*ServiceProviderRegistry, error) {
	contract, err := bindServiceProviderRegistry(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistry{ServiceProviderRegistryCaller: ServiceProviderRegistryCaller{contract: contract}, ServiceProviderRegistryTransactor: ServiceProviderRegistryTransactor{contract: contract}, ServiceProviderRegistryFilterer: ServiceProviderRegistryFilterer{contract: contract}}, nil
}

// NewServiceProviderRegistryCaller creates a new read-only instance of ServiceProviderRegistry, bound to a specific deployed contract.
func NewServiceProviderRegistryCaller(address common.Address, caller bind.ContractCaller) (*ServiceProviderRegistryCaller, error) {
	contract, err := bindServiceProviderRegistry(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryCaller{contract: contract}, nil
}

// NewServiceProviderRegistryTransactor creates a new write-only instance of ServiceProviderRegistry, bound to a specific deployed contract.
func NewServiceProviderRegistryTransactor(address common.Address, transactor bind.ContractTransactor) (*ServiceProviderRegistryTransactor, error) {
	contract, err := bindServiceProviderRegistry(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryTransactor{contract: contract}, nil
}

// NewServiceProviderRegistryFilterer creates a new log filterer instance of ServiceProviderRegistry, bound to a specific deployed contract.
func NewServiceProviderRegistryFilterer(address common.Address, filterer bind.ContractFilterer) (*ServiceProviderRegistryFilterer, error) {
	contract, err := bindServiceProviderRegistry(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryFilterer{contract: contract}, nil
}

// bindServiceProviderRegistry binds a generic wrapper to an already deployed contract.
func bindServiceProviderRegistry(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ServiceProviderRegistryMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ServiceProviderRegistry *ServiceProviderRegistryRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ServiceProviderRegistry.Contract.ServiceProviderRegistryCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ServiceProviderRegistry *ServiceProviderRegistryRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.ServiceProviderRegistryTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ServiceProviderRegistry *ServiceProviderRegistryRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.ServiceProviderRegistryTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ServiceProviderRegistry.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.contract.Transact(opts, method, params...)
}

// BURNACTOR is a free data retrieval call binding the contract method 0x0a6a63f1.
//
// Solidity: function BURN_ACTOR() view returns(address)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) BURNACTOR(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "BURN_ACTOR")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// BURNACTOR is a free data retrieval call binding the contract method 0x0a6a63f1.
//
// Solidity: function BURN_ACTOR() view returns(address)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) BURNACTOR() (common.Address, error) {
	return _ServiceProviderRegistry.Contract.BURNACTOR(&_ServiceProviderRegistry.CallOpts)
}

// BURNACTOR is a free data retrieval call binding the contract method 0x0a6a63f1.
//
// Solidity: function BURN_ACTOR() view returns(address)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) BURNACTOR() (common.Address, error) {
	return _ServiceProviderRegistry.Contract.BURNACTOR(&_ServiceProviderRegistry.CallOpts)
}

// MAXCAPABILITIES is a free data retrieval call binding the contract method 0x6e36e974.
//
// Solidity: function MAX_CAPABILITIES() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) MAXCAPABILITIES(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "MAX_CAPABILITIES")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MAXCAPABILITIES is a free data retrieval call binding the contract method 0x6e36e974.
//
// Solidity: function MAX_CAPABILITIES() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) MAXCAPABILITIES() (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.MAXCAPABILITIES(&_ServiceProviderRegistry.CallOpts)
}

// MAXCAPABILITIES is a free data retrieval call binding the contract method 0x6e36e974.
//
// Solidity: function MAX_CAPABILITIES() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) MAXCAPABILITIES() (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.MAXCAPABILITIES(&_ServiceProviderRegistry.CallOpts)
}

// MAXCAPABILITYKEYLENGTH is a free data retrieval call binding the contract method 0x7f657567.
//
// Solidity: function MAX_CAPABILITY_KEY_LENGTH() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) MAXCAPABILITYKEYLENGTH(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "MAX_CAPABILITY_KEY_LENGTH")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MAXCAPABILITYKEYLENGTH is a free data retrieval call binding the contract method 0x7f657567.
//
// Solidity: function MAX_CAPABILITY_KEY_LENGTH() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) MAXCAPABILITYKEYLENGTH() (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.MAXCAPABILITYKEYLENGTH(&_ServiceProviderRegistry.CallOpts)
}

// MAXCAPABILITYKEYLENGTH is a free data retrieval call binding the contract method 0x7f657567.
//
// Solidity: function MAX_CAPABILITY_KEY_LENGTH() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) MAXCAPABILITYKEYLENGTH() (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.MAXCAPABILITYKEYLENGTH(&_ServiceProviderRegistry.CallOpts)
}

// MAXCAPABILITYVALUELENGTH is a free data retrieval call binding the contract method 0xdcea1c6f.
//
// Solidity: function MAX_CAPABILITY_VALUE_LENGTH() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) MAXCAPABILITYVALUELENGTH(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "MAX_CAPABILITY_VALUE_LENGTH")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MAXCAPABILITYVALUELENGTH is a free data retrieval call binding the contract method 0xdcea1c6f.
//
// Solidity: function MAX_CAPABILITY_VALUE_LENGTH() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) MAXCAPABILITYVALUELENGTH() (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.MAXCAPABILITYVALUELENGTH(&_ServiceProviderRegistry.CallOpts)
}

// MAXCAPABILITYVALUELENGTH is a free data retrieval call binding the contract method 0xdcea1c6f.
//
// Solidity: function MAX_CAPABILITY_VALUE_LENGTH() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) MAXCAPABILITYVALUELENGTH() (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.MAXCAPABILITYVALUELENGTH(&_ServiceProviderRegistry.CallOpts)
}

// REGISTRATIONFEE is a free data retrieval call binding the contract method 0x64b4f751.
//
// Solidity: function REGISTRATION_FEE() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) REGISTRATIONFEE(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "REGISTRATION_FEE")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// REGISTRATIONFEE is a free data retrieval call binding the contract method 0x64b4f751.
//
// Solidity: function REGISTRATION_FEE() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) REGISTRATIONFEE() (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.REGISTRATIONFEE(&_ServiceProviderRegistry.CallOpts)
}

// REGISTRATIONFEE is a free data retrieval call binding the contract method 0x64b4f751.
//
// Solidity: function REGISTRATION_FEE() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) REGISTRATIONFEE() (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.REGISTRATIONFEE(&_ServiceProviderRegistry.CallOpts)
}

// UPGRADEINTERFACEVERSION is a free data retrieval call binding the contract method 0xad3cb1cc.
//
// Solidity: function UPGRADE_INTERFACE_VERSION() view returns(string)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) UPGRADEINTERFACEVERSION(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "UPGRADE_INTERFACE_VERSION")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// UPGRADEINTERFACEVERSION is a free data retrieval call binding the contract method 0xad3cb1cc.
//
// Solidity: function UPGRADE_INTERFACE_VERSION() view returns(string)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) UPGRADEINTERFACEVERSION() (string, error) {
	return _ServiceProviderRegistry.Contract.UPGRADEINTERFACEVERSION(&_ServiceProviderRegistry.CallOpts)
}

// UPGRADEINTERFACEVERSION is a free data retrieval call binding the contract method 0xad3cb1cc.
//
// Solidity: function UPGRADE_INTERFACE_VERSION() view returns(string)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) UPGRADEINTERFACEVERSION() (string, error) {
	return _ServiceProviderRegistry.Contract.UPGRADEINTERFACEVERSION(&_ServiceProviderRegistry.CallOpts)
}

// VERSION is a free data retrieval call binding the contract method 0xffa1ad74.
//
// Solidity: function VERSION() view returns(string)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) VERSION(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "VERSION")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// VERSION is a free data retrieval call binding the contract method 0xffa1ad74.
//
// Solidity: function VERSION() view returns(string)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) VERSION() (string, error) {
	return _ServiceProviderRegistry.Contract.VERSION(&_ServiceProviderRegistry.CallOpts)
}

// VERSION is a free data retrieval call binding the contract method 0xffa1ad74.
//
// Solidity: function VERSION() view returns(string)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) VERSION() (string, error) {
	return _ServiceProviderRegistry.Contract.VERSION(&_ServiceProviderRegistry.CallOpts)
}

// ActiveProductTypeProviderCount is a free data retrieval call binding the contract method 0x8bdc7747.
//
// Solidity: function activeProductTypeProviderCount(uint8 productType) view returns(uint256 count)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) ActiveProductTypeProviderCount(opts *bind.CallOpts, productType uint8) (*big.Int, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "activeProductTypeProviderCount", productType)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ActiveProductTypeProviderCount is a free data retrieval call binding the contract method 0x8bdc7747.
//
// Solidity: function activeProductTypeProviderCount(uint8 productType) view returns(uint256 count)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) ActiveProductTypeProviderCount(productType uint8) (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.ActiveProductTypeProviderCount(&_ServiceProviderRegistry.CallOpts, productType)
}

// ActiveProductTypeProviderCount is a free data retrieval call binding the contract method 0x8bdc7747.
//
// Solidity: function activeProductTypeProviderCount(uint8 productType) view returns(uint256 count)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) ActiveProductTypeProviderCount(productType uint8) (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.ActiveProductTypeProviderCount(&_ServiceProviderRegistry.CallOpts, productType)
}

// ActiveProviderCount is a free data retrieval call binding the contract method 0xf08bbda0.
//
// Solidity: function activeProviderCount() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) ActiveProviderCount(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "activeProviderCount")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ActiveProviderCount is a free data retrieval call binding the contract method 0xf08bbda0.
//
// Solidity: function activeProviderCount() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) ActiveProviderCount() (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.ActiveProviderCount(&_ServiceProviderRegistry.CallOpts)
}

// ActiveProviderCount is a free data retrieval call binding the contract method 0xf08bbda0.
//
// Solidity: function activeProviderCount() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) ActiveProviderCount() (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.ActiveProviderCount(&_ServiceProviderRegistry.CallOpts)
}

// AddressToProviderId is a free data retrieval call binding the contract method 0xe835440e.
//
// Solidity: function addressToProviderId(address providerAddress) view returns(uint256 providerId)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) AddressToProviderId(opts *bind.CallOpts, providerAddress common.Address) (*big.Int, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "addressToProviderId", providerAddress)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// AddressToProviderId is a free data retrieval call binding the contract method 0xe835440e.
//
// Solidity: function addressToProviderId(address providerAddress) view returns(uint256 providerId)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) AddressToProviderId(providerAddress common.Address) (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.AddressToProviderId(&_ServiceProviderRegistry.CallOpts, providerAddress)
}

// AddressToProviderId is a free data retrieval call binding the contract method 0xe835440e.
//
// Solidity: function addressToProviderId(address providerAddress) view returns(uint256 providerId)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) AddressToProviderId(providerAddress common.Address) (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.AddressToProviderId(&_ServiceProviderRegistry.CallOpts, providerAddress)
}

// DecodePDPOffering is a free data retrieval call binding the contract method 0xdeb0e462.
//
// Solidity: function decodePDPOffering(bytes data) pure returns((string,uint256,uint256,bool,bool,uint256,uint256,string,address))
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) DecodePDPOffering(opts *bind.CallOpts, data []byte) (ServiceProviderRegistryStoragePDPOffering, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "decodePDPOffering", data)

	if err != nil {
		return *new(ServiceProviderRegistryStoragePDPOffering), err
	}

	out0 := *abi.ConvertType(out[0], new(ServiceProviderRegistryStoragePDPOffering)).(*ServiceProviderRegistryStoragePDPOffering)

	return out0, err

}

// DecodePDPOffering is a free data retrieval call binding the contract method 0xdeb0e462.
//
// Solidity: function decodePDPOffering(bytes data) pure returns((string,uint256,uint256,bool,bool,uint256,uint256,string,address))
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) DecodePDPOffering(data []byte) (ServiceProviderRegistryStoragePDPOffering, error) {
	return _ServiceProviderRegistry.Contract.DecodePDPOffering(&_ServiceProviderRegistry.CallOpts, data)
}

// DecodePDPOffering is a free data retrieval call binding the contract method 0xdeb0e462.
//
// Solidity: function decodePDPOffering(bytes data) pure returns((string,uint256,uint256,bool,bool,uint256,uint256,string,address))
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) DecodePDPOffering(data []byte) (ServiceProviderRegistryStoragePDPOffering, error) {
	return _ServiceProviderRegistry.Contract.DecodePDPOffering(&_ServiceProviderRegistry.CallOpts, data)
}

// Eip712Domain is a free data retrieval call binding the contract method 0x84b0196e.
//
// Solidity: function eip712Domain() view returns(bytes1 fields, string name, string version, uint256 chainId, address verifyingContract, bytes32 salt, uint256[] extensions)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) Eip712Domain(opts *bind.CallOpts) (struct {
	Fields            [1]byte
	Name              string
	Version           string
	ChainId           *big.Int
	VerifyingContract common.Address
	Salt              [32]byte
	Extensions        []*big.Int
}, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "eip712Domain")

	outstruct := new(struct {
		Fields            [1]byte
		Name              string
		Version           string
		ChainId           *big.Int
		VerifyingContract common.Address
		Salt              [32]byte
		Extensions        []*big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Fields = *abi.ConvertType(out[0], new([1]byte)).(*[1]byte)
	outstruct.Name = *abi.ConvertType(out[1], new(string)).(*string)
	outstruct.Version = *abi.ConvertType(out[2], new(string)).(*string)
	outstruct.ChainId = *abi.ConvertType(out[3], new(*big.Int)).(**big.Int)
	outstruct.VerifyingContract = *abi.ConvertType(out[4], new(common.Address)).(*common.Address)
	outstruct.Salt = *abi.ConvertType(out[5], new([32]byte)).(*[32]byte)
	outstruct.Extensions = *abi.ConvertType(out[6], new([]*big.Int)).(*[]*big.Int)

	return *outstruct, err

}

// Eip712Domain is a free data retrieval call binding the contract method 0x84b0196e.
//
// Solidity: function eip712Domain() view returns(bytes1 fields, string name, string version, uint256 chainId, address verifyingContract, bytes32 salt, uint256[] extensions)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) Eip712Domain() (struct {
	Fields            [1]byte
	Name              string
	Version           string
	ChainId           *big.Int
	VerifyingContract common.Address
	Salt              [32]byte
	Extensions        []*big.Int
}, error) {
	return _ServiceProviderRegistry.Contract.Eip712Domain(&_ServiceProviderRegistry.CallOpts)
}

// Eip712Domain is a free data retrieval call binding the contract method 0x84b0196e.
//
// Solidity: function eip712Domain() view returns(bytes1 fields, string name, string version, uint256 chainId, address verifyingContract, bytes32 salt, uint256[] extensions)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) Eip712Domain() (struct {
	Fields            [1]byte
	Name              string
	Version           string
	ChainId           *big.Int
	VerifyingContract common.Address
	Salt              [32]byte
	Extensions        []*big.Int
}, error) {
	return _ServiceProviderRegistry.Contract.Eip712Domain(&_ServiceProviderRegistry.CallOpts)
}

// EncodePDPOffering is a free data retrieval call binding the contract method 0x82ee4b34.
//
// Solidity: function encodePDPOffering((string,uint256,uint256,bool,bool,uint256,uint256,string,address) pdpOffering) pure returns(bytes)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) EncodePDPOffering(opts *bind.CallOpts, pdpOffering ServiceProviderRegistryStoragePDPOffering) ([]byte, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "encodePDPOffering", pdpOffering)

	if err != nil {
		return *new([]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([]byte)).(*[]byte)

	return out0, err

}

// EncodePDPOffering is a free data retrieval call binding the contract method 0x82ee4b34.
//
// Solidity: function encodePDPOffering((string,uint256,uint256,bool,bool,uint256,uint256,string,address) pdpOffering) pure returns(bytes)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) EncodePDPOffering(pdpOffering ServiceProviderRegistryStoragePDPOffering) ([]byte, error) {
	return _ServiceProviderRegistry.Contract.EncodePDPOffering(&_ServiceProviderRegistry.CallOpts, pdpOffering)
}

// EncodePDPOffering is a free data retrieval call binding the contract method 0x82ee4b34.
//
// Solidity: function encodePDPOffering((string,uint256,uint256,bool,bool,uint256,uint256,string,address) pdpOffering) pure returns(bytes)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) EncodePDPOffering(pdpOffering ServiceProviderRegistryStoragePDPOffering) ([]byte, error) {
	return _ServiceProviderRegistry.Contract.EncodePDPOffering(&_ServiceProviderRegistry.CallOpts, pdpOffering)
}

// GetActiveProvidersByProductType is a free data retrieval call binding the contract method 0x213c63b1.
//
// Solidity: function getActiveProvidersByProductType(uint8 productType, uint256 offset, uint256 limit) view returns(((uint256,(address,address,string,string,bool,uint256),(uint8,bytes,string[],bool))[],bool) result)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) GetActiveProvidersByProductType(opts *bind.CallOpts, productType uint8, offset *big.Int, limit *big.Int) (ServiceProviderRegistryStoragePaginatedProviders, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "getActiveProvidersByProductType", productType, offset, limit)

	if err != nil {
		return *new(ServiceProviderRegistryStoragePaginatedProviders), err
	}

	out0 := *abi.ConvertType(out[0], new(ServiceProviderRegistryStoragePaginatedProviders)).(*ServiceProviderRegistryStoragePaginatedProviders)

	return out0, err

}

// GetActiveProvidersByProductType is a free data retrieval call binding the contract method 0x213c63b1.
//
// Solidity: function getActiveProvidersByProductType(uint8 productType, uint256 offset, uint256 limit) view returns(((uint256,(address,address,string,string,bool,uint256),(uint8,bytes,string[],bool))[],bool) result)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) GetActiveProvidersByProductType(productType uint8, offset *big.Int, limit *big.Int) (ServiceProviderRegistryStoragePaginatedProviders, error) {
	return _ServiceProviderRegistry.Contract.GetActiveProvidersByProductType(&_ServiceProviderRegistry.CallOpts, productType, offset, limit)
}

// GetActiveProvidersByProductType is a free data retrieval call binding the contract method 0x213c63b1.
//
// Solidity: function getActiveProvidersByProductType(uint8 productType, uint256 offset, uint256 limit) view returns(((uint256,(address,address,string,string,bool,uint256),(uint8,bytes,string[],bool))[],bool) result)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) GetActiveProvidersByProductType(productType uint8, offset *big.Int, limit *big.Int) (ServiceProviderRegistryStoragePaginatedProviders, error) {
	return _ServiceProviderRegistry.Contract.GetActiveProvidersByProductType(&_ServiceProviderRegistry.CallOpts, productType, offset, limit)
}

// GetAllActiveProviders is a free data retrieval call binding the contract method 0x2f67c065.
//
// Solidity: function getAllActiveProviders(uint256 offset, uint256 limit) view returns(uint256[] providerIds, bool hasMore)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) GetAllActiveProviders(opts *bind.CallOpts, offset *big.Int, limit *big.Int) (struct {
	ProviderIds []*big.Int
	HasMore     bool
}, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "getAllActiveProviders", offset, limit)

	outstruct := new(struct {
		ProviderIds []*big.Int
		HasMore     bool
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.ProviderIds = *abi.ConvertType(out[0], new([]*big.Int)).(*[]*big.Int)
	outstruct.HasMore = *abi.ConvertType(out[1], new(bool)).(*bool)

	return *outstruct, err

}

// GetAllActiveProviders is a free data retrieval call binding the contract method 0x2f67c065.
//
// Solidity: function getAllActiveProviders(uint256 offset, uint256 limit) view returns(uint256[] providerIds, bool hasMore)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) GetAllActiveProviders(offset *big.Int, limit *big.Int) (struct {
	ProviderIds []*big.Int
	HasMore     bool
}, error) {
	return _ServiceProviderRegistry.Contract.GetAllActiveProviders(&_ServiceProviderRegistry.CallOpts, offset, limit)
}

// GetAllActiveProviders is a free data retrieval call binding the contract method 0x2f67c065.
//
// Solidity: function getAllActiveProviders(uint256 offset, uint256 limit) view returns(uint256[] providerIds, bool hasMore)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) GetAllActiveProviders(offset *big.Int, limit *big.Int) (struct {
	ProviderIds []*big.Int
	HasMore     bool
}, error) {
	return _ServiceProviderRegistry.Contract.GetAllActiveProviders(&_ServiceProviderRegistry.CallOpts, offset, limit)
}

// GetNextProviderId is a free data retrieval call binding the contract method 0xd1329d4e.
//
// Solidity: function getNextProviderId() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) GetNextProviderId(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "getNextProviderId")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetNextProviderId is a free data retrieval call binding the contract method 0xd1329d4e.
//
// Solidity: function getNextProviderId() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) GetNextProviderId() (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.GetNextProviderId(&_ServiceProviderRegistry.CallOpts)
}

// GetNextProviderId is a free data retrieval call binding the contract method 0xd1329d4e.
//
// Solidity: function getNextProviderId() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) GetNextProviderId() (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.GetNextProviderId(&_ServiceProviderRegistry.CallOpts)
}

// GetPDPService is a free data retrieval call binding the contract method 0xc439fd57.
//
// Solidity: function getPDPService(uint256 providerId) view returns((string,uint256,uint256,bool,bool,uint256,uint256,string,address) pdpOffering, string[] capabilityKeys, bool isActive)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) GetPDPService(opts *bind.CallOpts, providerId *big.Int) (struct {
	PdpOffering    ServiceProviderRegistryStoragePDPOffering
	CapabilityKeys []string
	IsActive       bool
}, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "getPDPService", providerId)

	outstruct := new(struct {
		PdpOffering    ServiceProviderRegistryStoragePDPOffering
		CapabilityKeys []string
		IsActive       bool
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.PdpOffering = *abi.ConvertType(out[0], new(ServiceProviderRegistryStoragePDPOffering)).(*ServiceProviderRegistryStoragePDPOffering)
	outstruct.CapabilityKeys = *abi.ConvertType(out[1], new([]string)).(*[]string)
	outstruct.IsActive = *abi.ConvertType(out[2], new(bool)).(*bool)

	return *outstruct, err

}

// GetPDPService is a free data retrieval call binding the contract method 0xc439fd57.
//
// Solidity: function getPDPService(uint256 providerId) view returns((string,uint256,uint256,bool,bool,uint256,uint256,string,address) pdpOffering, string[] capabilityKeys, bool isActive)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) GetPDPService(providerId *big.Int) (struct {
	PdpOffering    ServiceProviderRegistryStoragePDPOffering
	CapabilityKeys []string
	IsActive       bool
}, error) {
	return _ServiceProviderRegistry.Contract.GetPDPService(&_ServiceProviderRegistry.CallOpts, providerId)
}

// GetPDPService is a free data retrieval call binding the contract method 0xc439fd57.
//
// Solidity: function getPDPService(uint256 providerId) view returns((string,uint256,uint256,bool,bool,uint256,uint256,string,address) pdpOffering, string[] capabilityKeys, bool isActive)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) GetPDPService(providerId *big.Int) (struct {
	PdpOffering    ServiceProviderRegistryStoragePDPOffering
	CapabilityKeys []string
	IsActive       bool
}, error) {
	return _ServiceProviderRegistry.Contract.GetPDPService(&_ServiceProviderRegistry.CallOpts, providerId)
}

// GetProduct is a free data retrieval call binding the contract method 0xaca0988f.
//
// Solidity: function getProduct(uint256 providerId, uint8 productType) view returns(bytes productData, string[] capabilityKeys, bool isActive)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) GetProduct(opts *bind.CallOpts, providerId *big.Int, productType uint8) (struct {
	ProductData    []byte
	CapabilityKeys []string
	IsActive       bool
}, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "getProduct", providerId, productType)

	outstruct := new(struct {
		ProductData    []byte
		CapabilityKeys []string
		IsActive       bool
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.ProductData = *abi.ConvertType(out[0], new([]byte)).(*[]byte)
	outstruct.CapabilityKeys = *abi.ConvertType(out[1], new([]string)).(*[]string)
	outstruct.IsActive = *abi.ConvertType(out[2], new(bool)).(*bool)

	return *outstruct, err

}

// GetProduct is a free data retrieval call binding the contract method 0xaca0988f.
//
// Solidity: function getProduct(uint256 providerId, uint8 productType) view returns(bytes productData, string[] capabilityKeys, bool isActive)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) GetProduct(providerId *big.Int, productType uint8) (struct {
	ProductData    []byte
	CapabilityKeys []string
	IsActive       bool
}, error) {
	return _ServiceProviderRegistry.Contract.GetProduct(&_ServiceProviderRegistry.CallOpts, providerId, productType)
}

// GetProduct is a free data retrieval call binding the contract method 0xaca0988f.
//
// Solidity: function getProduct(uint256 providerId, uint8 productType) view returns(bytes productData, string[] capabilityKeys, bool isActive)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) GetProduct(providerId *big.Int, productType uint8) (struct {
	ProductData    []byte
	CapabilityKeys []string
	IsActive       bool
}, error) {
	return _ServiceProviderRegistry.Contract.GetProduct(&_ServiceProviderRegistry.CallOpts, providerId, productType)
}

// GetProductCapabilities is a free data retrieval call binding the contract method 0xa6433240.
//
// Solidity: function getProductCapabilities(uint256 providerId, uint8 productType, string[] keys) view returns(bool[] exists, string[] values)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) GetProductCapabilities(opts *bind.CallOpts, providerId *big.Int, productType uint8, keys []string) (struct {
	Exists []bool
	Values []string
}, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "getProductCapabilities", providerId, productType, keys)

	outstruct := new(struct {
		Exists []bool
		Values []string
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.Exists = *abi.ConvertType(out[0], new([]bool)).(*[]bool)
	outstruct.Values = *abi.ConvertType(out[1], new([]string)).(*[]string)

	return *outstruct, err

}

// GetProductCapabilities is a free data retrieval call binding the contract method 0xa6433240.
//
// Solidity: function getProductCapabilities(uint256 providerId, uint8 productType, string[] keys) view returns(bool[] exists, string[] values)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) GetProductCapabilities(providerId *big.Int, productType uint8, keys []string) (struct {
	Exists []bool
	Values []string
}, error) {
	return _ServiceProviderRegistry.Contract.GetProductCapabilities(&_ServiceProviderRegistry.CallOpts, providerId, productType, keys)
}

// GetProductCapabilities is a free data retrieval call binding the contract method 0xa6433240.
//
// Solidity: function getProductCapabilities(uint256 providerId, uint8 productType, string[] keys) view returns(bool[] exists, string[] values)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) GetProductCapabilities(providerId *big.Int, productType uint8, keys []string) (struct {
	Exists []bool
	Values []string
}, error) {
	return _ServiceProviderRegistry.Contract.GetProductCapabilities(&_ServiceProviderRegistry.CallOpts, providerId, productType, keys)
}

// GetProductCapability is a free data retrieval call binding the contract method 0x1e35bdde.
//
// Solidity: function getProductCapability(uint256 providerId, uint8 productType, string key) view returns(bool exists, string value)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) GetProductCapability(opts *bind.CallOpts, providerId *big.Int, productType uint8, key string) (struct {
	Exists bool
	Value  string
}, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "getProductCapability", providerId, productType, key)

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

// GetProductCapability is a free data retrieval call binding the contract method 0x1e35bdde.
//
// Solidity: function getProductCapability(uint256 providerId, uint8 productType, string key) view returns(bool exists, string value)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) GetProductCapability(providerId *big.Int, productType uint8, key string) (struct {
	Exists bool
	Value  string
}, error) {
	return _ServiceProviderRegistry.Contract.GetProductCapability(&_ServiceProviderRegistry.CallOpts, providerId, productType, key)
}

// GetProductCapability is a free data retrieval call binding the contract method 0x1e35bdde.
//
// Solidity: function getProductCapability(uint256 providerId, uint8 productType, string key) view returns(bool exists, string value)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) GetProductCapability(providerId *big.Int, productType uint8, key string) (struct {
	Exists bool
	Value  string
}, error) {
	return _ServiceProviderRegistry.Contract.GetProductCapability(&_ServiceProviderRegistry.CallOpts, providerId, productType, key)
}

// GetProvider is a free data retrieval call binding the contract method 0x5c42d079.
//
// Solidity: function getProvider(uint256 providerId) view returns((address,address,string,string,bool,uint256) info)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) GetProvider(opts *bind.CallOpts, providerId *big.Int) (ServiceProviderRegistryStorageServiceProviderInfo, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "getProvider", providerId)

	if err != nil {
		return *new(ServiceProviderRegistryStorageServiceProviderInfo), err
	}

	out0 := *abi.ConvertType(out[0], new(ServiceProviderRegistryStorageServiceProviderInfo)).(*ServiceProviderRegistryStorageServiceProviderInfo)

	return out0, err

}

// GetProvider is a free data retrieval call binding the contract method 0x5c42d079.
//
// Solidity: function getProvider(uint256 providerId) view returns((address,address,string,string,bool,uint256) info)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) GetProvider(providerId *big.Int) (ServiceProviderRegistryStorageServiceProviderInfo, error) {
	return _ServiceProviderRegistry.Contract.GetProvider(&_ServiceProviderRegistry.CallOpts, providerId)
}

// GetProvider is a free data retrieval call binding the contract method 0x5c42d079.
//
// Solidity: function getProvider(uint256 providerId) view returns((address,address,string,string,bool,uint256) info)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) GetProvider(providerId *big.Int) (ServiceProviderRegistryStorageServiceProviderInfo, error) {
	return _ServiceProviderRegistry.Contract.GetProvider(&_ServiceProviderRegistry.CallOpts, providerId)
}

// GetProviderByAddress is a free data retrieval call binding the contract method 0x2335bde0.
//
// Solidity: function getProviderByAddress(address providerAddress) view returns((address,address,string,string,bool,uint256) info)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) GetProviderByAddress(opts *bind.CallOpts, providerAddress common.Address) (ServiceProviderRegistryStorageServiceProviderInfo, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "getProviderByAddress", providerAddress)

	if err != nil {
		return *new(ServiceProviderRegistryStorageServiceProviderInfo), err
	}

	out0 := *abi.ConvertType(out[0], new(ServiceProviderRegistryStorageServiceProviderInfo)).(*ServiceProviderRegistryStorageServiceProviderInfo)

	return out0, err

}

// GetProviderByAddress is a free data retrieval call binding the contract method 0x2335bde0.
//
// Solidity: function getProviderByAddress(address providerAddress) view returns((address,address,string,string,bool,uint256) info)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) GetProviderByAddress(providerAddress common.Address) (ServiceProviderRegistryStorageServiceProviderInfo, error) {
	return _ServiceProviderRegistry.Contract.GetProviderByAddress(&_ServiceProviderRegistry.CallOpts, providerAddress)
}

// GetProviderByAddress is a free data retrieval call binding the contract method 0x2335bde0.
//
// Solidity: function getProviderByAddress(address providerAddress) view returns((address,address,string,string,bool,uint256) info)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) GetProviderByAddress(providerAddress common.Address) (ServiceProviderRegistryStorageServiceProviderInfo, error) {
	return _ServiceProviderRegistry.Contract.GetProviderByAddress(&_ServiceProviderRegistry.CallOpts, providerAddress)
}

// GetProviderCount is a free data retrieval call binding the contract method 0x46ce4175.
//
// Solidity: function getProviderCount() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) GetProviderCount(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "getProviderCount")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetProviderCount is a free data retrieval call binding the contract method 0x46ce4175.
//
// Solidity: function getProviderCount() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) GetProviderCount() (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.GetProviderCount(&_ServiceProviderRegistry.CallOpts)
}

// GetProviderCount is a free data retrieval call binding the contract method 0x46ce4175.
//
// Solidity: function getProviderCount() view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) GetProviderCount() (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.GetProviderCount(&_ServiceProviderRegistry.CallOpts)
}

// GetProviderIdByAddress is a free data retrieval call binding the contract method 0x93ecb91e.
//
// Solidity: function getProviderIdByAddress(address providerAddress) view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) GetProviderIdByAddress(opts *bind.CallOpts, providerAddress common.Address) (*big.Int, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "getProviderIdByAddress", providerAddress)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetProviderIdByAddress is a free data retrieval call binding the contract method 0x93ecb91e.
//
// Solidity: function getProviderIdByAddress(address providerAddress) view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) GetProviderIdByAddress(providerAddress common.Address) (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.GetProviderIdByAddress(&_ServiceProviderRegistry.CallOpts, providerAddress)
}

// GetProviderIdByAddress is a free data retrieval call binding the contract method 0x93ecb91e.
//
// Solidity: function getProviderIdByAddress(address providerAddress) view returns(uint256)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) GetProviderIdByAddress(providerAddress common.Address) (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.GetProviderIdByAddress(&_ServiceProviderRegistry.CallOpts, providerAddress)
}

// GetProvidersByProductType is a free data retrieval call binding the contract method 0xfc260f7b.
//
// Solidity: function getProvidersByProductType(uint8 productType, uint256 offset, uint256 limit) view returns(((uint256,(address,address,string,string,bool,uint256),(uint8,bytes,string[],bool))[],bool) result)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) GetProvidersByProductType(opts *bind.CallOpts, productType uint8, offset *big.Int, limit *big.Int) (ServiceProviderRegistryStoragePaginatedProviders, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "getProvidersByProductType", productType, offset, limit)

	if err != nil {
		return *new(ServiceProviderRegistryStoragePaginatedProviders), err
	}

	out0 := *abi.ConvertType(out[0], new(ServiceProviderRegistryStoragePaginatedProviders)).(*ServiceProviderRegistryStoragePaginatedProviders)

	return out0, err

}

// GetProvidersByProductType is a free data retrieval call binding the contract method 0xfc260f7b.
//
// Solidity: function getProvidersByProductType(uint8 productType, uint256 offset, uint256 limit) view returns(((uint256,(address,address,string,string,bool,uint256),(uint8,bytes,string[],bool))[],bool) result)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) GetProvidersByProductType(productType uint8, offset *big.Int, limit *big.Int) (ServiceProviderRegistryStoragePaginatedProviders, error) {
	return _ServiceProviderRegistry.Contract.GetProvidersByProductType(&_ServiceProviderRegistry.CallOpts, productType, offset, limit)
}

// GetProvidersByProductType is a free data retrieval call binding the contract method 0xfc260f7b.
//
// Solidity: function getProvidersByProductType(uint8 productType, uint256 offset, uint256 limit) view returns(((uint256,(address,address,string,string,bool,uint256),(uint8,bytes,string[],bool))[],bool) result)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) GetProvidersByProductType(productType uint8, offset *big.Int, limit *big.Int) (ServiceProviderRegistryStoragePaginatedProviders, error) {
	return _ServiceProviderRegistry.Contract.GetProvidersByProductType(&_ServiceProviderRegistry.CallOpts, productType, offset, limit)
}

// IsProviderActive is a free data retrieval call binding the contract method 0x83df54a5.
//
// Solidity: function isProviderActive(uint256 providerId) view returns(bool)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) IsProviderActive(opts *bind.CallOpts, providerId *big.Int) (bool, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "isProviderActive", providerId)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsProviderActive is a free data retrieval call binding the contract method 0x83df54a5.
//
// Solidity: function isProviderActive(uint256 providerId) view returns(bool)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) IsProviderActive(providerId *big.Int) (bool, error) {
	return _ServiceProviderRegistry.Contract.IsProviderActive(&_ServiceProviderRegistry.CallOpts, providerId)
}

// IsProviderActive is a free data retrieval call binding the contract method 0x83df54a5.
//
// Solidity: function isProviderActive(uint256 providerId) view returns(bool)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) IsProviderActive(providerId *big.Int) (bool, error) {
	return _ServiceProviderRegistry.Contract.IsProviderActive(&_ServiceProviderRegistry.CallOpts, providerId)
}

// IsRegisteredProvider is a free data retrieval call binding the contract method 0x51ca236f.
//
// Solidity: function isRegisteredProvider(address provider) view returns(bool)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) IsRegisteredProvider(opts *bind.CallOpts, provider common.Address) (bool, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "isRegisteredProvider", provider)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsRegisteredProvider is a free data retrieval call binding the contract method 0x51ca236f.
//
// Solidity: function isRegisteredProvider(address provider) view returns(bool)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) IsRegisteredProvider(provider common.Address) (bool, error) {
	return _ServiceProviderRegistry.Contract.IsRegisteredProvider(&_ServiceProviderRegistry.CallOpts, provider)
}

// IsRegisteredProvider is a free data retrieval call binding the contract method 0x51ca236f.
//
// Solidity: function isRegisteredProvider(address provider) view returns(bool)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) IsRegisteredProvider(provider common.Address) (bool, error) {
	return _ServiceProviderRegistry.Contract.IsRegisteredProvider(&_ServiceProviderRegistry.CallOpts, provider)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) Owner() (common.Address, error) {
	return _ServiceProviderRegistry.Contract.Owner(&_ServiceProviderRegistry.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) Owner() (common.Address, error) {
	return _ServiceProviderRegistry.Contract.Owner(&_ServiceProviderRegistry.CallOpts)
}

// ProductCapabilities is a free data retrieval call binding the contract method 0x4368bafb.
//
// Solidity: function productCapabilities(uint256 providerId, uint8 productType, string key) view returns(string value)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) ProductCapabilities(opts *bind.CallOpts, providerId *big.Int, productType uint8, key string) (string, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "productCapabilities", providerId, productType, key)

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// ProductCapabilities is a free data retrieval call binding the contract method 0x4368bafb.
//
// Solidity: function productCapabilities(uint256 providerId, uint8 productType, string key) view returns(string value)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) ProductCapabilities(providerId *big.Int, productType uint8, key string) (string, error) {
	return _ServiceProviderRegistry.Contract.ProductCapabilities(&_ServiceProviderRegistry.CallOpts, providerId, productType, key)
}

// ProductCapabilities is a free data retrieval call binding the contract method 0x4368bafb.
//
// Solidity: function productCapabilities(uint256 providerId, uint8 productType, string key) view returns(string value)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) ProductCapabilities(providerId *big.Int, productType uint8, key string) (string, error) {
	return _ServiceProviderRegistry.Contract.ProductCapabilities(&_ServiceProviderRegistry.CallOpts, providerId, productType, key)
}

// ProductTypeProviderCount is a free data retrieval call binding the contract method 0xe459382f.
//
// Solidity: function productTypeProviderCount(uint8 productType) view returns(uint256 count)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) ProductTypeProviderCount(opts *bind.CallOpts, productType uint8) (*big.Int, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "productTypeProviderCount", productType)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ProductTypeProviderCount is a free data retrieval call binding the contract method 0xe459382f.
//
// Solidity: function productTypeProviderCount(uint8 productType) view returns(uint256 count)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) ProductTypeProviderCount(productType uint8) (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.ProductTypeProviderCount(&_ServiceProviderRegistry.CallOpts, productType)
}

// ProductTypeProviderCount is a free data retrieval call binding the contract method 0xe459382f.
//
// Solidity: function productTypeProviderCount(uint8 productType) view returns(uint256 count)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) ProductTypeProviderCount(productType uint8) (*big.Int, error) {
	return _ServiceProviderRegistry.Contract.ProductTypeProviderCount(&_ServiceProviderRegistry.CallOpts, productType)
}

// ProviderHasProduct is a free data retrieval call binding the contract method 0xcde24beb.
//
// Solidity: function providerHasProduct(uint256 providerId, uint8 productType) view returns(bool)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) ProviderHasProduct(opts *bind.CallOpts, providerId *big.Int, productType uint8) (bool, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "providerHasProduct", providerId, productType)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// ProviderHasProduct is a free data retrieval call binding the contract method 0xcde24beb.
//
// Solidity: function providerHasProduct(uint256 providerId, uint8 productType) view returns(bool)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) ProviderHasProduct(providerId *big.Int, productType uint8) (bool, error) {
	return _ServiceProviderRegistry.Contract.ProviderHasProduct(&_ServiceProviderRegistry.CallOpts, providerId, productType)
}

// ProviderHasProduct is a free data retrieval call binding the contract method 0xcde24beb.
//
// Solidity: function providerHasProduct(uint256 providerId, uint8 productType) view returns(bool)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) ProviderHasProduct(providerId *big.Int, productType uint8) (bool, error) {
	return _ServiceProviderRegistry.Contract.ProviderHasProduct(&_ServiceProviderRegistry.CallOpts, providerId, productType)
}

// ProviderProducts is a free data retrieval call binding the contract method 0x6bf6d74f.
//
// Solidity: function providerProducts(uint256 providerId, uint8 productType) view returns(uint8 productType, bytes productData, bool isActive)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) ProviderProducts(opts *bind.CallOpts, providerId *big.Int, productType uint8) (struct {
	ProductType uint8
	ProductData []byte
	IsActive    bool
}, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "providerProducts", providerId, productType)

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
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) ProviderProducts(providerId *big.Int, productType uint8) (struct {
	ProductType uint8
	ProductData []byte
	IsActive    bool
}, error) {
	return _ServiceProviderRegistry.Contract.ProviderProducts(&_ServiceProviderRegistry.CallOpts, providerId, productType)
}

// ProviderProducts is a free data retrieval call binding the contract method 0x6bf6d74f.
//
// Solidity: function providerProducts(uint256 providerId, uint8 productType) view returns(uint8 productType, bytes productData, bool isActive)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) ProviderProducts(providerId *big.Int, productType uint8) (struct {
	ProductType uint8
	ProductData []byte
	IsActive    bool
}, error) {
	return _ServiceProviderRegistry.Contract.ProviderProducts(&_ServiceProviderRegistry.CallOpts, providerId, productType)
}

// Providers is a free data retrieval call binding the contract method 0x50f3fc81.
//
// Solidity: function providers(uint256 providerId) view returns(address serviceProvider, address payee, string name, string description, bool isActive, uint256 providerId)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) Providers(opts *bind.CallOpts, providerId *big.Int) (struct {
	ServiceProvider common.Address
	Payee           common.Address
	Name            string
	Description     string
	IsActive        bool
	ProviderId      *big.Int
}, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "providers", providerId)

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
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) Providers(providerId *big.Int) (struct {
	ServiceProvider common.Address
	Payee           common.Address
	Name            string
	Description     string
	IsActive        bool
	ProviderId      *big.Int
}, error) {
	return _ServiceProviderRegistry.Contract.Providers(&_ServiceProviderRegistry.CallOpts, providerId)
}

// Providers is a free data retrieval call binding the contract method 0x50f3fc81.
//
// Solidity: function providers(uint256 providerId) view returns(address serviceProvider, address payee, string name, string description, bool isActive, uint256 providerId)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) Providers(providerId *big.Int) (struct {
	ServiceProvider common.Address
	Payee           common.Address
	Name            string
	Description     string
	IsActive        bool
	ProviderId      *big.Int
}, error) {
	return _ServiceProviderRegistry.Contract.Providers(&_ServiceProviderRegistry.CallOpts, providerId)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_ServiceProviderRegistry *ServiceProviderRegistryCaller) ProxiableUUID(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _ServiceProviderRegistry.contract.Call(opts, &out, "proxiableUUID")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) ProxiableUUID() ([32]byte, error) {
	return _ServiceProviderRegistry.Contract.ProxiableUUID(&_ServiceProviderRegistry.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_ServiceProviderRegistry *ServiceProviderRegistryCallerSession) ProxiableUUID() ([32]byte, error) {
	return _ServiceProviderRegistry.Contract.ProxiableUUID(&_ServiceProviderRegistry.CallOpts)
}

// AddProduct is a paid mutator transaction binding the contract method 0x02ff8437.
//
// Solidity: function addProduct(uint8 productType, bytes productData, string[] capabilityKeys, string[] capabilityValues) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactor) AddProduct(opts *bind.TransactOpts, productType uint8, productData []byte, capabilityKeys []string, capabilityValues []string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.contract.Transact(opts, "addProduct", productType, productData, capabilityKeys, capabilityValues)
}

// AddProduct is a paid mutator transaction binding the contract method 0x02ff8437.
//
// Solidity: function addProduct(uint8 productType, bytes productData, string[] capabilityKeys, string[] capabilityValues) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) AddProduct(productType uint8, productData []byte, capabilityKeys []string, capabilityValues []string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.AddProduct(&_ServiceProviderRegistry.TransactOpts, productType, productData, capabilityKeys, capabilityValues)
}

// AddProduct is a paid mutator transaction binding the contract method 0x02ff8437.
//
// Solidity: function addProduct(uint8 productType, bytes productData, string[] capabilityKeys, string[] capabilityValues) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactorSession) AddProduct(productType uint8, productData []byte, capabilityKeys []string, capabilityValues []string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.AddProduct(&_ServiceProviderRegistry.TransactOpts, productType, productData, capabilityKeys, capabilityValues)
}

// Initialize is a paid mutator transaction binding the contract method 0x8129fc1c.
//
// Solidity: function initialize() returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactor) Initialize(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ServiceProviderRegistry.contract.Transact(opts, "initialize")
}

// Initialize is a paid mutator transaction binding the contract method 0x8129fc1c.
//
// Solidity: function initialize() returns()
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) Initialize() (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.Initialize(&_ServiceProviderRegistry.TransactOpts)
}

// Initialize is a paid mutator transaction binding the contract method 0x8129fc1c.
//
// Solidity: function initialize() returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactorSession) Initialize() (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.Initialize(&_ServiceProviderRegistry.TransactOpts)
}

// Migrate is a paid mutator transaction binding the contract method 0xc9c5b5b4.
//
// Solidity: function migrate(string newVersion) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactor) Migrate(opts *bind.TransactOpts, newVersion string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.contract.Transact(opts, "migrate", newVersion)
}

// Migrate is a paid mutator transaction binding the contract method 0xc9c5b5b4.
//
// Solidity: function migrate(string newVersion) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) Migrate(newVersion string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.Migrate(&_ServiceProviderRegistry.TransactOpts, newVersion)
}

// Migrate is a paid mutator transaction binding the contract method 0xc9c5b5b4.
//
// Solidity: function migrate(string newVersion) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactorSession) Migrate(newVersion string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.Migrate(&_ServiceProviderRegistry.TransactOpts, newVersion)
}

// RegisterProvider is a paid mutator transaction binding the contract method 0xb47be8ab.
//
// Solidity: function registerProvider(address payee, string name, string description, uint8 productType, bytes productData, string[] capabilityKeys, string[] capabilityValues) payable returns(uint256 providerId)
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactor) RegisterProvider(opts *bind.TransactOpts, payee common.Address, name string, description string, productType uint8, productData []byte, capabilityKeys []string, capabilityValues []string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.contract.Transact(opts, "registerProvider", payee, name, description, productType, productData, capabilityKeys, capabilityValues)
}

// RegisterProvider is a paid mutator transaction binding the contract method 0xb47be8ab.
//
// Solidity: function registerProvider(address payee, string name, string description, uint8 productType, bytes productData, string[] capabilityKeys, string[] capabilityValues) payable returns(uint256 providerId)
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) RegisterProvider(payee common.Address, name string, description string, productType uint8, productData []byte, capabilityKeys []string, capabilityValues []string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.RegisterProvider(&_ServiceProviderRegistry.TransactOpts, payee, name, description, productType, productData, capabilityKeys, capabilityValues)
}

// RegisterProvider is a paid mutator transaction binding the contract method 0xb47be8ab.
//
// Solidity: function registerProvider(address payee, string name, string description, uint8 productType, bytes productData, string[] capabilityKeys, string[] capabilityValues) payable returns(uint256 providerId)
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactorSession) RegisterProvider(payee common.Address, name string, description string, productType uint8, productData []byte, capabilityKeys []string, capabilityValues []string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.RegisterProvider(&_ServiceProviderRegistry.TransactOpts, payee, name, description, productType, productData, capabilityKeys, capabilityValues)
}

// RemoveProduct is a paid mutator transaction binding the contract method 0xa9d239b6.
//
// Solidity: function removeProduct(uint8 productType) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactor) RemoveProduct(opts *bind.TransactOpts, productType uint8) (*types.Transaction, error) {
	return _ServiceProviderRegistry.contract.Transact(opts, "removeProduct", productType)
}

// RemoveProduct is a paid mutator transaction binding the contract method 0xa9d239b6.
//
// Solidity: function removeProduct(uint8 productType) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) RemoveProduct(productType uint8) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.RemoveProduct(&_ServiceProviderRegistry.TransactOpts, productType)
}

// RemoveProduct is a paid mutator transaction binding the contract method 0xa9d239b6.
//
// Solidity: function removeProduct(uint8 productType) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactorSession) RemoveProduct(productType uint8) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.RemoveProduct(&_ServiceProviderRegistry.TransactOpts, productType)
}

// RemoveProvider is a paid mutator transaction binding the contract method 0xb6363b99.
//
// Solidity: function removeProvider() returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactor) RemoveProvider(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ServiceProviderRegistry.contract.Transact(opts, "removeProvider")
}

// RemoveProvider is a paid mutator transaction binding the contract method 0xb6363b99.
//
// Solidity: function removeProvider() returns()
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) RemoveProvider() (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.RemoveProvider(&_ServiceProviderRegistry.TransactOpts)
}

// RemoveProvider is a paid mutator transaction binding the contract method 0xb6363b99.
//
// Solidity: function removeProvider() returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactorSession) RemoveProvider() (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.RemoveProvider(&_ServiceProviderRegistry.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactor) RenounceOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ServiceProviderRegistry.contract.Transact(opts, "renounceOwnership")
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) RenounceOwnership() (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.RenounceOwnership(&_ServiceProviderRegistry.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactorSession) RenounceOwnership() (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.RenounceOwnership(&_ServiceProviderRegistry.TransactOpts)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _ServiceProviderRegistry.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.TransferOwnership(&_ServiceProviderRegistry.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.TransferOwnership(&_ServiceProviderRegistry.TransactOpts, newOwner)
}

// UpdatePDPServiceWithCapabilities is a paid mutator transaction binding the contract method 0x0b5c0125.
//
// Solidity: function updatePDPServiceWithCapabilities((string,uint256,uint256,bool,bool,uint256,uint256,string,address) pdpOffering, string[] capabilityKeys, string[] capabilityValues) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactor) UpdatePDPServiceWithCapabilities(opts *bind.TransactOpts, pdpOffering ServiceProviderRegistryStoragePDPOffering, capabilityKeys []string, capabilityValues []string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.contract.Transact(opts, "updatePDPServiceWithCapabilities", pdpOffering, capabilityKeys, capabilityValues)
}

// UpdatePDPServiceWithCapabilities is a paid mutator transaction binding the contract method 0x0b5c0125.
//
// Solidity: function updatePDPServiceWithCapabilities((string,uint256,uint256,bool,bool,uint256,uint256,string,address) pdpOffering, string[] capabilityKeys, string[] capabilityValues) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) UpdatePDPServiceWithCapabilities(pdpOffering ServiceProviderRegistryStoragePDPOffering, capabilityKeys []string, capabilityValues []string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.UpdatePDPServiceWithCapabilities(&_ServiceProviderRegistry.TransactOpts, pdpOffering, capabilityKeys, capabilityValues)
}

// UpdatePDPServiceWithCapabilities is a paid mutator transaction binding the contract method 0x0b5c0125.
//
// Solidity: function updatePDPServiceWithCapabilities((string,uint256,uint256,bool,bool,uint256,uint256,string,address) pdpOffering, string[] capabilityKeys, string[] capabilityValues) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactorSession) UpdatePDPServiceWithCapabilities(pdpOffering ServiceProviderRegistryStoragePDPOffering, capabilityKeys []string, capabilityValues []string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.UpdatePDPServiceWithCapabilities(&_ServiceProviderRegistry.TransactOpts, pdpOffering, capabilityKeys, capabilityValues)
}

// UpdateProduct is a paid mutator transaction binding the contract method 0x8c9a7b56.
//
// Solidity: function updateProduct(uint8 productType, bytes productData, string[] capabilityKeys, string[] capabilityValues) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactor) UpdateProduct(opts *bind.TransactOpts, productType uint8, productData []byte, capabilityKeys []string, capabilityValues []string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.contract.Transact(opts, "updateProduct", productType, productData, capabilityKeys, capabilityValues)
}

// UpdateProduct is a paid mutator transaction binding the contract method 0x8c9a7b56.
//
// Solidity: function updateProduct(uint8 productType, bytes productData, string[] capabilityKeys, string[] capabilityValues) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) UpdateProduct(productType uint8, productData []byte, capabilityKeys []string, capabilityValues []string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.UpdateProduct(&_ServiceProviderRegistry.TransactOpts, productType, productData, capabilityKeys, capabilityValues)
}

// UpdateProduct is a paid mutator transaction binding the contract method 0x8c9a7b56.
//
// Solidity: function updateProduct(uint8 productType, bytes productData, string[] capabilityKeys, string[] capabilityValues) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactorSession) UpdateProduct(productType uint8, productData []byte, capabilityKeys []string, capabilityValues []string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.UpdateProduct(&_ServiceProviderRegistry.TransactOpts, productType, productData, capabilityKeys, capabilityValues)
}

// UpdateProviderInfo is a paid mutator transaction binding the contract method 0xd1c21b5b.
//
// Solidity: function updateProviderInfo(string name, string description) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactor) UpdateProviderInfo(opts *bind.TransactOpts, name string, description string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.contract.Transact(opts, "updateProviderInfo", name, description)
}

// UpdateProviderInfo is a paid mutator transaction binding the contract method 0xd1c21b5b.
//
// Solidity: function updateProviderInfo(string name, string description) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) UpdateProviderInfo(name string, description string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.UpdateProviderInfo(&_ServiceProviderRegistry.TransactOpts, name, description)
}

// UpdateProviderInfo is a paid mutator transaction binding the contract method 0xd1c21b5b.
//
// Solidity: function updateProviderInfo(string name, string description) returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactorSession) UpdateProviderInfo(name string, description string) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.UpdateProviderInfo(&_ServiceProviderRegistry.TransactOpts, name, description)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactor) UpgradeToAndCall(opts *bind.TransactOpts, newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _ServiceProviderRegistry.contract.Transact(opts, "upgradeToAndCall", newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_ServiceProviderRegistry *ServiceProviderRegistrySession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.UpgradeToAndCall(&_ServiceProviderRegistry.TransactOpts, newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_ServiceProviderRegistry *ServiceProviderRegistryTransactorSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _ServiceProviderRegistry.Contract.UpgradeToAndCall(&_ServiceProviderRegistry.TransactOpts, newImplementation, data)
}

// ServiceProviderRegistryContractUpgradedIterator is returned from FilterContractUpgraded and is used to iterate over the raw logs and unpacked data for ContractUpgraded events raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryContractUpgradedIterator struct {
	Event *ServiceProviderRegistryContractUpgraded // Event containing the contract specifics and raw log

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
func (it *ServiceProviderRegistryContractUpgradedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ServiceProviderRegistryContractUpgraded)
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
		it.Event = new(ServiceProviderRegistryContractUpgraded)
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
func (it *ServiceProviderRegistryContractUpgradedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ServiceProviderRegistryContractUpgradedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ServiceProviderRegistryContractUpgraded represents a ContractUpgraded event raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryContractUpgraded struct {
	Version        string
	Implementation common.Address
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterContractUpgraded is a free log retrieval operation binding the contract event 0x2b51ff7c4cc8e6fe1c72e9d9685b7d2a88a5d82ad3a644afbdceb0272c89c1c3.
//
// Solidity: event ContractUpgraded(string version, address implementation)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) FilterContractUpgraded(opts *bind.FilterOpts) (*ServiceProviderRegistryContractUpgradedIterator, error) {

	logs, sub, err := _ServiceProviderRegistry.contract.FilterLogs(opts, "ContractUpgraded")
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryContractUpgradedIterator{contract: _ServiceProviderRegistry.contract, event: "ContractUpgraded", logs: logs, sub: sub}, nil
}

// WatchContractUpgraded is a free log subscription operation binding the contract event 0x2b51ff7c4cc8e6fe1c72e9d9685b7d2a88a5d82ad3a644afbdceb0272c89c1c3.
//
// Solidity: event ContractUpgraded(string version, address implementation)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) WatchContractUpgraded(opts *bind.WatchOpts, sink chan<- *ServiceProviderRegistryContractUpgraded) (event.Subscription, error) {

	logs, sub, err := _ServiceProviderRegistry.contract.WatchLogs(opts, "ContractUpgraded")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ServiceProviderRegistryContractUpgraded)
				if err := _ServiceProviderRegistry.contract.UnpackLog(event, "ContractUpgraded", log); err != nil {
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

// ParseContractUpgraded is a log parse operation binding the contract event 0x2b51ff7c4cc8e6fe1c72e9d9685b7d2a88a5d82ad3a644afbdceb0272c89c1c3.
//
// Solidity: event ContractUpgraded(string version, address implementation)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) ParseContractUpgraded(log types.Log) (*ServiceProviderRegistryContractUpgraded, error) {
	event := new(ServiceProviderRegistryContractUpgraded)
	if err := _ServiceProviderRegistry.contract.UnpackLog(event, "ContractUpgraded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ServiceProviderRegistryEIP712DomainChangedIterator is returned from FilterEIP712DomainChanged and is used to iterate over the raw logs and unpacked data for EIP712DomainChanged events raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryEIP712DomainChangedIterator struct {
	Event *ServiceProviderRegistryEIP712DomainChanged // Event containing the contract specifics and raw log

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
func (it *ServiceProviderRegistryEIP712DomainChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ServiceProviderRegistryEIP712DomainChanged)
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
		it.Event = new(ServiceProviderRegistryEIP712DomainChanged)
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
func (it *ServiceProviderRegistryEIP712DomainChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ServiceProviderRegistryEIP712DomainChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ServiceProviderRegistryEIP712DomainChanged represents a EIP712DomainChanged event raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryEIP712DomainChanged struct {
	Raw types.Log // Blockchain specific contextual infos
}

// FilterEIP712DomainChanged is a free log retrieval operation binding the contract event 0x0a6387c9ea3628b88a633bb4f3b151770f70085117a15f9bf3787cda53f13d31.
//
// Solidity: event EIP712DomainChanged()
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) FilterEIP712DomainChanged(opts *bind.FilterOpts) (*ServiceProviderRegistryEIP712DomainChangedIterator, error) {

	logs, sub, err := _ServiceProviderRegistry.contract.FilterLogs(opts, "EIP712DomainChanged")
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryEIP712DomainChangedIterator{contract: _ServiceProviderRegistry.contract, event: "EIP712DomainChanged", logs: logs, sub: sub}, nil
}

// WatchEIP712DomainChanged is a free log subscription operation binding the contract event 0x0a6387c9ea3628b88a633bb4f3b151770f70085117a15f9bf3787cda53f13d31.
//
// Solidity: event EIP712DomainChanged()
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) WatchEIP712DomainChanged(opts *bind.WatchOpts, sink chan<- *ServiceProviderRegistryEIP712DomainChanged) (event.Subscription, error) {

	logs, sub, err := _ServiceProviderRegistry.contract.WatchLogs(opts, "EIP712DomainChanged")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ServiceProviderRegistryEIP712DomainChanged)
				if err := _ServiceProviderRegistry.contract.UnpackLog(event, "EIP712DomainChanged", log); err != nil {
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

// ParseEIP712DomainChanged is a log parse operation binding the contract event 0x0a6387c9ea3628b88a633bb4f3b151770f70085117a15f9bf3787cda53f13d31.
//
// Solidity: event EIP712DomainChanged()
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) ParseEIP712DomainChanged(log types.Log) (*ServiceProviderRegistryEIP712DomainChanged, error) {
	event := new(ServiceProviderRegistryEIP712DomainChanged)
	if err := _ServiceProviderRegistry.contract.UnpackLog(event, "EIP712DomainChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ServiceProviderRegistryInitializedIterator is returned from FilterInitialized and is used to iterate over the raw logs and unpacked data for Initialized events raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryInitializedIterator struct {
	Event *ServiceProviderRegistryInitialized // Event containing the contract specifics and raw log

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
func (it *ServiceProviderRegistryInitializedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ServiceProviderRegistryInitialized)
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
		it.Event = new(ServiceProviderRegistryInitialized)
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
func (it *ServiceProviderRegistryInitializedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ServiceProviderRegistryInitializedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ServiceProviderRegistryInitialized represents a Initialized event raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryInitialized struct {
	Version uint64
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterInitialized is a free log retrieval operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) FilterInitialized(opts *bind.FilterOpts) (*ServiceProviderRegistryInitializedIterator, error) {

	logs, sub, err := _ServiceProviderRegistry.contract.FilterLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryInitializedIterator{contract: _ServiceProviderRegistry.contract, event: "Initialized", logs: logs, sub: sub}, nil
}

// WatchInitialized is a free log subscription operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) WatchInitialized(opts *bind.WatchOpts, sink chan<- *ServiceProviderRegistryInitialized) (event.Subscription, error) {

	logs, sub, err := _ServiceProviderRegistry.contract.WatchLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ServiceProviderRegistryInitialized)
				if err := _ServiceProviderRegistry.contract.UnpackLog(event, "Initialized", log); err != nil {
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

// ParseInitialized is a log parse operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) ParseInitialized(log types.Log) (*ServiceProviderRegistryInitialized, error) {
	event := new(ServiceProviderRegistryInitialized)
	if err := _ServiceProviderRegistry.contract.UnpackLog(event, "Initialized", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ServiceProviderRegistryOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryOwnershipTransferredIterator struct {
	Event *ServiceProviderRegistryOwnershipTransferred // Event containing the contract specifics and raw log

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
func (it *ServiceProviderRegistryOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ServiceProviderRegistryOwnershipTransferred)
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
		it.Event = new(ServiceProviderRegistryOwnershipTransferred)
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
func (it *ServiceProviderRegistryOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ServiceProviderRegistryOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ServiceProviderRegistryOwnershipTransferred represents a OwnershipTransferred event raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*ServiceProviderRegistryOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _ServiceProviderRegistry.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryOwnershipTransferredIterator{contract: _ServiceProviderRegistry.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *ServiceProviderRegistryOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _ServiceProviderRegistry.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ServiceProviderRegistryOwnershipTransferred)
				if err := _ServiceProviderRegistry.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
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

// ParseOwnershipTransferred is a log parse operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) ParseOwnershipTransferred(log types.Log) (*ServiceProviderRegistryOwnershipTransferred, error) {
	event := new(ServiceProviderRegistryOwnershipTransferred)
	if err := _ServiceProviderRegistry.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ServiceProviderRegistryProductAddedIterator is returned from FilterProductAdded and is used to iterate over the raw logs and unpacked data for ProductAdded events raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryProductAddedIterator struct {
	Event *ServiceProviderRegistryProductAdded // Event containing the contract specifics and raw log

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
func (it *ServiceProviderRegistryProductAddedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ServiceProviderRegistryProductAdded)
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
		it.Event = new(ServiceProviderRegistryProductAdded)
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
func (it *ServiceProviderRegistryProductAddedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ServiceProviderRegistryProductAddedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ServiceProviderRegistryProductAdded represents a ProductAdded event raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryProductAdded struct {
	ProviderId       *big.Int
	ProductType      uint8
	ServiceUrl       string
	ServiceProvider  common.Address
	CapabilityKeys   []string
	CapabilityValues []string
	Raw              types.Log // Blockchain specific contextual infos
}

// FilterProductAdded is a free log retrieval operation binding the contract event 0x93c484964e7897bebc18f4392da3a48d42bf4356601904bf354a6537376af717.
//
// Solidity: event ProductAdded(uint256 indexed providerId, uint8 indexed productType, string serviceUrl, address serviceProvider, string[] capabilityKeys, string[] capabilityValues)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) FilterProductAdded(opts *bind.FilterOpts, providerId []*big.Int, productType []uint8) (*ServiceProviderRegistryProductAddedIterator, error) {

	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}
	var productTypeRule []interface{}
	for _, productTypeItem := range productType {
		productTypeRule = append(productTypeRule, productTypeItem)
	}

	logs, sub, err := _ServiceProviderRegistry.contract.FilterLogs(opts, "ProductAdded", providerIdRule, productTypeRule)
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryProductAddedIterator{contract: _ServiceProviderRegistry.contract, event: "ProductAdded", logs: logs, sub: sub}, nil
}

// WatchProductAdded is a free log subscription operation binding the contract event 0x93c484964e7897bebc18f4392da3a48d42bf4356601904bf354a6537376af717.
//
// Solidity: event ProductAdded(uint256 indexed providerId, uint8 indexed productType, string serviceUrl, address serviceProvider, string[] capabilityKeys, string[] capabilityValues)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) WatchProductAdded(opts *bind.WatchOpts, sink chan<- *ServiceProviderRegistryProductAdded, providerId []*big.Int, productType []uint8) (event.Subscription, error) {

	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}
	var productTypeRule []interface{}
	for _, productTypeItem := range productType {
		productTypeRule = append(productTypeRule, productTypeItem)
	}

	logs, sub, err := _ServiceProviderRegistry.contract.WatchLogs(opts, "ProductAdded", providerIdRule, productTypeRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ServiceProviderRegistryProductAdded)
				if err := _ServiceProviderRegistry.contract.UnpackLog(event, "ProductAdded", log); err != nil {
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

// ParseProductAdded is a log parse operation binding the contract event 0x93c484964e7897bebc18f4392da3a48d42bf4356601904bf354a6537376af717.
//
// Solidity: event ProductAdded(uint256 indexed providerId, uint8 indexed productType, string serviceUrl, address serviceProvider, string[] capabilityKeys, string[] capabilityValues)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) ParseProductAdded(log types.Log) (*ServiceProviderRegistryProductAdded, error) {
	event := new(ServiceProviderRegistryProductAdded)
	if err := _ServiceProviderRegistry.contract.UnpackLog(event, "ProductAdded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ServiceProviderRegistryProductRemovedIterator is returned from FilterProductRemoved and is used to iterate over the raw logs and unpacked data for ProductRemoved events raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryProductRemovedIterator struct {
	Event *ServiceProviderRegistryProductRemoved // Event containing the contract specifics and raw log

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
func (it *ServiceProviderRegistryProductRemovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ServiceProviderRegistryProductRemoved)
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
		it.Event = new(ServiceProviderRegistryProductRemoved)
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
func (it *ServiceProviderRegistryProductRemovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ServiceProviderRegistryProductRemovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ServiceProviderRegistryProductRemoved represents a ProductRemoved event raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryProductRemoved struct {
	ProviderId  *big.Int
	ProductType uint8
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterProductRemoved is a free log retrieval operation binding the contract event 0x4c363c6cd3d80189ef501b26de41894b3ed5e7b4a85b096be6cbcaa8a13e5e4d.
//
// Solidity: event ProductRemoved(uint256 indexed providerId, uint8 indexed productType)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) FilterProductRemoved(opts *bind.FilterOpts, providerId []*big.Int, productType []uint8) (*ServiceProviderRegistryProductRemovedIterator, error) {

	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}
	var productTypeRule []interface{}
	for _, productTypeItem := range productType {
		productTypeRule = append(productTypeRule, productTypeItem)
	}

	logs, sub, err := _ServiceProviderRegistry.contract.FilterLogs(opts, "ProductRemoved", providerIdRule, productTypeRule)
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryProductRemovedIterator{contract: _ServiceProviderRegistry.contract, event: "ProductRemoved", logs: logs, sub: sub}, nil
}

// WatchProductRemoved is a free log subscription operation binding the contract event 0x4c363c6cd3d80189ef501b26de41894b3ed5e7b4a85b096be6cbcaa8a13e5e4d.
//
// Solidity: event ProductRemoved(uint256 indexed providerId, uint8 indexed productType)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) WatchProductRemoved(opts *bind.WatchOpts, sink chan<- *ServiceProviderRegistryProductRemoved, providerId []*big.Int, productType []uint8) (event.Subscription, error) {

	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}
	var productTypeRule []interface{}
	for _, productTypeItem := range productType {
		productTypeRule = append(productTypeRule, productTypeItem)
	}

	logs, sub, err := _ServiceProviderRegistry.contract.WatchLogs(opts, "ProductRemoved", providerIdRule, productTypeRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ServiceProviderRegistryProductRemoved)
				if err := _ServiceProviderRegistry.contract.UnpackLog(event, "ProductRemoved", log); err != nil {
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

// ParseProductRemoved is a log parse operation binding the contract event 0x4c363c6cd3d80189ef501b26de41894b3ed5e7b4a85b096be6cbcaa8a13e5e4d.
//
// Solidity: event ProductRemoved(uint256 indexed providerId, uint8 indexed productType)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) ParseProductRemoved(log types.Log) (*ServiceProviderRegistryProductRemoved, error) {
	event := new(ServiceProviderRegistryProductRemoved)
	if err := _ServiceProviderRegistry.contract.UnpackLog(event, "ProductRemoved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ServiceProviderRegistryProductUpdatedIterator is returned from FilterProductUpdated and is used to iterate over the raw logs and unpacked data for ProductUpdated events raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryProductUpdatedIterator struct {
	Event *ServiceProviderRegistryProductUpdated // Event containing the contract specifics and raw log

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
func (it *ServiceProviderRegistryProductUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ServiceProviderRegistryProductUpdated)
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
		it.Event = new(ServiceProviderRegistryProductUpdated)
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
func (it *ServiceProviderRegistryProductUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ServiceProviderRegistryProductUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ServiceProviderRegistryProductUpdated represents a ProductUpdated event raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryProductUpdated struct {
	ProviderId       *big.Int
	ProductType      uint8
	ServiceUrl       string
	ServiceProvider  common.Address
	CapabilityKeys   []string
	CapabilityValues []string
	Raw              types.Log // Blockchain specific contextual infos
}

// FilterProductUpdated is a free log retrieval operation binding the contract event 0xc340a6dcdd0e7d3f96b6b2d3729fe5f0a6114e5847ba52b9e0071bf156dbaed6.
//
// Solidity: event ProductUpdated(uint256 indexed providerId, uint8 indexed productType, string serviceUrl, address serviceProvider, string[] capabilityKeys, string[] capabilityValues)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) FilterProductUpdated(opts *bind.FilterOpts, providerId []*big.Int, productType []uint8) (*ServiceProviderRegistryProductUpdatedIterator, error) {

	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}
	var productTypeRule []interface{}
	for _, productTypeItem := range productType {
		productTypeRule = append(productTypeRule, productTypeItem)
	}

	logs, sub, err := _ServiceProviderRegistry.contract.FilterLogs(opts, "ProductUpdated", providerIdRule, productTypeRule)
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryProductUpdatedIterator{contract: _ServiceProviderRegistry.contract, event: "ProductUpdated", logs: logs, sub: sub}, nil
}

// WatchProductUpdated is a free log subscription operation binding the contract event 0xc340a6dcdd0e7d3f96b6b2d3729fe5f0a6114e5847ba52b9e0071bf156dbaed6.
//
// Solidity: event ProductUpdated(uint256 indexed providerId, uint8 indexed productType, string serviceUrl, address serviceProvider, string[] capabilityKeys, string[] capabilityValues)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) WatchProductUpdated(opts *bind.WatchOpts, sink chan<- *ServiceProviderRegistryProductUpdated, providerId []*big.Int, productType []uint8) (event.Subscription, error) {

	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}
	var productTypeRule []interface{}
	for _, productTypeItem := range productType {
		productTypeRule = append(productTypeRule, productTypeItem)
	}

	logs, sub, err := _ServiceProviderRegistry.contract.WatchLogs(opts, "ProductUpdated", providerIdRule, productTypeRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ServiceProviderRegistryProductUpdated)
				if err := _ServiceProviderRegistry.contract.UnpackLog(event, "ProductUpdated", log); err != nil {
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

// ParseProductUpdated is a log parse operation binding the contract event 0xc340a6dcdd0e7d3f96b6b2d3729fe5f0a6114e5847ba52b9e0071bf156dbaed6.
//
// Solidity: event ProductUpdated(uint256 indexed providerId, uint8 indexed productType, string serviceUrl, address serviceProvider, string[] capabilityKeys, string[] capabilityValues)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) ParseProductUpdated(log types.Log) (*ServiceProviderRegistryProductUpdated, error) {
	event := new(ServiceProviderRegistryProductUpdated)
	if err := _ServiceProviderRegistry.contract.UnpackLog(event, "ProductUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ServiceProviderRegistryProviderInfoUpdatedIterator is returned from FilterProviderInfoUpdated and is used to iterate over the raw logs and unpacked data for ProviderInfoUpdated events raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryProviderInfoUpdatedIterator struct {
	Event *ServiceProviderRegistryProviderInfoUpdated // Event containing the contract specifics and raw log

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
func (it *ServiceProviderRegistryProviderInfoUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ServiceProviderRegistryProviderInfoUpdated)
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
		it.Event = new(ServiceProviderRegistryProviderInfoUpdated)
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
func (it *ServiceProviderRegistryProviderInfoUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ServiceProviderRegistryProviderInfoUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ServiceProviderRegistryProviderInfoUpdated represents a ProviderInfoUpdated event raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryProviderInfoUpdated struct {
	ProviderId *big.Int
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterProviderInfoUpdated is a free log retrieval operation binding the contract event 0xae10af73bdb200f240b1ea85ef806346fb24c82388af00414f4c5fcfeef68f76.
//
// Solidity: event ProviderInfoUpdated(uint256 indexed providerId)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) FilterProviderInfoUpdated(opts *bind.FilterOpts, providerId []*big.Int) (*ServiceProviderRegistryProviderInfoUpdatedIterator, error) {

	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}

	logs, sub, err := _ServiceProviderRegistry.contract.FilterLogs(opts, "ProviderInfoUpdated", providerIdRule)
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryProviderInfoUpdatedIterator{contract: _ServiceProviderRegistry.contract, event: "ProviderInfoUpdated", logs: logs, sub: sub}, nil
}

// WatchProviderInfoUpdated is a free log subscription operation binding the contract event 0xae10af73bdb200f240b1ea85ef806346fb24c82388af00414f4c5fcfeef68f76.
//
// Solidity: event ProviderInfoUpdated(uint256 indexed providerId)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) WatchProviderInfoUpdated(opts *bind.WatchOpts, sink chan<- *ServiceProviderRegistryProviderInfoUpdated, providerId []*big.Int) (event.Subscription, error) {

	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}

	logs, sub, err := _ServiceProviderRegistry.contract.WatchLogs(opts, "ProviderInfoUpdated", providerIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ServiceProviderRegistryProviderInfoUpdated)
				if err := _ServiceProviderRegistry.contract.UnpackLog(event, "ProviderInfoUpdated", log); err != nil {
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

// ParseProviderInfoUpdated is a log parse operation binding the contract event 0xae10af73bdb200f240b1ea85ef806346fb24c82388af00414f4c5fcfeef68f76.
//
// Solidity: event ProviderInfoUpdated(uint256 indexed providerId)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) ParseProviderInfoUpdated(log types.Log) (*ServiceProviderRegistryProviderInfoUpdated, error) {
	event := new(ServiceProviderRegistryProviderInfoUpdated)
	if err := _ServiceProviderRegistry.contract.UnpackLog(event, "ProviderInfoUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ServiceProviderRegistryProviderRegisteredIterator is returned from FilterProviderRegistered and is used to iterate over the raw logs and unpacked data for ProviderRegistered events raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryProviderRegisteredIterator struct {
	Event *ServiceProviderRegistryProviderRegistered // Event containing the contract specifics and raw log

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
func (it *ServiceProviderRegistryProviderRegisteredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ServiceProviderRegistryProviderRegistered)
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
		it.Event = new(ServiceProviderRegistryProviderRegistered)
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
func (it *ServiceProviderRegistryProviderRegisteredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ServiceProviderRegistryProviderRegisteredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ServiceProviderRegistryProviderRegistered represents a ProviderRegistered event raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryProviderRegistered struct {
	ProviderId      *big.Int
	ServiceProvider common.Address
	Payee           common.Address
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterProviderRegistered is a free log retrieval operation binding the contract event 0xaff7a33d237d3d600a92c556cda34cb73cf7cccc667e163c90b1d2d392b031a5.
//
// Solidity: event ProviderRegistered(uint256 indexed providerId, address indexed serviceProvider, address indexed payee)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) FilterProviderRegistered(opts *bind.FilterOpts, providerId []*big.Int, serviceProvider []common.Address, payee []common.Address) (*ServiceProviderRegistryProviderRegisteredIterator, error) {

	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}
	var serviceProviderRule []interface{}
	for _, serviceProviderItem := range serviceProvider {
		serviceProviderRule = append(serviceProviderRule, serviceProviderItem)
	}
	var payeeRule []interface{}
	for _, payeeItem := range payee {
		payeeRule = append(payeeRule, payeeItem)
	}

	logs, sub, err := _ServiceProviderRegistry.contract.FilterLogs(opts, "ProviderRegistered", providerIdRule, serviceProviderRule, payeeRule)
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryProviderRegisteredIterator{contract: _ServiceProviderRegistry.contract, event: "ProviderRegistered", logs: logs, sub: sub}, nil
}

// WatchProviderRegistered is a free log subscription operation binding the contract event 0xaff7a33d237d3d600a92c556cda34cb73cf7cccc667e163c90b1d2d392b031a5.
//
// Solidity: event ProviderRegistered(uint256 indexed providerId, address indexed serviceProvider, address indexed payee)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) WatchProviderRegistered(opts *bind.WatchOpts, sink chan<- *ServiceProviderRegistryProviderRegistered, providerId []*big.Int, serviceProvider []common.Address, payee []common.Address) (event.Subscription, error) {

	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}
	var serviceProviderRule []interface{}
	for _, serviceProviderItem := range serviceProvider {
		serviceProviderRule = append(serviceProviderRule, serviceProviderItem)
	}
	var payeeRule []interface{}
	for _, payeeItem := range payee {
		payeeRule = append(payeeRule, payeeItem)
	}

	logs, sub, err := _ServiceProviderRegistry.contract.WatchLogs(opts, "ProviderRegistered", providerIdRule, serviceProviderRule, payeeRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ServiceProviderRegistryProviderRegistered)
				if err := _ServiceProviderRegistry.contract.UnpackLog(event, "ProviderRegistered", log); err != nil {
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

// ParseProviderRegistered is a log parse operation binding the contract event 0xaff7a33d237d3d600a92c556cda34cb73cf7cccc667e163c90b1d2d392b031a5.
//
// Solidity: event ProviderRegistered(uint256 indexed providerId, address indexed serviceProvider, address indexed payee)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) ParseProviderRegistered(log types.Log) (*ServiceProviderRegistryProviderRegistered, error) {
	event := new(ServiceProviderRegistryProviderRegistered)
	if err := _ServiceProviderRegistry.contract.UnpackLog(event, "ProviderRegistered", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ServiceProviderRegistryProviderRemovedIterator is returned from FilterProviderRemoved and is used to iterate over the raw logs and unpacked data for ProviderRemoved events raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryProviderRemovedIterator struct {
	Event *ServiceProviderRegistryProviderRemoved // Event containing the contract specifics and raw log

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
func (it *ServiceProviderRegistryProviderRemovedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ServiceProviderRegistryProviderRemoved)
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
		it.Event = new(ServiceProviderRegistryProviderRemoved)
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
func (it *ServiceProviderRegistryProviderRemovedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ServiceProviderRegistryProviderRemovedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ServiceProviderRegistryProviderRemoved represents a ProviderRemoved event raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryProviderRemoved struct {
	ProviderId *big.Int
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterProviderRemoved is a free log retrieval operation binding the contract event 0x452148878c72ebab44f2761cb8b0b79c50628a437350aee5f3aab66625addcc4.
//
// Solidity: event ProviderRemoved(uint256 indexed providerId)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) FilterProviderRemoved(opts *bind.FilterOpts, providerId []*big.Int) (*ServiceProviderRegistryProviderRemovedIterator, error) {

	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}

	logs, sub, err := _ServiceProviderRegistry.contract.FilterLogs(opts, "ProviderRemoved", providerIdRule)
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryProviderRemovedIterator{contract: _ServiceProviderRegistry.contract, event: "ProviderRemoved", logs: logs, sub: sub}, nil
}

// WatchProviderRemoved is a free log subscription operation binding the contract event 0x452148878c72ebab44f2761cb8b0b79c50628a437350aee5f3aab66625addcc4.
//
// Solidity: event ProviderRemoved(uint256 indexed providerId)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) WatchProviderRemoved(opts *bind.WatchOpts, sink chan<- *ServiceProviderRegistryProviderRemoved, providerId []*big.Int) (event.Subscription, error) {

	var providerIdRule []interface{}
	for _, providerIdItem := range providerId {
		providerIdRule = append(providerIdRule, providerIdItem)
	}

	logs, sub, err := _ServiceProviderRegistry.contract.WatchLogs(opts, "ProviderRemoved", providerIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ServiceProviderRegistryProviderRemoved)
				if err := _ServiceProviderRegistry.contract.UnpackLog(event, "ProviderRemoved", log); err != nil {
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

// ParseProviderRemoved is a log parse operation binding the contract event 0x452148878c72ebab44f2761cb8b0b79c50628a437350aee5f3aab66625addcc4.
//
// Solidity: event ProviderRemoved(uint256 indexed providerId)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) ParseProviderRemoved(log types.Log) (*ServiceProviderRegistryProviderRemoved, error) {
	event := new(ServiceProviderRegistryProviderRemoved)
	if err := _ServiceProviderRegistry.contract.UnpackLog(event, "ProviderRemoved", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ServiceProviderRegistryUpgradedIterator is returned from FilterUpgraded and is used to iterate over the raw logs and unpacked data for Upgraded events raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryUpgradedIterator struct {
	Event *ServiceProviderRegistryUpgraded // Event containing the contract specifics and raw log

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
func (it *ServiceProviderRegistryUpgradedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ServiceProviderRegistryUpgraded)
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
		it.Event = new(ServiceProviderRegistryUpgraded)
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
func (it *ServiceProviderRegistryUpgradedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ServiceProviderRegistryUpgradedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ServiceProviderRegistryUpgraded represents a Upgraded event raised by the ServiceProviderRegistry contract.
type ServiceProviderRegistryUpgraded struct {
	Implementation common.Address
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterUpgraded is a free log retrieval operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) FilterUpgraded(opts *bind.FilterOpts, implementation []common.Address) (*ServiceProviderRegistryUpgradedIterator, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _ServiceProviderRegistry.contract.FilterLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return &ServiceProviderRegistryUpgradedIterator{contract: _ServiceProviderRegistry.contract, event: "Upgraded", logs: logs, sub: sub}, nil
}

// WatchUpgraded is a free log subscription operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) WatchUpgraded(opts *bind.WatchOpts, sink chan<- *ServiceProviderRegistryUpgraded, implementation []common.Address) (event.Subscription, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _ServiceProviderRegistry.contract.WatchLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ServiceProviderRegistryUpgraded)
				if err := _ServiceProviderRegistry.contract.UnpackLog(event, "Upgraded", log); err != nil {
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

// ParseUpgraded is a log parse operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_ServiceProviderRegistry *ServiceProviderRegistryFilterer) ParseUpgraded(log types.Log) (*ServiceProviderRegistryUpgraded, error) {
	event := new(ServiceProviderRegistryUpgraded)
	if err := _ServiceProviderRegistry.contract.UnpackLog(event, "Upgraded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
