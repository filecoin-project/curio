package pdp

import (
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

type fwssCreatePayload struct {
	Payer        common.Address
	MetadataKeys []string
}

func decodeFWSSCreatePayload(extraData []byte) (*fwssCreatePayload, error) {
	if len(extraData) == 0 {
		return nil, fmt.Errorf("extraData is empty")
	}

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
	createArgs := abi.Arguments{
		{Type: addressType},     // payer
		{Type: uint256Type},     // clientDataSetId
		{Type: stringArrayType}, // keys
		{Type: stringArrayType}, // values
		{Type: bytesType},       // signature
	}

	createDecoded, err := createArgs.Unpack(createPayload)
	if err != nil {
		return nil, fmt.Errorf("decode createPayload: %w", err)
	}
	if len(createDecoded) < 3 {
		return nil, fmt.Errorf("createPayload missing metadata keys")
	}

	payer, ok := createDecoded[0].(common.Address)
	if !ok {
		return nil, fmt.Errorf("payer is not an address")
	}
	keys, ok := createDecoded[2].([]string)
	if !ok {
		return nil, fmt.Errorf("keys is not []string")
	}

	return &fwssCreatePayload{
		Payer:        payer,
		MetadataKeys: keys,
	}, nil
}

// FWSSPayerFromExtraData extracts the FilecoinWarmStorageService payer from
// pull extraData. The expected format is the combined operation payload:
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
