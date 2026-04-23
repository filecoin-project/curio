package multicall

import (
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/xerrors"
)

// BatchCallGenerate generates a batch call for the given calls. The transaction is generated for the senderEth subsystem.
// Do not use this for sending transactions.
func BatchCallGenerate(calls []Multicall3Call) (*types.Transaction, error) {
	multicallAddress, err := MultiCallAddress()
	if err != nil {
		return nil, xerrors.Errorf("failed to get multicall address: %w", err)
	}

	mabi, err := IMulticall3MetaData.GetAbi()
	if err != nil {
		return nil, xerrors.Errorf("failed to get IMulticall3 ABI: %w", err)
	}

	callData, err := mabi.Pack("aggregate", calls)
	if err != nil {
		return nil, xerrors.Errorf("failed to pack data: %w", err)
	}

	// Prepare the transaction (nonce will be set to 0, SenderETH will assign it)
	return types.NewTransaction(
		0,
		multicallAddress,
		big.NewInt(0),
		0,
		nil,
		callData,
	), nil
}
