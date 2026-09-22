package curiochain

import (
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	miner19 "github.com/filecoin-project/go-state-types/builtin/v19/miner"

	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
)

func SectorQAPower(info *miner.SectorOnChainInfo) (abi.StoragePower, error) {
	sectorSize, err := info.SealProof.SectorSize()
	if err != nil {
		return big.Zero(), err
	}
	return miner19.QAPowerForSector(sectorSize, info), nil
}
