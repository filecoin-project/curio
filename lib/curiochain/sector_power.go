package curiochain

import (
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	miner19 "github.com/filecoin-project/go-state-types/builtin/v19/miner"

	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
)

// TODO: Use the upstream constant when the Solstice dependencies are released.
const FULL_QA_POWER miner.SectorOnChainInfoFlags = 1 << 1

// SectorIsFullQaPower includes both flagged sectors and legacy sectors at 10x power.
func SectorIsFullQaPower(info *miner.SectorOnChainInfo) bool {
	if info.Flags&FULL_QA_POWER != 0 {
		return true
	}
	duration := int64(info.Expiration - info.PowerBaseEpoch)
	if duration <= 0 {
		return false
	}
	sectorSize, err := info.SealProof.SectorSize()
	if err != nil {
		return false
	}
	fullWeight := big.Mul(big.NewInt(int64(sectorSize)), big.NewInt(duration))
	return info.VerifiedDealWeight.GreaterThanEqual(fullWeight)
}

func SectorQAPower(info *miner.SectorOnChainInfo) (abi.StoragePower, error) {
	sectorSize, err := info.SealProof.SectorSize()
	if err != nil {
		return big.Zero(), err
	}
	if SectorIsFullQaPower(info) {
		return miner.QAPowerMax(sectorSize), nil
	}
	if info.Expiration <= info.PowerBaseEpoch {
		return big.Zero(), xerrors.Errorf("sector %d has non-positive power duration", info.SectorNumber)
	}
	return miner19.QAPowerForSector(sectorSize, info), nil
}
