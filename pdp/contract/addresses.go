package contract

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/snadrus/must"

	"github.com/filecoin-project/curio/build"

	"github.com/filecoin-project/lotus/chain/types"
)

type PDPContracts struct {
	PDPVerifier common.Address
}

func ContractAddresses() PDPContracts {
	switch build.BuildType {
	case build.BuildCalibnet:
		return PDPContracts{
			PDPVerifier: common.HexToAddress("0x4E1e9AB9bf23E9Fe96041E0a2d2f0B99dE27FBb2"),
		}
	case build.BuildMainnet:
		// Compatible contract not yet deployed
		panic("compatible PDP contract not available on mainnet")
	default:
		panic("PDP contract unknown for this network")
	}
}

const NumChallenges = 5

func SybilFee() *big.Int {
	return must.One(types.ParseFIL("0.1")).Int
}
