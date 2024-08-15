package storageingest

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	logging "github.com/ipfs/go-log/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"
	verifregtypes "github.com/filecoin-project/go-state-types/builtin/v9/verifreg"
	"github.com/filecoin-project/go-state-types/dline"
	"github.com/filecoin-project/go-state-types/network"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/tasks/seal"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	lpiece "github.com/filecoin-project/lotus/storage/pipeline/piece"
)

const loopFrequency = 10 * time.Second

var log = logging.Logger("storage-ingest")

type Ingester interface {
	AllocatePieceToSector(ctx context.Context, tx *harmonydb.Tx, maddr address.Address, piece lpiece.PieceDealInfo, rawSize int64, source url.URL, header http.Header) (api.SectorOffset, error)
	SectorStartSealing(ctx context.Context, maddr address.Address, sector abi.SectorNumber) error
	GetExpectedSealDuration() abi.ChainEpoch
}

type PieceIngesterApi interface {
	ChainHead(context.Context) (*types.TipSet, error)
	StateMinerInfo(context.Context, address.Address, types.TipSetKey) (api.MinerInfo, error)
	StateMinerAllocated(ctx context.Context, a address.Address, key types.TipSetKey) (*bitfield.BitField, error)
	StateNetworkVersion(ctx context.Context, key types.TipSetKey) (network.Version, error)
	StateGetAllocation(ctx context.Context, clientAddr address.Address, allocationId verifregtypes.AllocationId, tsk types.TipSetKey) (*verifregtypes.Allocation, error)
	StateGetAllocationForPendingDeal(ctx context.Context, dealId abi.DealID, tsk types.TipSetKey) (*verifregtypes.Allocation, error)
	StateGetAllocationIdForPendingDeal(ctx context.Context, dealId abi.DealID, tsk types.TipSetKey) (verifregtypes.AllocationId, error)
	StateLookupID(context.Context, address.Address, types.TipSetKey) (address.Address, error)
	StateSectorGetInfo(ctx context.Context, maddr address.Address, sectorNumber abi.SectorNumber, tsk types.TipSetKey) (*miner.SectorOnChainInfo, error)

	StateSectorPartition(ctx context.Context, maddr address.Address, sectorNumber abi.SectorNumber, tok types.TipSetKey) (*miner.SectorLocation, error)
	StateMinerPartitions(ctx context.Context, m address.Address, dlIdx uint64, tsk types.TipSetKey) ([]api.Partition, error)
	StateMinerProvingDeadline(context.Context, address.Address, types.TipSetKey) (*dline.Info, error)
}

type openSector struct {
	miner              abi.ActorID
	number             abi.SectorNumber
	currentSize        abi.PaddedPieceSize
	earliestStartEpoch abi.ChainEpoch
	index              uint64
	openedAt           *time.Time
	latestEndEpoch     abi.ChainEpoch
}

type mdetails struct {
	sealProof   abi.RegisteredSealProof
	updateProof abi.RegisteredUpdateProof
	sectorSize  abi.SectorSize
}

type PieceIngester struct {
	ctx                  context.Context
	db                   *harmonydb.DB
	api                  PieceIngesterApi
	addToID              map[address.Address]int64
	idToAddr             map[abi.ActorID]address.Address
	minerDetails         map[int64]*mdetails
	sealRightNow         bool // Should be true only for CurioAPI AllocatePieceToSector method
	maxWaitTime          time.Duration
	expectedSealDuration abi.ChainEpoch
}

type verifiedDeal struct {
	isVerified bool
	tmin       abi.ChainEpoch
	tmax       abi.ChainEpoch
}

func NewPieceIngester(ctx context.Context, db *harmonydb.DB, api PieceIngesterApi, miners []address.Address, sealRightNow bool, cfg *config.CurioConfig) (*PieceIngester, error) {
	if len(miners) == 0 {
		return nil, xerrors.Errorf("no miners provided")
	}

	addToID := make(map[address.Address]int64)
	minerDetails := make(map[int64]*mdetails)
	idToAddr := make(map[abi.ActorID]address.Address)

	for _, maddr := range miners {
		mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return nil, err
		}

		mid, err := address.IDFromAddress(maddr)
		if err != nil {
			return nil, xerrors.Errorf("getting miner ID: %w", err)
		}

		nv, err := api.StateNetworkVersion(ctx, types.EmptyTSK)
		if err != nil {
			return nil, xerrors.Errorf("getting network version: %w", err)
		}

		proof, err := miner.PreferredSealProofTypeFromWindowPoStType(nv, mi.WindowPoStProofType, cfg.Subsystems.UseSyntheticPoRep)
		if err != nil {
			return nil, xerrors.Errorf("getting preferred seal proof type: %w", err)
		}

		proofInfo, ok := abi.SealProofInfos[proof]
		if !ok {
			return nil, xerrors.Errorf("getting seal proof type: %w", err)
		}

		addToID[maddr] = int64(mid)
		minerDetails[int64(mid)] = &mdetails{
			sealProof:   proof,
			sectorSize:  mi.SectorSize,
			updateProof: proofInfo.UpdateProof,
		}
		idToAddr[abi.ActorID(mid)] = maddr
	}

	epochs := time.Duration(cfg.Market.StorageMarketConfig.MK12.ExpectedPoRepSealDuration).Seconds() / float64(build.BlockDelaySecs)
	expectedEpochs := math.Ceil(epochs)

	pi := &PieceIngester{
		ctx:                  ctx,
		db:                   db,
		api:                  api,
		sealRightNow:         sealRightNow,
		maxWaitTime:          time.Duration(cfg.Ingest.MaxDealWaitTime),
		addToID:              addToID,
		minerDetails:         minerDetails,
		idToAddr:             idToAddr,
		expectedSealDuration: abi.ChainEpoch(int64(expectedEpochs)),
	}

	go pi.start()

	return pi, nil
}

func (p *PieceIngester) start() {
	ticker := time.NewTicker(loopFrequency)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			err := p.Seal()
			if err != nil {
				log.Error(err)
			}
		}
	}
}

func (p *PieceIngester) Seal() error {
	head, err := p.api.ChainHead(p.ctx)
	if err != nil {
		return xerrors.Errorf("getting chain head: %w", err)
	}

	shouldSeal := func(sector *openSector) bool {
		// Start sealing a sector if
		// 1. If sector is full
		// 2. We have been waiting for MaxWaitDuration
		// 3. StartEpoch is currentEpoch + expectedSealDuration
		if sector.currentSize == abi.PaddedPieceSize(p.minerDetails[int64(sector.miner)].sectorSize) {
			log.Debugf("start sealing sector %d of miner %s: %s", sector.number, p.idToAddr[sector.miner].String(), "sector full")
			return true
		}
		if time.Since(*sector.openedAt) > p.maxWaitTime {
			log.Debugf("start sealing sector %d of miner %s: %s", sector.number, p.idToAddr[sector.miner].String(), "MaxWaitTime reached")
			return true
		}
		if sector.earliestStartEpoch < head.Height()+p.expectedSealDuration {
			log.Debugf("start sealing sector %d of miner %s: %s", sector.number, p.idToAddr[sector.miner].String(), "earliest start epoch")
			return true
		}
		return false
	}

	comm, err := p.db.BeginTransaction(p.ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		for _, mid := range p.addToID {
			openSectors, err := p.getOpenSectors(tx, mid)
			if err != nil {
				return false, err
			}

			for _, sector := range openSectors {
				sector := sector
				if shouldSeal(sector) {
					// Start sealing the sector
					cn, err := tx.Exec(`INSERT INTO sectors_sdr_pipeline (sp_id, sector_number, reg_seal_proof) VALUES ($1, $2, $3);`, mid, sector.number, p.minerDetails[mid].sealProof)

					if err != nil {
						return false, xerrors.Errorf("adding sector to pipeline: %w", err)
					}

					if cn != 1 {
						return false, xerrors.Errorf("adding sector to pipeline: incorrect number of rows returned")
					}

					_, err = tx.Exec("SELECT transfer_and_delete_open_piece($1, $2)", mid, sector.number)
					if err != nil {
						return false, xerrors.Errorf("adding sector to pipeline: %w", err)
					}
				}

			}

		}
		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return xerrors.Errorf("start sealing sector: %w", err)
	}

	if !comm {
		return xerrors.Errorf("start sealing sector: commit failed")
	}

	return nil
}

func (p *PieceIngester) AllocatePieceToSector(ctx context.Context, tx *harmonydb.Tx, maddr address.Address, piece lpiece.PieceDealInfo, rawSize int64, source url.URL, header http.Header) (api.SectorOffset, error) {
	var psize abi.PaddedPieceSize

	if piece.PieceActivationManifest != nil {
		psize = piece.PieceActivationManifest.Size
	} else {
		psize = piece.DealProposal.PieceSize
	}

	// check raw size
	if psize != padreader.PaddedSize(uint64(rawSize)).Padded() {
		return api.SectorOffset{}, xerrors.Errorf("raw size doesn't match padded piece size")
	}

	var propJson []byte

	dataHdrJson, err := json.Marshal(header)
	if err != nil {
		return api.SectorOffset{}, xerrors.Errorf("json.Marshal(header): %w", err)
	}

	vd := verifiedDeal{
		isVerified: false,
	}

	if piece.DealProposal != nil {
		vd.isVerified = piece.DealProposal.VerifiedDeal
		if vd.isVerified {
			alloc, err := p.api.StateGetAllocationForPendingDeal(ctx, piece.DealID, types.EmptyTSK)
			if err != nil {
				return api.SectorOffset{}, xerrors.Errorf("getting pending allocation for deal %d: %w", piece.DealID, err)
			}
			if alloc == nil {
				return api.SectorOffset{}, xerrors.Errorf("no allocation found for deal %d: %w", piece.DealID, err)
			}
			vd.tmin = alloc.TermMin
			vd.tmax = alloc.TermMax
		}
		propJson, err = json.Marshal(piece.DealProposal)
		if err != nil {
			return api.SectorOffset{}, xerrors.Errorf("json.Marshal(piece.DealProposal): %w", err)
		}
	} else {
		vd.isVerified = piece.PieceActivationManifest.VerifiedAllocationKey != nil
		if vd.isVerified {
			client, err := address.NewIDAddress(uint64(piece.PieceActivationManifest.VerifiedAllocationKey.Client))
			if err != nil {
				return api.SectorOffset{}, xerrors.Errorf("getting client address from actor ID: %w", err)
			}
			alloc, err := p.api.StateGetAllocation(ctx, client, verifregtypes.AllocationId(piece.PieceActivationManifest.VerifiedAllocationKey.ID), types.EmptyTSK)
			if err != nil {
				return api.SectorOffset{}, xerrors.Errorf("getting allocation details for %d: %w", piece.PieceActivationManifest.VerifiedAllocationKey.ID, err)
			}
			if alloc == nil {
				return api.SectorOffset{}, xerrors.Errorf("no allocation found for ID %d: %w", piece.PieceActivationManifest.VerifiedAllocationKey.ID, err)
			}
			vd.tmin = alloc.TermMin
			vd.tmax = alloc.TermMax
		}
		propJson, err = json.Marshal(piece.PieceActivationManifest)
		if err != nil {
			return api.SectorOffset{}, xerrors.Errorf("json.Marshal(piece.PieceActivationManifest): %w", err)
		}
	}

	if !p.sealRightNow {
		// Try to allocate the piece to an open sector
		allocated, ret, err := p.allocateToExisting(tx, maddr, piece, psize, rawSize, source, dataHdrJson, propJson, vd)
		if err != nil {
			return api.SectorOffset{}, err
		}
		if allocated {
			return ret, nil
		}
	}

	// Allocation to open sector failed, create a new sector and add the piece to it
	num, err := seal.AllocateSectorNumbers(ctx, p.api, tx, maddr, 1)
	if err != nil {
		return api.SectorOffset{}, xerrors.Errorf("allocating new sector: %w", err)
	}

	if len(num) != 1 {
		return api.SectorOffset{}, xerrors.Errorf("expected one sector number")
	}
	n := num[0]

	// Assign piece to new sector
	if piece.DealProposal != nil {
		_, err = tx.Exec(`SELECT insert_sector_market_piece($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			p.addToID[maddr], n, 0,
			piece.DealProposal.PieceCID, piece.DealProposal.PieceSize,
			source.String(), dataHdrJson, rawSize, !piece.KeepUnsealed,
			piece.PublishCid, piece.DealID, propJson, piece.DealSchedule.StartEpoch, piece.DealSchedule.EndEpoch)
		if err != nil {
			return api.SectorOffset{}, xerrors.Errorf("adding deal to sector: %w", err)
		}
	} else {
		_, err = tx.Exec(`SELECT insert_sector_ddo_piece($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			p.addToID[maddr], n, 0,
			piece.PieceActivationManifest.CID, piece.PieceActivationManifest.Size,
			source.String(), dataHdrJson, rawSize, !piece.KeepUnsealed,
			piece.DealSchedule.StartEpoch, piece.DealSchedule.EndEpoch, propJson)
		if err != nil {
			return api.SectorOffset{}, xerrors.Errorf("adding deal to sector: %w", err)
		}
	}

	if p.sealRightNow {
		err = p.SectorStartSealing(ctx, maddr, num[0])
		if err != nil {
			return api.SectorOffset{}, xerrors.Errorf("SectorStartSealing: %w", err)
		}
	}

	return api.SectorOffset{
		Sector: n,
		Offset: 0,
	}, nil
}

func (p *PieceIngester) allocateToExisting(tx *harmonydb.Tx, maddr address.Address, piece lpiece.PieceDealInfo, psize abi.PaddedPieceSize, rawSize int64, source url.URL, dataHdrJson, propJson []byte, vd verifiedDeal) (bool, api.SectorOffset, error) {
	var ret api.SectorOffset
	var allocated bool
	var rerr error

	openSectors, err := p.getOpenSectors(tx, p.addToID[maddr])
	if err != nil {
		return false, api.SectorOffset{}, err
	}

	for _, sec := range openSectors {
		sec := sec
		if sec.currentSize+psize <= abi.PaddedPieceSize(p.minerDetails[p.addToID[maddr]].sectorSize) {
			if vd.isVerified {
				sectorLifeTime := sec.latestEndEpoch - sec.earliestStartEpoch
				// Allocation's TMin must fit in sector and TMax should be at least sector lifetime or more
				// Based on https://github.com/filecoin-project/builtin-actors/blob/a0e34d22665ac8c84f02fea8a099216f29ffaeeb/actors/verifreg/src/lib.rs#L1071-L1086
				if sectorLifeTime <= vd.tmin && sectorLifeTime >= vd.tmax {
					continue
				}
			}

			ret.Sector = sec.number
			ret.Offset = sec.currentSize

			// Insert market deal to DB for the sector
			if piece.DealProposal != nil {
				cn, err := tx.Exec(`SELECT insert_sector_market_piece($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
					p.addToID[maddr], sec.number, sec.index+1,
					piece.DealProposal.PieceCID, piece.DealProposal.PieceSize,
					source.String(), dataHdrJson, rawSize, !piece.KeepUnsealed,
					piece.PublishCid, piece.DealID, propJson, piece.DealSchedule.StartEpoch, piece.DealSchedule.EndEpoch)

				if err != nil {
					return false, api.SectorOffset{}, fmt.Errorf("adding deal to sector: %v", err)
				}

				if cn != 1 {
					return false, api.SectorOffset{}, xerrors.Errorf("expected one piece")
				}

			} else { // Insert DDO deal to DB for the sector
				cn, err := tx.Exec(`SELECT insert_sector_ddo_piece($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
					p.addToID[maddr], sec.number, sec.index+1,
					piece.PieceActivationManifest.CID, piece.PieceActivationManifest.Size,
					source.String(), dataHdrJson, rawSize, !piece.KeepUnsealed,
					piece.DealSchedule.StartEpoch, piece.DealSchedule.EndEpoch, propJson)

				if err != nil {
					return false, api.SectorOffset{}, fmt.Errorf("adding deal to sector: %v", err)
				}

				if cn != 1 {
					return false, api.SectorOffset{}, xerrors.Errorf("expected one piece")
				}

			}
			allocated = true
			break
		}
	}

	return allocated, ret, rerr

}

type pieceDetails struct {
	Miner      abi.ActorID         `db:"sp_id"`
	Sector     abi.SectorNumber    `db:"sector_number"`
	Size       abi.PaddedPieceSize `db:"piece_size"`
	StartEpoch abi.ChainEpoch      `db:"deal_start_epoch"`
	EndEpoch   abi.ChainEpoch      `db:"deal_end_epoch"`
	Index      uint64              `db:"piece_index"`
	CreatedAt  *time.Time          `db:"created_at"`
}

func (p *PieceIngester) SectorStartSealing(ctx context.Context, maddr address.Address, sector abi.SectorNumber) error {
	comm, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// Get current open sector pieces from DB
		var pieces []pieceDetails
		err = tx.Select(&pieces, `
					SELECT
					    sp_id,
					    sector_number,
						piece_size,
						piece_index,
						COALESCE(direct_start_epoch, f05_deal_start_epoch, 0) AS deal_start_epoch,
						COALESCE(direct_end_epoch, f05_deal_end_epoch, 0) AS deal_end_epoch,
						created_at
					FROM
						open_sector_pieces
					WHERE
						sp_id = $1 AND sector_number = $2 AND is_snap = false
					ORDER BY
						piece_index DESC;`, p.addToID[maddr], sector)
		if err != nil {
			return false, xerrors.Errorf("getting open sectors from DB")
		}

		if len(pieces) < 1 {
			return false, xerrors.Errorf("sector %d is not waiting to be sealed", sector)
		}

		cn, err := tx.Exec(`INSERT INTO sectors_sdr_pipeline (sp_id, sector_number, reg_seal_proof) VALUES ($1, $2, $3);`, p.addToID[maddr], sector, p.minerDetails[p.addToID[maddr]].sealProof)

		if err != nil {
			return false, xerrors.Errorf("adding sector to pipeline: %w", err)
		}

		if cn != 1 {
			return false, xerrors.Errorf("incorrect number of rows returned")
		}

		_, err = tx.Exec("SELECT transfer_and_delete_open_piece($1, $2)", p.addToID[maddr], sector)
		if err != nil {
			return false, xerrors.Errorf("adding sector to pipeline: %w", err)
		}

		return true, nil

	}, harmonydb.OptionRetry())

	if err != nil {
		return xerrors.Errorf("start sealing sector: %w", err)
	}

	if !comm {
		return xerrors.Errorf("start sealing sector: commit failed")
	}

	return nil
}

func (p *PieceIngester) getOpenSectors(tx *harmonydb.Tx, mid int64) ([]*openSector, error) {
	// Get current open sector pieces from DB
	var pieces []pieceDetails
	err := tx.Select(&pieces, `
					SELECT
					    sp_id,
					    sector_number,
						piece_size,
						piece_index,
						COALESCE(direct_start_epoch, f05_deal_start_epoch, 0) AS deal_start_epoch,
						COALESCE(direct_end_epoch, f05_deal_end_epoch, 0) AS deal_end_epoch,
						created_at
					FROM
						open_sector_pieces
					WHERE
						sp_id = $1 AND is_snap = false
					ORDER BY
						piece_index DESC;`, mid)
	if err != nil {
		return nil, xerrors.Errorf("getting open sectors from DB")
	}

	getStartEpoch := func(new abi.ChainEpoch, cur abi.ChainEpoch) abi.ChainEpoch {
		if cur > 0 && cur < new {
			return cur
		}
		return new
	}

	getEndEpoch := func(new abi.ChainEpoch, cur abi.ChainEpoch) abi.ChainEpoch {
		if cur > 0 && cur > new {
			return cur
		}
		return new
	}

	getOpenedAt := func(piece pieceDetails, cur *time.Time) *time.Time {
		if piece.CreatedAt.Before(*cur) {
			return piece.CreatedAt
		}
		return cur
	}

	sectorMap := map[abi.SectorNumber]*openSector{}
	for _, pi := range pieces {
		pi := pi
		sector, ok := sectorMap[pi.Sector]
		if !ok {
			sectorMap[pi.Sector] = &openSector{
				miner:              pi.Miner,
				number:             pi.Sector,
				currentSize:        pi.Size,
				earliestStartEpoch: getStartEpoch(pi.StartEpoch, 0),
				index:              pi.Index,
				openedAt:           pi.CreatedAt,
				latestEndEpoch:     getEndEpoch(pi.EndEpoch, 0),
			}
			continue
		}
		sector.currentSize += pi.Size
		sector.earliestStartEpoch = getStartEpoch(pi.StartEpoch, sector.earliestStartEpoch)
		sector.latestEndEpoch = getEndEpoch(pi.EndEpoch, sector.earliestStartEpoch)
		if sector.index < pi.Index {
			sector.index = pi.Index
		}
		sector.openedAt = getOpenedAt(pi, sector.openedAt)
	}

	var os []*openSector

	for _, v := range sectorMap {
		v := v
		os = append(os, v)
	}

	return os, nil
}

func (p *PieceIngester) GetExpectedSealDuration() abi.ChainEpoch {
	return p.expectedSealDuration
}
