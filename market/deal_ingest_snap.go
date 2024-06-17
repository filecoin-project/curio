package market

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/go-address"
	cborutil "github.com/filecoin-project/go-cbor-util"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	miner2 "github.com/filecoin-project/go-state-types/builtin/v13/miner"
	verifreg13 "github.com/filecoin-project/go-state-types/builtin/v13/verifreg"
	"github.com/filecoin-project/go-state-types/builtin/v9/verifreg"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/builtin/market"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	verifregtypes "github.com/filecoin-project/lotus/chain/actors/builtin/verifreg"
	"github.com/filecoin-project/lotus/chain/types"
	lpiece "github.com/filecoin-project/lotus/storage/pipeline/piece"
	"golang.org/x/xerrors"
	"net/http"
	"net/url"
	"time"
)

const IdealEndEpochBuffer = 2 * builtin.EpochsInDay

type PieceIngesterSnap struct {
	ctx                 context.Context
	db                  *harmonydb.DB
	api                 PieceIngesterApi
	miner               address.Address
	mid                 uint64 // miner ID
	windowPoStProofType abi.RegisteredPoStProof
	sectorSize          abi.SectorSize
	sealRightNow        bool // Should be true only for CurioAPI AllocatePieceToSector method
	maxWaitTime         time.Duration
}

func NewPieceIngesterSnap(ctx context.Context, db *harmonydb.DB, api PieceIngesterApi, maddr address.Address, sealRightNow bool, maxWaitTime time.Duration) (*PieceIngesterSnap, error) {
	mi, err := api.StateMinerInfo(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return nil, err
	}

	mid, err := address.IDFromAddress(maddr)
	if err != nil {
		return nil, xerrors.Errorf("getting miner ID: %w", err)
	}

	pi := &PieceIngesterSnap{
		ctx:                 ctx,
		db:                  db,
		api:                 api,
		sealRightNow:        sealRightNow,
		miner:               maddr,
		maxWaitTime:         maxWaitTime,
		sectorSize:          mi.SectorSize,
		windowPoStProofType: mi.WindowPoStProofType,
		mid:                 mid,
	}

	go pi.start()

	return pi, nil
}

func (p *PieceIngesterSnap) start() {
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

func (p *PieceIngesterSnap) Seal() error { //////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////// PROBS WORKS
	head, err := p.api.ChainHead(p.ctx)
	if err != nil {
		return xerrors.Errorf("getting chain head: %w", err)
	}

	upt, err := p.getUpgradeProofType()
	if err != nil {
		return xerrors.Errorf("getting seal proof type: %w", err)
	}

	shouldSeal := func(sector *openSector) bool {
		// Start sealing a sector if
		// 1. If sector is full
		// 2. We have been waiting for MaxWaitDuration
		// 3. StartEpoch is less than 8 hours // todo: make this config?
		if sector.currentSize == abi.PaddedPieceSize(p.sectorSize) {
			log.Debugf("start sealing sector %d of miner %d: %s", sector.number, p.miner.String(), "sector full")
			return true
		}
		if time.Since(*sector.openedAt) > p.maxWaitTime {
			log.Debugf("start sealing sector %d of miner %d: %s", sector.number, p.miner.String(), "MaxWaitTime reached")
			return true
		}
		if sector.earliestStartEpoch < head.Height()+abi.ChainEpoch(960) {
			log.Debugf("start sealing sector %d of miner %d: %s", sector.number, p.miner.String(), "earliest start epoch")
			return true
		}
		return false
	}

	comm, err := p.db.BeginTransaction(p.ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		openSectors, err := p.getOpenSectors(tx)
		if err != nil {
			return false, err
		}

		for _, sector := range openSectors {
			sector := sector
			if shouldSeal(sector) {
				// Start sealing the sector
				cn, err := tx.Exec(`INSERT INTO sectors_snap_pipeline (sp_id, sector_number, upgrade_proof) VALUES ($1, $2, $3);`, p.mid, sector.number, upt)

				if err != nil {
					return false, xerrors.Errorf("adding sector to pipeline: %w", err)
				}

				if cn != 1 {
					return false, xerrors.Errorf("adding sector to pipeline: incorrect number of rows returned")
				}

				_, err = tx.Exec("SELECT transfer_and_delete_open_piece_snap($1, $2)", p.mid, sector.number)
				if err != nil {
					return false, xerrors.Errorf("adding sector to pipeline: %w", err)
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

func (p *PieceIngesterSnap) AllocatePieceToSector(ctx context.Context, maddr address.Address, piece lpiece.PieceDealInfo, rawSize int64, source url.URL, header http.Header) (api.SectorOffset, error) {
	if maddr != p.miner {
		return api.SectorOffset{}, xerrors.Errorf("miner address doesn't match")
	}

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
		// For snap we convert f05 deals to DDO

		alloc, err := p.api.StateGetAllocationIdForPendingDeal(ctx, piece.DealID, types.EmptyTSK)
		if err != nil {
			return api.SectorOffset{}, xerrors.Errorf("getting allocation for deal %d: %w", piece.DealID, err)
		}
		clid, err := p.api.StateLookupID(ctx, piece.DealProposal.Client, types.EmptyTSK)
		if err != nil {
			return api.SectorOffset{}, xerrors.Errorf("getting client address for deal %d: %w", piece.DealID, err)
		}

		clientId, err := address.IDFromAddress(clid)
		if err != nil {
			return api.SectorOffset{}, xerrors.Errorf("getting client address for deal %d: %w", piece.DealID, err)
		}

		var vac *miner2.VerifiedAllocationKey
		if alloc != verifreg.NoAllocationID {
			vac = &miner2.VerifiedAllocationKey{
				Client: abi.ActorID(clientId),
				ID:     verifreg13.AllocationId(alloc),
			}
		}

		payload, err := cborutil.Dump(piece.DealID)
		if err != nil {
			return api.SectorOffset{}, xerrors.Errorf("serializing deal id: %w", err)
		}

		piece.PieceActivationManifest = &miner.PieceActivationManifest{
			CID:                   piece.PieceCID(),
			Size:                  piece.DealProposal.PieceSize,
			VerifiedAllocationKey: vac,
			Notify: []miner2.DataActivationNotification{
				{
					Address: market.Address,
					Payload: payload,
				},
			},
		}

		piece.DealProposal = nil
		piece.DealID = 0
		piece.PublishCid = nil
	}

	var maxExpiration int64
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

		maxExpiration = int64(piece.DealSchedule.EndEpoch + alloc.TermMax)
	}
	propJson, err = json.Marshal(piece.PieceActivationManifest)
	if err != nil {
		return api.SectorOffset{}, xerrors.Errorf("json.Marshal(piece.PieceActivationManifest): %w", err)
	}

	if !p.sealRightNow {
		// Try to allocate the piece to an open sector
		allocated, ret, err := p.allocateToExisting(ctx, piece, psize, rawSize, source, dataHdrJson, propJson, vd)
		if err != nil {
			return api.SectorOffset{}, err
		}
		if allocated {
			return ret, nil
		}
	}

	//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////// ////////////////////////////////// TODO get new sector!!
	// Allocation to open sector failed, create a new sector and add the piece to it

	// TX

	// select sectors_meta where is_cc = true
	// now we have a list of all sectors that are CC
	// We're selecting the best sector for *this* piece, not any other / pending pieces (like lotus-miner, which made this logic needlessly complex)
	// We want a sector with an expiration above the piece expiration but below termMax
	// Ideally we want the sector expiration te be a day or two above the deal expiration, so that it can accomodate other deals with slightly longer expirations
	// Nice to have: use preloaded sectors

	// /TX

	var num *int64
	_, err = p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		var candidates []struct {
			Sector     int64 `db:"sector_num"`
			Expiration int64 `db:"expiration_epoch"`
		}

		// maxExpiration = maybe(max sector expiration_epoch)
		// minExpiration = piece.DealSchedule.EndEpoch
		// ideal expiration = minExpiration + 2 days
		err = tx.Select(&candidates, `
			SELECT sector_num, expiration_epoch
			FROM sectors_meta
			WHERE is_cc = true AND sp_id = $4
			  AND expiration_epoch IS NOT NULL
			  AND expiration_epoch > $1
			  AND ($2 = 0 OR expiration_epoch < $2)
			ORDER BY ABS(expiration_epoch - ($1 + $3))
			LIMIT 10
		`, int64(piece.DealSchedule.EndEpoch), maxExpiration, IdealEndEpochBuffer, p.mid)
		if err != nil {
			return false, xerrors.Errorf("allocating sector numbers: %w", err)
		}

		if len(candidates) == 0 {
			return false, xerrors.Errorf("no suitable sectors found")
		}

		// todo - nice to have:
		//  * double check the sector expiration
		//  * check sector liveness
		//  * check deadline mutable

		candidate := candidates[0] // this one works best

		_, err = tx.Exec(`SELECT insert_snap_ddo_piece($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
			p.mid, candidate.Sector, 0,
			piece.PieceActivationManifest.CID, piece.PieceActivationManifest.Size,
			source.String(), dataHdrJson, rawSize, !piece.KeepUnsealed,
			piece.DealSchedule.StartEpoch, piece.DealSchedule.EndEpoch, propJson)
		if err != nil {
			return false, xerrors.Errorf("adding deal to sector: %w", err)
		}

		num = &candidate.Sector

		return true, nil
	}, harmonydb.OptionRetry())
	if err != nil {
		return api.SectorOffset{}, xerrors.Errorf("allocating sector numbers: %w", err)
	}

	if num == nil {
		return api.SectorOffset{}, xerrors.Errorf("expected one sector number")
	}

	if p.sealRightNow {
		err = p.SectorStartSealing(ctx, abi.SectorNumber(*num))
		if err != nil {
			return api.SectorOffset{}, xerrors.Errorf("SectorStartSealing: %w", err)
		}
	}

	return api.SectorOffset{
		Sector: abi.SectorNumber(*num),
		Offset: 0,
	}, nil
}

func (p *PieceIngesterSnap) allocateToExisting(ctx context.Context, piece lpiece.PieceDealInfo, psize abi.PaddedPieceSize, rawSize int64, source url.URL, dataHdrJson, propJson []byte, vd verifiedDeal) (bool, api.SectorOffset, error) {
	var ret api.SectorOffset
	var allocated bool
	var rerr error

	comm, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		openSectors, err := p.getOpenSectors(tx)
		if err != nil {
			return false, err
		}

		for _, sec := range openSectors {
			sec := sec
			if sec.currentSize+psize <= abi.PaddedPieceSize(p.sectorSize) {
				if vd.isVerified {
					sectorLifeTime := sec.latestEndEpoch - sec.earliestStartEpoch
					// Allocation's TMin must fit in sector and TMax should be at least sector lifetime or more
					// Based on https://github.com/filecoin-project/builtin-actors/blob/a0e34d22665ac8c84f02fea8a099216f29ffaeeb/actors/verifreg/src/lib.rs#L1071-L1086
					if sectorLifeTime <= vd.tmin && sectorLifeTime >= vd.tmax {
						continue
					}

					//  ////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////  TODO ADD SNAP SECTOR EXP CHECKS
				}

				ret.Sector = sec.number
				ret.Offset = sec.currentSize

				// Insert DDO deal to DB for the sector
				cn, err := tx.Exec(`SELECT insert_snap_ddo_piece($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
					p.mid, sec.number, sec.index+1,
					piece.PieceActivationManifest.CID, piece.PieceActivationManifest.Size,
					source.String(), dataHdrJson, rawSize, !piece.KeepUnsealed,
					piece.DealSchedule.StartEpoch, piece.DealSchedule.EndEpoch, propJson)

				if err != nil {
					return false, fmt.Errorf("adding deal to sector: %v", err)
				}

				if cn != 1 {
					return false, xerrors.Errorf("expected one piece")
				}

				allocated = true
				break
			}
		}
		return true, nil
	}, harmonydb.OptionRetry())

	if !comm {
		rerr = xerrors.Errorf("allocating piece to a sector: commit failed")
	}

	if err != nil {
		rerr = xerrors.Errorf("allocating piece to a sector: %w", err)
	}

	return allocated, ret, rerr

}

func (p *PieceIngesterSnap) SectorStartSealing(ctx context.Context, sector abi.SectorNumber) error {
	upt, err := p.getUpgradeProofType()
	if err != nil {
		return xerrors.Errorf("getting seal proof type: %w", err)
	}

	comm, err := p.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// Get current open sector pieces from DB
		var pieces []pieceDetails
		err = tx.Select(&pieces, `
					SELECT
					    sector_number,
						piece_size,
						piece_index,
						COALESCE(direct_start_epoch, f05_deal_start_epoch, 0) AS deal_start_epoch,
						COALESCE(direct_end_epoch, f05_deal_end_epoch, 0) AS deal_end_epoch,
						created_at
					FROM
						open_sector_pieces
					WHERE
						sp_id = $1 AND sector_number = $2 AND is_snap = true
					ORDER BY
						piece_index DESC;`, p.mid, sector)
		if err != nil {
			return false, xerrors.Errorf("getting open sectors from DB")
		}

		if len(pieces) < 1 {
			return false, xerrors.Errorf("sector %d is not waiting to be sealed", sector)
		}

		cn, err := tx.Exec(`INSERT INTO sectors_snap_pipeline (sp_id, sector_number, upgrade_proof) VALUES ($1, $2, $3);`, p.mid, sector, upt)

		if err != nil {
			return false, xerrors.Errorf("adding sector to pipeline: %w", err)
		}

		if cn != 1 {
			return false, xerrors.Errorf("incorrect number of rows returned")
		}

		_, err = tx.Exec("SELECT transfer_and_delete_open_piece_snap($1, $2)", p.mid, sector)
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

func (p *PieceIngesterSnap) getOpenSectors(tx *harmonydb.Tx) ([]*openSector, error) {
	// Get current open sector pieces from DB
	var pieces []pieceDetails
	err := tx.Select(&pieces, `
					SELECT
					    sector_number,
						piece_size,
						piece_index,
						COALESCE(direct_start_epoch, f05_deal_start_epoch, 0) AS deal_start_epoch,
						COALESCE(direct_end_epoch, f05_deal_end_epoch, 0) AS deal_end_epoch,
						created_at
					FROM
						open_sector_pieces
					WHERE
						sp_id = $1 AND is_snap = true
					ORDER BY
						piece_index DESC;`, p.mid)
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

func (p *PieceIngesterSnap) getUpgradeProofType() (abi.RegisteredUpdateProof, error) {
	nv, err := p.api.StateNetworkVersion(p.ctx, types.EmptyTSK)
	if err != nil {
		return 0, xerrors.Errorf("getting network version: %w", err)
	}

	spt, err := miner.PreferredSealProofTypeFromWindowPoStType(nv, p.windowPoStProofType, false)
	if err != nil {
		return 0, xerrors.Errorf("getting seal proof type: %w", err)
	}

	proofInfo, ok := abi.SealProofInfos[spt]
	if !ok {
		return 0, xerrors.Errorf("getting seal proof type: %w", err)
	}

	return proofInfo.UpdateProof, nil
}
