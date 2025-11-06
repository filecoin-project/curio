package storageingest

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	cborutil "github.com/filecoin-project/go-cbor-util"
	"github.com/filecoin-project/go-padreader"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	miner2 "github.com/filecoin-project/go-state-types/builtin/v13/miner"
	verifreg13 "github.com/filecoin-project/go-state-types/builtin/v13/verifreg"
	"github.com/filecoin-project/go-state-types/builtin/v9/verifreg"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/builtin/market"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	verifregtypes "github.com/filecoin-project/lotus/chain/actors/builtin/verifreg"
	"github.com/filecoin-project/lotus/chain/types"
	lpiece "github.com/filecoin-project/lotus/storage/pipeline/piece"
)

const IdealEndEpochBuffer = 2 * builtin.EpochsInDay
const MaxEndEpochBufferUnverified = 180 * builtin.EpochsInDay

// assuming that snap takes up to 20min to get to submitting the message we want to avoid sectors from deadlines which will
// become immutable in the next 20min (40 epochs)
// NOTE: Don't set this value to more than one deadline (60 epochs)
var SnapImmutableDeadlineEpochsBuffer = abi.ChainEpoch(40)

type PieceIngesterSnap struct {
	ctx                  context.Context
	db                   *harmonydb.DB
	api                  PieceIngesterApi
	addToID              map[address.Address]int64
	idToAddr             map[abi.ActorID]address.Address
	minerDetails         map[int64]*mdetails
	maxWaitTime          *config.Dynamic[time.Duration]
	expectedSnapDuration abi.ChainEpoch
}

func NewPieceIngesterSnap(ctx context.Context, db *harmonydb.DB, api PieceIngesterApi, miners []address.Address, cfg *config.CurioConfig) (*PieceIngesterSnap, error) {
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

		proof, err := miner.PreferredSealProofTypeFromWindowPoStType(nv, mi.WindowPoStProofType, false)
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

	epochs := cfg.Market.StorageMarketConfig.MK12.ExpectedSnapSealDuration.Seconds() / float64(build.BlockDelaySecs)
	expectedEpochs := math.Ceil(epochs)

	pi := &PieceIngesterSnap{
		ctx:                  ctx,
		db:                   db,
		api:                  api,
		maxWaitTime:          cfg.Ingest.MaxDealWaitTime,
		addToID:              addToID,
		minerDetails:         minerDetails,
		idToAddr:             idToAddr,
		expectedSnapDuration: abi.ChainEpoch(int64(expectedEpochs)),
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

func (p *PieceIngesterSnap) Seal() error {
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
		if time.Since(*sector.openedAt) > p.maxWaitTime.Get() {
			log.Debugf("start sealing sector %d of miner %s: %s", sector.number, p.idToAddr[sector.miner].String(), "MaxWaitTime reached")
			return true
		}
		if sector.earliestStartEpoch < head.Height()+p.expectedSnapDuration {
			log.Debugf("start sealing sector %d of miner %s: %s", sector.number, p.idToAddr[sector.miner].String(), "earliest start epoch")
			return true
		}

		log.Debugw("not starting to seal sector", "sector", sector.number, "miner", p.idToAddr[sector.miner].String(), "csize", sector.currentSize, "open", time.Since(*sector.openedAt), "toStart", sector.earliestStartEpoch-(head.Height()+p.expectedSnapDuration))
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
					cn, err := tx.Exec(`INSERT INTO sectors_snap_pipeline (sp_id, sector_number, upgrade_proof) VALUES ($1, $2, $3);`, mid, sector.number, p.minerDetails[mid].updateProof)

					if err != nil {
						return false, xerrors.Errorf("adding sector to pipeline: %w", err)
					}

					if cn != 1 {
						return false, xerrors.Errorf("adding sector to pipeline: incorrect number of rows returned")
					}

					_, err = tx.Exec("SELECT transfer_and_delete_sorted_open_piece_snap($1, $2)", mid, sector.number)
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

func (p *PieceIngesterSnap) AllocatePieceToSector(ctx context.Context, tx *harmonydb.Tx, maddr address.Address, piece lpiece.PieceDealInfo, rawSize int64, source url.URL, header http.Header) (*abi.SectorNumber, *abi.RegisteredSealProof, error) {
	var psize abi.PaddedPieceSize

	if piece.PieceActivationManifest != nil {
		psize = piece.PieceActivationManifest.Size
	} else {
		psize = piece.DealProposal.PieceSize
	}

	var propJson []byte

	dataHdrJson, err := json.Marshal(header)
	if err != nil {
		return nil, nil, xerrors.Errorf("json.Marshal(header): %w", err)
	}

	vd := verifiedDeal{
		isVerified: false,
	}

	if piece.DealProposal != nil {
		// For snap we convert f05 deals to DDO

		alloc, err := p.api.StateGetAllocationIdForPendingDeal(ctx, piece.DealID, types.EmptyTSK)
		if err != nil {
			return nil, nil, xerrors.Errorf("getting allocation for deal %d: %w", piece.DealID, err)
		}
		clid, err := p.api.StateLookupID(ctx, piece.DealProposal.Client, types.EmptyTSK)
		if err != nil {
			return nil, nil, xerrors.Errorf("getting client address for deal %d: %w", piece.DealID, err)
		}

		clientId, err := address.IDFromAddress(clid)
		if err != nil {
			return nil, nil, xerrors.Errorf("getting client address for deal %d: %w", piece.DealID, err)
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
			return nil, nil, xerrors.Errorf("serializing deal id: %w", err)
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

	head, err := p.api.ChainHead(ctx)
	if err != nil {
		return nil, nil, xerrors.Errorf("getting chain head: %w", err)
	}

	var maxExpiration int64
	vd.isVerified = piece.PieceActivationManifest.VerifiedAllocationKey != nil
	if vd.isVerified {
		client, err := address.NewIDAddress(uint64(piece.PieceActivationManifest.VerifiedAllocationKey.Client))
		if err != nil {
			return nil, nil, xerrors.Errorf("getting client address from actor ID: %w", err)
		}
		alloc, err := p.api.StateGetAllocation(ctx, client, verifregtypes.AllocationId(piece.PieceActivationManifest.VerifiedAllocationKey.ID), types.EmptyTSK)
		if err != nil {
			return nil, nil, xerrors.Errorf("getting allocation details for %d: %w", piece.PieceActivationManifest.VerifiedAllocationKey.ID, err)
		}
		if alloc == nil {
			return nil, nil, xerrors.Errorf("no allocation found for ID %d: %w", piece.PieceActivationManifest.VerifiedAllocationKey.ID, err)
		}
		vd.tmin = alloc.TermMin
		vd.tmax = alloc.TermMax

		maxExpiration = int64(head.Height() + alloc.TermMax)
	} else {
		maxExpiration = int64(piece.DealSchedule.EndEpoch) + MaxEndEpochBufferUnverified
	}
	propJson, err = json.Marshal(piece.PieceActivationManifest)
	if err != nil {
		return nil, nil, xerrors.Errorf("json.Marshal(piece.PieceActivationManifest): %w", err)
	}

	// Reject incorrect sized online deals except verified deal less than 1 MiB because verified deals can be 1 MiB minimum even if rawSize is much lower
	if psize != padreader.PaddedSize(uint64(rawSize)).Padded() && (!vd.isVerified || psize > abi.PaddedPieceSize(1<<20)) {
		return nil, nil, xerrors.Errorf("raw size doesn't match padded piece size")
	}

	// Try to allocate the piece to an open sector
	allocated, ret, sp, err := p.allocateToExisting(ctx, tx, maddr, piece, psize, rawSize, source, dataHdrJson, propJson, vd)
	if err != nil {
		return nil, nil, err
	}
	if allocated {
		return ret, sp, nil
	}

	// non-mutable deadline is the current deadline and the next one. Doesn't matter if the current one was proven or not.

	curDeadline, err := p.api.StateMinerProvingDeadline(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return nil, nil, xerrors.Errorf("getting proving deadline: %w", err)
	}

	dlIdxImmutableCur := curDeadline.Index
	dlIdxImmutableNext := (curDeadline.Index + 1) % curDeadline.WPoStPeriodDeadlines

	// The deadline which might become mutable soon
	dlIdxImmutableNextNext := dlIdxImmutableNext
	epochsInDeadline := curDeadline.CurrentEpoch - curDeadline.Open // how many into the deadline we are
	epochsPerDeadline := curDeadline.WPoStProvingPeriod / abi.ChainEpoch(curDeadline.WPoStPeriodDeadlines)

	if epochsInDeadline >= epochsPerDeadline-SnapImmutableDeadlineEpochsBuffer {
		dlIdxImmutableNextNext = (dlIdxImmutableNext + 1) % curDeadline.WPoStPeriodDeadlines
	}

	// Allocation to open sector failed, create a new sector and add the piece to it

	// TX

	// select sectors_meta where is_cc = true
	// now we have a list of all sectors that are CC
	// We're selecting the best sector for *this* piece, not any other / pending pieces (like lotus-miner, which made this logic needlessly complex)
	// We want a sector with an expiration above the piece expiration but below termMax
	// Ideally we want the sector expiration te be a day or two above the deal expiration, so that it can accomodate other deals with slightly longer expirations
	// Nice to have: use preloaded sectors

	// /TX

	type CandidateSector struct {
		Sector       int64 `db:"sector_num"`
		RegSealProof int64 `db:"reg_seal_proof"`
		Expiration   int64 `db:"expiration_epoch"`
	}

	// maxExpiration = maybe(max sector expiration_epoch)
	// minExpiration = piece.DealSchedule.EndEpoch
	// ideal expiration = minExpiration + 2 days
	rows, err := tx.Query(`
			SELECT sm.sector_num, sm.reg_seal_proof, sm.expiration_epoch
			FROM sectors_meta sm
			LEFT JOIN sectors_snap_pipeline ssp on sm.sp_id = ssp.sp_id and sm.sector_num = ssp.sector_number
			LEFT JOIN open_sector_pieces osp on sm.sp_id = osp.sp_id and sm.sector_num = osp.sector_number and osp.piece_index = 0
			WHERE sm.is_cc = true AND ssp.start_time IS NULL AND osp.created_at IS NULL
			  AND sm.sp_id = $4
			  AND sm.expiration_epoch IS NOT NULL
			  AND sm.expiration_epoch > $1
			  AND ($2 = 0 OR sm.expiration_epoch < $2)
			  AND deadline IS NOT NULL AND deadline NOT IN ($5, $6, $7)
			ORDER BY ABS(sm.expiration_epoch - ($1 + $3))
		`, int64(piece.DealSchedule.EndEpoch), maxExpiration, IdealEndEpochBuffer, p.addToID[maddr], dlIdxImmutableCur, dlIdxImmutableNext, dlIdxImmutableNextNext)
	if err != nil {
		return nil, nil, xerrors.Errorf("allocating sector numbers: %w", err)
	}
	defer rows.Close()

	deadlineCache := map[uint64][]api.Partition{}
	var tried int
	var bestCandidate *CandidateSector

	for rows.Next() {
		var candidate CandidateSector
		err := rows.Scan(&candidate.Sector, &candidate.RegSealProof, &candidate.Expiration)
		if err != nil {
			return nil, nil, xerrors.Errorf("scanning row: %w", err)
		}
		tried++

		sloc, err := p.api.StateSectorPartition(ctx, maddr, abi.SectorNumber(candidate.Sector), types.EmptyTSK)
		if err != nil {
			return nil, nil, xerrors.Errorf("getting sector locations: %w", err)
		}

		if _, ok := deadlineCache[sloc.Deadline]; !ok {
			dls, err := p.api.StateMinerPartitions(ctx, maddr, sloc.Deadline, types.EmptyTSK)
			if err != nil {
				return nil, nil, xerrors.Errorf("getting partitions: %w", err)
			}

			deadlineCache[sloc.Deadline] = dls
		}

		dl := deadlineCache[sloc.Deadline]
		if len(dl) <= int(sloc.Partition) {
			return nil, nil, xerrors.Errorf("partition %d not found in deadline %d", sloc.Partition, sloc.Deadline)
		}
		part := dl[sloc.Partition]

		active, err := part.ActiveSectors.IsSet(uint64(candidate.Sector))
		if err != nil {
			return nil, nil, xerrors.Errorf("checking active sectors: %w", err)
		}
		if !active {
			live, err1 := part.LiveSectors.IsSet(uint64(candidate.Sector))
			faulty, err2 := part.FaultySectors.IsSet(uint64(candidate.Sector))
			recovering, err3 := part.RecoveringSectors.IsSet(uint64(candidate.Sector))
			if err1 != nil || err2 != nil || err3 != nil {
				return nil, nil, xerrors.Errorf("checking sector status: %w, %w, %w", err1, err2, err3)
			}

			log.Debugw("sector not active, skipping", "sector", candidate.Sector, "live", live, "faulty", faulty, "recovering", recovering)
			continue
		}

		bestCandidate = &candidate
		break
	}

	if err := rows.Err(); err != nil {
		return nil, nil, xerrors.Errorf("iterating rows: %w", err)
	}

	rows.Close()

	if bestCandidate == nil {
		minEpoch := piece.DealSchedule.EndEpoch
		maxEpoch := abi.ChainEpoch(maxExpiration)

		minEpochDays := (minEpoch - head.Height()) / builtin.EpochsInDay
		maxEpochDays := (maxEpoch - head.Height()) / builtin.EpochsInDay

		return nil, nil, xerrors.Errorf("no suitable sectors found, minEpoch: %d, maxEpoch: %d, minExpirationDays: %d, maxExpirationDays: %d (avoiding deadlines %d,%d,%d)", minEpoch, maxEpoch, minEpochDays, maxEpochDays, dlIdxImmutableCur, dlIdxImmutableNext, dlIdxImmutableNextNext)
	}

	candidate := *bestCandidate

	si, err := p.api.StateSectorGetInfo(ctx, maddr, abi.SectorNumber(candidate.Sector), types.EmptyTSK)
	if err != nil {
		return nil, nil, xerrors.Errorf("getting sector info: %w", err)
	}

	sectorLifeTime := si.Expiration - head.Height()
	if sectorLifeTime < 0 {
		return nil, nil, xerrors.Errorf("sector lifetime is negative!?")
	}
	if piece.DealSchedule.EndEpoch > si.Expiration {
		return nil, nil, xerrors.Errorf("sector expiration is too soon: %d < %d", si.Expiration, piece.DealSchedule.EndEpoch)
	}
	if maxExpiration != 0 && si.Expiration > abi.ChainEpoch(maxExpiration) {
		return nil, nil, xerrors.Errorf("sector expiration is too late: %d > %d", si.Expiration, maxExpiration)
	}

	// info log detailing EVERYTHING including all the epoch bounds
	log.Infow("allocating piece to sector",
		"sector", candidate.Sector,
		"expiration", si.Expiration,
		"sectorLifeTime", sectorLifeTime,
		"dealStartEpoch", piece.DealSchedule.StartEpoch,
		"dealEndEpoch", piece.DealSchedule.EndEpoch,
		"maxExpiration", maxExpiration,
		"avoidingDeadlines", []int{int(dlIdxImmutableCur), int(dlIdxImmutableNext), int(dlIdxImmutableNextNext)},
	)

	_, err = tx.Exec(`SELECT insert_snap_ddo_piece($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		p.addToID[maddr], candidate.Sector, 0,
		piece.PieceActivationManifest.CID, piece.PieceActivationManifest.Size,
		source.String(), dataHdrJson, rawSize, !piece.KeepUnsealed,
		piece.DealSchedule.StartEpoch, piece.DealSchedule.EndEpoch, propJson)
	if err != nil {
		return nil, nil, xerrors.Errorf("adding deal to sector: %w", err)
	}

	num := abi.SectorNumber(uint64(candidate.Sector))
	rsp := abi.RegisteredSealProof(candidate.RegSealProof)

	return &num, &rsp, nil
}

func (p *PieceIngesterSnap) allocateToExisting(ctx context.Context, tx *harmonydb.Tx, maddr address.Address, piece lpiece.PieceDealInfo, psize abi.PaddedPieceSize, rawSize int64, source url.URL, dataHdrJson, propJson []byte, vd verifiedDeal) (bool, *abi.SectorNumber, *abi.RegisteredSealProof, error) {
	var ret abi.SectorNumber
	var retSp abi.RegisteredSealProof
	var allocated bool
	var rerr error

	openSectors, err := p.getOpenSectors(tx, p.addToID[maddr])
	if err != nil {
		return false, nil, nil, err
	}

	head, err := p.api.ChainHead(ctx)
	if err != nil {
		return false, nil, nil, err
	}

	for _, sec := range openSectors {
		sec := sec
		// Check that each sector has unique pieces
		var nextSector bool
		for i := range sec.pieces {
			if sec.pieces[i].cid == piece.PieceCID().String() && sec.pieces[i].size == piece.Size() {
				nextSector = true
				break
			}
		}
		if nextSector {
			continue
		}
		if sec.currentSize+psize <= abi.PaddedPieceSize(p.minerDetails[p.addToID[maddr]].sectorSize) {
			si, err := p.api.StateSectorGetInfo(ctx, maddr, sec.number, types.EmptyTSK)
			if err != nil {
				log.Errorw("getting sector info", "error", err, "sector", sec.number, "miner", maddr)
				continue
			}

			if vd.isVerified {

				sectorLifeTime := si.Expiration - head.Height()
				if sectorLifeTime < 0 {
					log.Errorw("sector lifetime is negative", "sector", sec.number, "miner", maddr, "lifetime", sectorLifeTime)
					continue
				}

				// Allocation's TMin must fit in sector and TMax should be at least sector lifetime or more
				// Based on https://github.com/filecoin-project/builtin-actors/blob/a0e34d22665ac8c84f02fea8a099216f29ffaeeb/actors/verifreg/src/lib.rs#L1071-L1086
				if sectorLifeTime <= vd.tmin && sectorLifeTime >= vd.tmax {
					continue
				}
			}

			ret = sec.number
			retSp = si.SealProof

			// Insert DDO deal to DB for the sector
			cn, err := tx.Exec(`SELECT insert_snap_ddo_piece($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
				p.addToID[maddr], sec.number, sec.index+1,
				piece.PieceActivationManifest.CID, piece.PieceActivationManifest.Size,
				source.String(), dataHdrJson, rawSize, !piece.KeepUnsealed,
				piece.DealSchedule.StartEpoch, piece.DealSchedule.EndEpoch, propJson)

			if err != nil {
				return false, nil, nil, fmt.Errorf("adding deal to sector: %v", err)
			}

			if cn != 1 {
				return false, nil, nil, xerrors.Errorf("expected one piece")
			}

			allocated = true
			break
		}
	}

	return allocated, &ret, &retSp, rerr
}

func (p *PieceIngesterSnap) getOpenSectors(tx *harmonydb.Tx, mid int64) ([]*openSector, error) {
	// Get current open sector pieces from DB
	var pieces []pieceDetails
	err := tx.Select(&pieces, `
					SELECT
					    sp_id,
					    sector_number,
					    piece_cid,
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
		if piece.CreatedAt.Time.Before(*cur) {
			return &piece.CreatedAt.Time
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
				openedAt:           &pi.CreatedAt.Time,
				latestEndEpoch:     getEndEpoch(pi.EndEpoch, 0),
				pieces: []pieceInfo{
					{
						cid:  pi.Cid,
						size: pi.Size,
					},
				},
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
		sector.pieces = append(sector.pieces, pieceInfo{
			cid:  pi.Cid,
			size: pi.Size,
		})
	}

	var os []*openSector

	for _, v := range sectorMap {
		v := v
		os = append(os, v)
	}

	return os, nil
}

func (p *PieceIngesterSnap) GetExpectedSealDuration() abi.ChainEpoch {
	return p.expectedSnapDuration
}
