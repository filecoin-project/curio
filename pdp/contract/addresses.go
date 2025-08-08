package contract

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/snadrus/must"

	"github.com/filecoin-project/curio/build"

	"github.com/filecoin-project/lotus/chain/types"
)

const PDPMainnet = "0x9C65E8E57C98cCc040A3d825556832EA1e9f4Df6"
const PDPCalibnet = "0x5A23b7df87f59A291C26A2A1d684AD03Ce9B68DC"
const PDPTestNet = "Change Me"

type PDPContracts struct {
	PDPVerifier common.Address
}

func ContractAddresses() PDPContracts {
	return PDPContracts{
		PDPVerifier: ConfigurePDPAddress(),
	}
}

func ConfigurePDPAddress() common.Address {
	switch build.BuildType {
	case build.BuildCalibnet:
		return common.HexToAddress(PDPCalibnet)
	case build.BuildMainnet:
		return common.HexToAddress(PDPMainnet)
	case build.Build2k, build.BuildDebug:
		if !common.IsHexAddress(PDPTestNet) {
			panic("PDPTestNet not set")
		}
		return common.HexToAddress(PDPTestNet)
	default:
		panic("pdp contracts unknown for this network")
	}
}

const NumChallenges = 5

func SybilFee() *big.Int {
	return must.One(types.ParseFIL("0.1")).Int
}
