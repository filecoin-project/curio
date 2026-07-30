package pdp

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/filecoin-project/curio/pdp/contract"
)

// FWSSCreateIdentity is the payer + clientDataSetId from an FWSS create payload.
type FWSSCreateIdentity struct {
	Payer           common.Address
	ClientDataSetId *big.Int
	MetadataKeys    []string
}

type fwssCreatePayload = FWSSCreateIdentity

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
		{Type: addressType},     // payer
		{Type: uint256Type},     // clientDataSetId
		{Type: stringArrayType}, // keys
		{Type: stringArrayType}, // values
		{Type: bytesType},       // signature
	}, nil
}

func decodeFWSSCreateData(createPayload []byte) (*FWSSCreateIdentity, error) {
	if len(createPayload) == 0 {
		return nil, fmt.Errorf("createPayload is empty")
	}
	createArgs, err := createPayloadArgs()
	if err != nil {
		return nil, err
	}
	createDecoded, err := createArgs.Unpack(createPayload)
	if err != nil {
		return nil, fmt.Errorf("decode createPayload: %w", err)
	}
	if len(createDecoded) < 3 {
		return nil, fmt.Errorf("createPayload missing fields")
	}

	payer, ok := createDecoded[0].(common.Address)
	if !ok {
		return nil, fmt.Errorf("payer is not an address")
	}
	clientDataSetId, ok := createDecoded[1].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("clientDataSetId is not *big.Int")
	}
	keys, ok := createDecoded[2].([]string)
	if !ok {
		return nil, fmt.Errorf("keys is not []string")
	}

	return &FWSSCreateIdentity{
		Payer:           payer,
		ClientDataSetId: clientDataSetId,
		MetadataKeys:    keys,
	}, nil
}

func combinedCreatePayload(extraData []byte) ([]byte, error) {
	bytesType, err := abi.NewType("bytes", "", nil)
	if err != nil {
		return nil, fmt.Errorf("create bytes ABI type: %w", err)
	}
	outerArgs := abi.Arguments{
		{Type: bytesType}, // createPayload
		{Type: bytesType}, // addPayload
	}
	decoded, err := outerArgs.Unpack(extraData)
	if err != nil {
		return nil, fmt.Errorf("decode combined extraData: %w", err)
	}
	if len(decoded) < 1 {
		return nil, fmt.Errorf("combined extraData missing createPayload")
	}
	createPayload, ok := decoded[0].([]byte)
	if !ok {
		return nil, fmt.Errorf("createPayload is not bytes")
	}
	return createPayload, nil
}

// decodeFWSSCreatePayload decodes combined create-and-add extraData:
//
//	(bytes createPayload, bytes addPayload)
func decodeFWSSCreatePayload(extraData []byte) (*FWSSCreateIdentity, error) {
	if len(extraData) == 0 {
		return nil, fmt.Errorf("extraData is empty")
	}
	createPayload, err := combinedCreatePayload(extraData)
	if err != nil {
		return nil, err
	}
	return decodeFWSSCreateData(createPayload)
}

// DecodeFWSSCreateIdentityFromExtraData extracts payer + clientDataSetId from either
// combined create-and-add extraData or a bare createDataSet create payload.
func DecodeFWSSCreateIdentityFromExtraData(extraData []byte) (*FWSSCreateIdentity, error) {
	if len(extraData) == 0 {
		return nil, fmt.Errorf("extraData is empty")
	}
	if payload, err := decodeFWSSCreatePayload(extraData); err == nil {
		return payload, nil
	}
	return decodeFWSSCreateData(extraData)
}

// FWSSPayerFromExtraData extracts the FilecoinWarmStorageService payer from
// create-new pull extraData. The expected format is the combined operation payload:
//
//	(bytes createPayload, bytes addPayload)
//
// where createPayload is:
//
//	(address payer, uint256 clientDataSetId, string[] keys, string[] values, bytes signature)
func FWSSPayerFromExtraData(extraData []byte) (common.Address, error) {
	payload, err := decodeFWSSCreatePayload(extraData)
	if err != nil {
		return common.Address{}, err
	}
	if payload.Payer == (common.Address{}) {
		return common.Address{}, fmt.Errorf("payer is zero address")
	}

	return payload.Payer, nil
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

	switch method.Name {
	case "createDataSet":
		if len(args) < 2 {
			return nil, fmt.Errorf("createDataSet missing extraData")
		}
		extraData, ok := args[1].([]byte)
		if !ok {
			return nil, fmt.Errorf("createDataSet extraData is not bytes")
		}
		return DecodeFWSSCreateIdentityFromExtraData(extraData)
	case "addPieces":
		if len(args) < 4 {
			return nil, fmt.Errorf("addPieces missing args")
		}
		setId, ok := args[0].(*big.Int)
		if !ok {
			return nil, fmt.Errorf("addPieces setId is not *big.Int")
		}
		// NEW_DATA_SET_SENTINEL is 0
		if setId == nil || setId.Sign() != 0 {
			return nil, fmt.Errorf("addPieces is not a new-data-set create")
		}
		extraData, ok := args[3].([]byte)
		if !ok {
			return nil, fmt.Errorf("addPieces extraData is not bytes")
		}
		return DecodeFWSSCreateIdentityFromExtraData(extraData)
	default:
		return nil, fmt.Errorf("unsupported PDPVerifier method %s", method.Name)
	}
}

// CreateIdentityFromSignedTx extracts FWSS create identity from a signed PDPVerifier tx.
func CreateIdentityFromSignedTx(signedTx *types.Transaction) (*FWSSCreateIdentity, error) {
	if signedTx == nil {
		return nil, fmt.Errorf("signed tx is nil")
	}
	return CreateIdentityFromPDPCalldata(signedTx.Data())
}
