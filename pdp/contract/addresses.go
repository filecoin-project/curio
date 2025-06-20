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
	Pandora     common.Address
}

func ContractAddresses() PDPContracts {
	switch build.BuildType {
	case build.BuildCalibnet:
		return PDPContracts{
			PDPVerifier: common.HexToAddress("0x5A23b7df87f59A291C26A2A1d684AD03Ce9B68DC"),
			Pandora:     common.HexToAddress("0xf49ba5eaCdFD5EE3744efEdf413791935FE4D4c5"),
		}
	case build.BuildMainnet:
		return PDPContracts{
			PDPVerifier: common.HexToAddress("0x9C65E8E57C98cCc040A3d825556832EA1e9f4Df6"),
		}
	default:
		panic("pdp contracts unknown for this network")
	}
}

const NumChallenges = 5

func SybilFee() *big.Int {
	return must.One(types.ParseFIL("0.1")).Int
}
