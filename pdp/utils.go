package pdp

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/filecoin-project/curio/pdp/contract"
)

// FWSSCreateIdentity is the decoded FilecoinWarmStorageService create payload:
// payer, clientDataSetId, and metadata key/value pairs.
type FWSSCreateIdentity struct {
	Payer           common.Address
	ClientDataSetId *big.Int
	MetadataKeys    []string
	MetadataValues  []string
}

// HasMetadataKey reports whether key is present in MetadataKeys.
func (p *FWSSCreateIdentity) HasMetadataKey(key string) bool {
	if p == nil {
		return false
	}
	for _, k := range p.MetadataKeys {
		if k == key {
			return true
		}
	}
	return false
}

// ReadRestricted reports whether metadata key "read" is "restricted".
func (p *FWSSCreateIdentity) ReadRestricted() bool {
	if p == nil {
		return false
	}
	var value string
	for i, k := range p.MetadataKeys {
		if k != MetadataKeyACL || i >= len(p.MetadataValues) {
			continue
		}
		value = strings.TrimSpace(p.MetadataValues[i])
		break
	}
	return strings.EqualFold(value, DATASET_READ_RESTRICTED)
}

// DecodeFWSSCreateIdentity ABI-decodes create extraData.
// extraData may be the create payload or (bytes createPayload, bytes addPayload).
func DecodeFWSSCreateIdentity(extraData []byte) (*FWSSCreateIdentity, error) {
	if len(extraData) == 0 {
		return nil, fmt.Errorf("extraData is empty")
	}
	if bytesType, err := abi.NewType("bytes", "", nil); err == nil {
		decoded, err := abi.Arguments{{Type: bytesType}, {Type: bytesType}}.Unpack(extraData)
		if err == nil && len(decoded) >= 1 {
			if createPayload, ok := decoded[0].([]byte); ok {
				if id, err := unpackCreateIdentity(createPayload); err == nil {
					return id, nil
				}
			}
		}
	}
	return unpackCreateIdentity(extraData)
}

// FWSSPayerFromExtraData returns the non-zero payer from create-and-add extraData.
func FWSSPayerFromExtraData(extraData []byte) (common.Address, error) {
	id, err := DecodeFWSSCreateIdentity(extraData)
	if err != nil {
		return common.Address{}, err
	}
	if id.Payer == (common.Address{}) {
		return common.Address{}, fmt.Errorf("payer is zero address")
	}
	return id.Payer, nil
}

// CreateIdentityFromPDPCalldata extracts FWSS create identity from PDPVerifier
// createDataSet or addPieces(NEW_DATA_SET_SENTINEL, ...) transaction calldata.
func CreateIdentityFromPDPCalldata(data []byte) (*FWSSCreateIdentity, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("calldata too short")
	}
	pdpABI, err := contract.PDPVerifierMetaData.GetAbi()
	if err != nil {
		return nil, fmt.Errorf("PDPVerifier ABI: %w", err)
	}
	method, err := pdpABI.MethodById(data[:4])
	if err != nil {
		return nil, fmt.Errorf("PDPVerifier method: %w", err)
	}

	args, err := method.Inputs.Unpack(data[4:])
	if err != nil {
		return nil, fmt.Errorf("unpack %s: %w", method.Name, err)
	}

	var extraData []byte
	switch method.Name {
	case "createDataSet":
		if len(args) < 2 {
			return nil, fmt.Errorf("createDataSet missing extraData")
		}
		var ok bool
		extraData, ok = args[1].([]byte)
		if !ok {
			return nil, fmt.Errorf("createDataSet extraData is not bytes")
		}
	case "addPieces":
		if len(args) < 4 {
			return nil, fmt.Errorf("addPieces missing args")
		}
		setId, ok := args[0].(*big.Int)
		if !ok {
			return nil, fmt.Errorf("addPieces setId is not *big.Int")
		}
		if setId == nil || setId.Sign() != 0 {
			return nil, fmt.Errorf("addPieces is not a new-data-set create")
		}
		extraData, ok = args[3].([]byte)
		if !ok {
			return nil, fmt.Errorf("addPieces extraData is not bytes")
		}
	default:
		return nil, fmt.Errorf("unsupported PDPVerifier method %s", method.Name)
	}
	return DecodeFWSSCreateIdentity(extraData)
}

// CreateIdentityFromSignedTx extracts FWSS create identity from a signed PDPVerifier tx.
func CreateIdentityFromSignedTx(signedTx *types.Transaction) (*FWSSCreateIdentity, error) {
	if signedTx == nil {
		return nil, fmt.Errorf("signed tx is nil")
	}
	return CreateIdentityFromPDPCalldata(signedTx.Data())
}

// unpackCreateIdentity unpacks the create tuple:
// (address payer, uint256 clientDataSetId, string[] keys, string[] values, bytes signature).
func unpackCreateIdentity(createPayload []byte) (*FWSSCreateIdentity, error) {
	if len(createPayload) == 0 {
		return nil, fmt.Errorf("createPayload is empty")
	}
	args, err := createPayloadArgs()
	if err != nil {
		return nil, err
	}
	decoded, err := args.Unpack(createPayload)
	if err != nil {
		return nil, fmt.Errorf("decode createPayload: %w", err)
	}
	if len(decoded) < 3 {
		return nil, fmt.Errorf("createPayload missing fields")
	}

	payer, ok := decoded[0].(common.Address)
	if !ok {
		return nil, fmt.Errorf("payer is not an address")
	}
	clientDataSetId, ok := decoded[1].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("clientDataSetId is not *big.Int")
	}
	keys, ok := decoded[2].([]string)
	if !ok {
		return nil, fmt.Errorf("keys is not []string")
	}
	var values []string
	if len(decoded) >= 4 && decoded[3] != nil {
		values, ok = decoded[3].([]string)
		if !ok {
			return nil, fmt.Errorf("values is not []string")
		}
	}

	return &FWSSCreateIdentity{
		Payer:           payer,
		ClientDataSetId: clientDataSetId,
		MetadataKeys:    keys,
		MetadataValues:  values,
	}, nil
}

func createPayloadArgs() (abi.Arguments, error) {
	bytesType, err := abi.NewType("bytes", "", nil)
	if err != nil {
		return nil, fmt.Errorf("create bytes ABI type: %w", err)
	}
	addressType, err := abi.NewType("address", "", nil)
	if err != nil {
		return nil, fmt.Errorf("create address ABI type: %w", err)
	}
	uint256Type, err := abi.NewType("uint256", "", nil)
	if err != nil {
		return nil, fmt.Errorf("create uint256 ABI type: %w", err)
	}
	stringArrayType, err := abi.NewType("string[]", "", nil)
	if err != nil {
		return nil, fmt.Errorf("create string array ABI type: %w", err)
	}
	return abi.Arguments{
		{Type: addressType},
		{Type: uint256Type},
		{Type: stringArrayType},
		{Type: stringArrayType},
		{Type: bytesType},
	}, nil
}
