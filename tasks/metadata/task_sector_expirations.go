package metadata

import (
	"context"
	"time"

	cbor "github.com/ipfs/go-ipld-cbor"
	logging "github.com/ipfs/go-log/v2"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/harmony/resources"
	"github.com/filecoin-project/curio/harmony/taskhelp"
	"github.com/filecoin-project/curio/lib/curiochain"

	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/actors/builtin/verifreg"
	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("metadata")

const SectorMetadataRefreshInterval = 15 * time.Minute

type SectorMetadataNodeAPI interface {
	ChainHead(ctx context.Context) (*types.TipSet, error)
	StateGetActor(ctx context.Context, actor address.Address, tsk types.TipSetKey) (*types.Actor, error)
	StateSectorPartition(ctx context.Context, maddr address.Address, sectorNumber abi.SectorNumber, tok types.TipSetKey) (*miner.SectorLocation, error)
}

type SectorMetadata struct {
	db     *harmonydb.DB
	bstore curiochain.CurioBlockstore

	api SectorMetadataNodeAPI
}

func NewSectorMetadataTask(db *harmonydb.DB, bstore curiochain.CurioBlockstore, api SectorMetadataNodeAPI) *SectorMetadata {
	return &SectorMetadata{
		db:     db,
		bstore: bstore,
		api:    api,
	}
}

func (s *SectorMetadata) Do(taskID harmonytask.TaskID, stillOwned func() bool) (done bool, err error) {
	ctx := context.Background()

	// Get last refresh info
	var lastRefresh []struct {
		LastRefreshAt    *time.Time `db:"last_refresh_at"`
		LastRefreshEpoch *int64     `db:"last_refresh_epoch"`
		LastRefreshTsk   []byte     `db:"last_refresh_tsk"`
	}
	if err := s.db.Select(ctx, &lastRefresh, "SELECT last_refresh_at, last_refresh_epoch, last_refresh_tsk FROM sectors_meta_updates LIMIT 1"); err != nil {
		return false, xerrors.Errorf("getting last refresh info: %w", err)
	}

	if len(lastRefresh) > 0 && lastRefresh[0].LastRefreshAt != nil {
		log.Infow("starting sector metadata refresh", "last_refresh_at", lastRefresh[0].LastRefreshAt, "last_refresh_epoch", lastRefresh[0].LastRefreshEpoch)
	}

	head, err := s.api.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("getting chain head: %w", err)
	}

	var sectors []struct {
		SpID      uint64 `db:"sp_id"`
		SectorNum uint64 `db:"sector_num"`

		Expiration *uint64 `db:"expiration_epoch"`

		Partition *uint64 `db:"partition"`
		Deadline  *uint64 `db:"deadline"`
	}

	if err := s.db.Select(ctx, &sectors, "select sp_id, sector_num, expiration_epoch, partition, deadline from sectors_meta ORDER BY sp_id, sector_num"); err != nil {
		return false, xerrors.Errorf("get sector list: %w", err)
	}

	astor := adt.WrapStore(ctx, cbor.NewCborStore(s.bstore))
	minerStates := map[abi.ActorID]miner.State{}

	type partitionUpdate struct {
		SpID      uint64
		SectorNum uint64
		Partition uint64
		Deadline  uint64
	}

	const batchSize = 1000
	updateBatch := make([]partitionUpdate, 0, batchSize)
	total := 0

	flushBatch := func() error {
		if len(updateBatch) == 0 {
			return nil
		}

		total += len(updateBatch)
		log.Infow("updating sector partitions", "total", total)

		_, err := s.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			batch := &pgx.Batch{}
			for _, update := range updateBatch {
				batch.Queue("UPDATE sectors_meta SET partition = $1, deadline = $2 WHERE sp_id = $3 AND sector_num = $4",
					update.Partition, update.Deadline, update.SpID, update.SectorNum)
			}

			br := tx.SendBatch(ctx, batch)
			defer func() {
				_ = br.Close()
			}()

			for i := 0; i < batch.Len(); i++ {
				_, err := br.Exec()
				if err != nil {
					return false, xerrors.Errorf("executing batch update %d: %w", i, err)
				}
			}

			return true, nil
		}, harmonydb.OptionRetry())
		return err
	}

	for _, sector := range sectors {
		maddr, err := address.NewIDAddress(sector.SpID)
		if err != nil {
			return false, xerrors.Errorf("creating miner address: %w", err)
		}

		mstate, ok := minerStates[abi.ActorID(sector.SpID)]
		if !ok {
			act, err := s.api.StateGetActor(ctx, maddr, head.Key())
			if err != nil {
				return false, xerrors.Errorf("getting miner actor: %w", err)
			}

			mstate, err = miner.Load(astor, act)
			if err != nil {
				return false, xerrors.Errorf("loading miner state: %w", err)
			}

			minerStates[abi.ActorID(sector.SpID)] = mstate
		}

		si, err := mstate.GetSector(abi.SectorNumber(sector.SectorNum))
		if err != nil {
			return false, xerrors.Errorf("getting sector info: %w", err)
		}
		if si == nil {
			continue
		}

		if sector.Expiration == nil || si.Expiration != abi.ChainEpoch(*sector.Expiration) {
			_, err := s.db.Exec(ctx, "update sectors_meta set expiration_epoch = $1 where sp_id = $2 and sector_num = $3", si.Expiration, sector.SpID, sector.SectorNum)
			if err != nil {
				return false, xerrors.Errorf("updating sector expiration: %w", err)
			}
		}

		if sector.Partition == nil || sector.Deadline == nil {
			loc, err := s.api.StateSectorPartition(ctx, maddr, abi.SectorNumber(sector.SectorNum), head.Key())
			if err != nil {
				return false, xerrors.Errorf("getting sector partition: %w", err)
			}

			if loc != nil {
				updateBatch = append(updateBatch, partitionUpdate{
					SpID:      sector.SpID,
					SectorNum: sector.SectorNum,
					Partition: loc.Partition,
					Deadline:  loc.Deadline,
				})

				if len(updateBatch) >= batchSize {
					if err := flushBatch(); err != nil {
						return false, xerrors.Errorf("flushing batch: %w", err)
					}
					updateBatch = updateBatch[:0]
				}
			}
		}
	}

	// Flush any remaining updates
	if err := flushBatch(); err != nil {
		return false, xerrors.Errorf("flushing final batch: %w", err)
	}

	// Update verifreg claims
	log.Info("starting verifreg claim crawl")
	if err := s.updateVerifregClaims(ctx, astor, head.Key()); err != nil {
		log.Errorw("failed to update verifreg claims", "error", err)
		// Don't fail the entire task, just log the error
	}

	// Update the sectors_meta_updates table with current refresh info
	tskBytes := head.Key().Bytes()
	_, err = s.db.Exec(ctx, `
		INSERT INTO sectors_meta_updates (singleton, last_refresh_at, last_refresh_epoch, last_refresh_tsk)
		VALUES (FALSE, CURRENT_TIMESTAMP, $1, $2)
		ON CONFLICT (singleton) DO UPDATE 
		SET last_refresh_at = CURRENT_TIMESTAMP,
		    last_refresh_epoch = $1,
		    last_refresh_tsk = $2
	`, int64(head.Height()), tskBytes)
	if err != nil {
		return false, xerrors.Errorf("updating sectors_meta_updates: %w", err)
	}

	log.Infow("completed sector metadata refresh", "epoch", head.Height(), "total_updates", total)

	return true, nil
}

func (s *SectorMetadata) updateVerifregClaims(ctx context.Context, astor adt.Store, tsk types.TipSetKey) error {
	// Get verifreg actor
	verifregAct, err := s.api.StateGetActor(ctx, builtin.VerifiedRegistryActorAddr, tsk)
	if err != nil {
		return xerrors.Errorf("getting verifreg actor: %w", err)
	}

	verifregSt, err := verifreg.Load(astor, verifregAct)
	if err != nil {
		return xerrors.Errorf("loading verifreg state: %w", err)
	}

	// Get all unique SP IDs from sectors_meta
	var spIDs []struct {
		SpID uint64 `db:"sp_id"`
	}
	if err := s.db.Select(ctx, &spIDs, `SELECT DISTINCT sp_id FROM sectors_meta ORDER BY sp_id`); err != nil {
		return xerrors.Errorf("getting sp ids: %w", err)
	}

	log.Infow("crawling verifreg claims", "sp_count", len(spIDs))

	type claimUpdate struct {
		SpID         uint64
		SectorNum    uint64
		MinClaimTerm *abi.ChainEpoch
		MaxClaimTerm *abi.ChainEpoch
	}

	const batchSize = 1000
	updateBatch := make([]claimUpdate, 0, batchSize)
	totalUpdates := 0

	flushClaimBatch := func() error {
		if len(updateBatch) == 0 {
			return nil
		}

		totalUpdates += len(updateBatch)
		log.Infow("updating sector claims", "total", totalUpdates)

		_, err := s.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			batch := &pgx.Batch{}
			for _, update := range updateBatch {
				if update.MinClaimTerm != nil && update.MaxClaimTerm != nil {
					batch.Queue("UPDATE sectors_meta SET min_claim_epoch = $1, max_claim_epoch = $2 WHERE sp_id = $3 AND sector_num = $4",
						int64(*update.MinClaimTerm), int64(*update.MaxClaimTerm), update.SpID, update.SectorNum)
				} else {
					// Clear claims if sector no longer has any
					batch.Queue("UPDATE sectors_meta SET min_claim_epoch = NULL, max_claim_epoch = NULL WHERE sp_id = $1 AND sector_num = $2",
						update.SpID, update.SectorNum)
				}
			}

			br := tx.SendBatch(ctx, batch)
			defer func() {
				_ = br.Close()
			}()

			for i := 0; i < batch.Len(); i++ {
				_, err := br.Exec()
				if err != nil {
					return false, xerrors.Errorf("executing batch update %d: %w", i, err)
				}
			}

			return true, nil
		}, harmonydb.OptionRetry())
		return err
	}

	// Process each SP
	for _, sp := range spIDs {
		maddr, err := address.NewIDAddress(sp.SpID)
		if err != nil {
			log.Warnw("invalid sp id", "sp_id", sp.SpID, "error", err)
			continue
		}

		// Get claim IDs by sector for this miner
		claimIdsBySector, err := verifregSt.GetClaimIdsBySector(maddr)
		if err != nil {
			log.Warnw("failed to get claim IDs by sector", "sp_id", sp.SpID, "error", err)
			continue
		}

		var claimsMap map[verifreg.ClaimId]verifreg.Claim
		if len(claimIdsBySector) > 0 {
			// Get all claims for this miner
			claimsMap, err = verifregSt.GetClaims(maddr)
			if err != nil {
				log.Warnw("failed to get claims", "sp_id", sp.SpID, "error", err)
				continue
			}
		}

		// Get all sectors for this SP
		var sectors []struct {
			SectorNum uint64  `db:"sector_num"`
			MinClaim  *uint64 `db:"min_claim_epoch"`
			MaxClaim  *uint64 `db:"max_claim_epoch"`
		}
		if err := s.db.Select(ctx, &sectors, `SELECT sector_num, min_claim_epoch, max_claim_epoch FROM sectors_meta WHERE sp_id = $1`, sp.SpID); err != nil {
			log.Warnw("failed to get sectors", "sp_id", sp.SpID, "error", err)
			continue
		}

		// Build a map of existing claim data
		existingClaims := make(map[uint64]struct {
			min *uint64
			max *uint64
		})
		for _, sector := range sectors {
			existingClaims[sector.SectorNum] = struct {
				min *uint64
				max *uint64
			}{min: sector.MinClaim, max: sector.MaxClaim}
		}

		// Calculate claim terms for each sector
		for sectorNum, claimIds := range claimIdsBySector {
			if len(claimIds) == 0 {
				// If sector previously had claims but now doesn't, clear them
				existing := existingClaims[uint64(sectorNum)]
				if existing.min != nil || existing.max != nil {
					updateBatch = append(updateBatch, claimUpdate{
						SpID:         sp.SpID,
						SectorNum:    uint64(sectorNum),
						MinClaimTerm: nil,
						MaxClaimTerm: nil,
					})
				}
				continue
			}

			var minTermEnd *abi.ChainEpoch
			var maxTermEnd *abi.ChainEpoch

			for _, claimId := range claimIds {
				claim, ok := claimsMap[claimId]
				if !ok {
					log.Warnw("claim not found in map", "sp_id", sp.SpID, "sector", sectorNum, "claim_id", claimId)
					continue
				}

				termEnd := claim.TermStart + claim.TermMax
				if minTermEnd == nil || termEnd < *minTermEnd {
					minTermEnd = &termEnd
				}
				if maxTermEnd == nil || termEnd > *maxTermEnd {
					maxTermEnd = &termEnd
				}
			}

			if minTermEnd == nil || maxTermEnd == nil {
				continue
			}

			// Check if we need to update
			existing := existingClaims[uint64(sectorNum)]
			needsUpdate := false
			if existing.min == nil || existing.max == nil {
				needsUpdate = true
			} else if uint64(*minTermEnd) != *existing.min || uint64(*maxTermEnd) != *existing.max {
				needsUpdate = true
			}

			if needsUpdate {
				updateBatch = append(updateBatch, claimUpdate{
					SpID:         sp.SpID,
					SectorNum:    uint64(sectorNum),
					MinClaimTerm: minTermEnd,
					MaxClaimTerm: maxTermEnd,
				})

				if len(updateBatch) >= batchSize {
					if err := flushClaimBatch(); err != nil {
						return xerrors.Errorf("flushing claim batch: %w", err)
					}
					updateBatch = updateBatch[:0]
				}
			}
		}

		// Check for sectors that had claims but no longer do
		for sectorNum, existing := range existingClaims {
			if existing.min == nil && existing.max == nil {
				continue // Already has no claims
			}

			if _, hasClaimsNow := claimIdsBySector[abi.SectorNumber(sectorNum)]; !hasClaimsNow {
				updateBatch = append(updateBatch, claimUpdate{
					SpID:         sp.SpID,
					SectorNum:    sectorNum,
					MinClaimTerm: nil,
					MaxClaimTerm: nil,
				})

				if len(updateBatch) >= batchSize {
					if err := flushClaimBatch(); err != nil {
						return xerrors.Errorf("flushing claim batch: %w", err)
					}
					updateBatch = updateBatch[:0]
				}
			}
		}
	}

	// Flush any remaining claim updates
	if err := flushClaimBatch(); err != nil {
		return xerrors.Errorf("flushing final claim batch: %w", err)
	}

	log.Infow("completed verifreg claim crawl", "total_updates", totalUpdates)
	return nil
}

func (s *SectorMetadata) CanAccept(ids []harmonytask.TaskID, engine *harmonytask.TaskEngine) (*harmonytask.TaskID, error) {
	id := ids[0]
	return &id, nil
}

func (s *SectorMetadata) TypeDetails() harmonytask.TaskTypeDetails {
	return harmonytask.TaskTypeDetails{
		Max:  taskhelp.Max(1),
		Name: "SectorMetadata",
		Cost: resources.Resources{
			Cpu: 1,
			Ram: 64 << 20,
			Gpu: 0,
		},
		IAmBored: harmonytask.SingletonTaskAdder(SectorMetadataRefreshInterval, s),
	}
}

func (s *SectorMetadata) Adder(taskFunc harmonytask.AddTaskFunc) {
}

var _ = harmonytask.Reg(&SectorMetadata{})
var _ harmonytask.TaskInterface = &SectorMetadata{}
