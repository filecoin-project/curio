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
			PDPVerifier: common.HexToAddress("0x38529187C03de8d60C8489af063c675b0892CCD9"),
		}
	default:
		panic("pdp contracts unknown for this network")
	}
}

const NumChallenges = 5

func SybilFee() *big.Int {
	return must.One(types.ParseFIL("0.1")).Int
}

/*
ProofFee returns the proof fee for a given challenge count

	function proofFee(uint256 challengeCount) internal view returns (uint256) {
	    uint256 gasPrice;
	    if (block.basefee > ONE_NANO_FIL) {
	        gasPrice = block.basefee;
	    } else {
	        gasPrice = ONE_NANO_FIL;
	    }
	    uint256 numerator = PROOF_GAS_FLOOR * challengeCount * gasPrice;
	    uint256 denominator = ONE_PERCENT_DENOM;
	    return numerator / denominator;
	}
*/
func ProofFee(ts *types.TipSet) *big.Int {
	oneNanoFil := types.NewInt(1e9)
	proofGasFloor := types.NewInt(2_000_000)
	onePercentDenom := types.NewInt(90) // 100 is 1%, but we want to overestimate the fee (excess is returned)
	challengeCount := types.NewInt(NumChallenges)

	var gasPrice *big.Int
	if ts.Blocks()[0].ParentBaseFee.GreaterThan(oneNanoFil) {
		gasPrice = ts.Blocks()[0].ParentBaseFee.Int
	} else {
		gasPrice = oneNanoFil.Int
	}

	numerator := new(big.Int).Mul(proofGasFloor.Int, new(big.Int).Mul(challengeCount.Int, gasPrice))
	return new(big.Int).Div(numerator, onePercentDenom.Int)
}
