package webrpc

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/samber/lo"
	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/chain/types"
)

type SectorInfo struct {
	SectorNumber  int64
	SpID          uint64
	PipelinePoRep *sectorListEntry

	Pieces    []SectorPieceMeta
	Locations []LocationTable
	Tasks     []SectorInfoTaskSummary

	TaskHistory []TaskHistory

	Resumable bool
}

type SectorInfoTaskSummary struct {
	Name           string
	SincePosted    string
	Owner, OwnerID *string
	ID             int64
}

type TaskHistory struct {
	PipelineTaskID int64      `db:"pipeline_task_id"`
	Name           *string    `db:"name"`
	CompletedBy    *string    `db:"completed_by_host_and_port"`
	Result         *bool      `db:"result"`
	Err            *string    `db:"err"`
	WorkStart      *time.Time `db:"work_start"`
	WorkEnd        *time.Time `db:"work_end"`

	// display
	Took string `db:"-"`
}

// Pieces
type SectorPieceMeta struct {
	PieceIndex int64  `db:"piece_index"`
	PieceCid   string `db:"piece_cid"`
	PieceSize  int64  `db:"piece_size"`

	DataUrl          string `db:"data_url"`
	DataRawSize      int64  `db:"data_raw_size"`
	DeleteOnFinalize bool   `db:"data_delete_on_finalize"`

	F05PublishCid *string `db:"f05_publish_cid"`
	F05DealID     *int64  `db:"f05_deal_id"`

	DDOPam *string `db:"direct_piece_activation_manifest"`

	// display
	StrPieceSize   string `db:"-"`
	StrDataRawSize string `db:"-"`

	// piece park
	IsParkedPiece          bool      `db:"-"`
	IsParkedPieceFound     bool      `db:"-"`
	PieceParkID            int64     `db:"-"`
	PieceParkDataUrl       string    `db:"-"`
	PieceParkCreatedAt     time.Time `db:"-"`
	PieceParkComplete      bool      `db:"-"`
	PieceParkTaskID        *int64    `db:"-"`
	PieceParkCleanupTaskID *int64    `db:"-"`
}

type FileLocations struct {
	StorageID string
	Urls      []string
}

type LocationTable struct {
	PathType        *string
	PathTypeRowSpan int

	FileType        *string
	FileTypeRowSpan int

	Locations []FileLocations
}

func (a *WebRPC) SectorInfo(ctx context.Context, sp string, intid int64) (*SectorInfo, error) {

	maddr, err := address.NewFromString(sp)
	if err != nil {
		return nil, xerrors.Errorf("invalid sp")
	}

	spid, err := address.IDFromAddress(maddr)
	if err != nil {
		return nil, xerrors.Errorf("invalid sp")
	}

	var tasks []PipelineTask

	err = a.deps.DB.Select(ctx, &tasks, `SELECT 
       sp_id, sector_number,
       create_time,
       task_id_sdr, after_sdr,
       task_id_tree_d, after_tree_d,
       task_id_tree_c, after_tree_c,
       task_id_tree_r, after_tree_r,
       task_id_precommit_msg, after_precommit_msg,
       after_precommit_msg_success, seed_epoch,
       task_id_porep, porep_proof, after_porep,
       task_id_finalize, after_finalize,
       task_id_move_storage, after_move_storage,
       task_id_commit_msg, after_commit_msg,
       after_commit_msg_success,
       failed, failed_reason
    FROM sectors_sdr_pipeline WHERE sp_id = $1 AND sector_number = $2`, spid, intid)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch pipeline task info: %w", err)
	}

	// if len(tasks) == 0 {  They could be onboarded from elsewhere.
	// 	return nil, xerrors.Errorf("sector not found")
	// }

	head, err := a.deps.Chain.ChainHead(ctx)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch chain head: %w", err)
	}
	epoch := head.Height()

	mbf, err := a.getMinerBitfields(ctx, maddr, a.stor)
	if err != nil {
		return nil, xerrors.Errorf("failed to load miner bitfields: %w", err)
	}

	var sle *sectorListEntry
	if len(tasks) > 0 {
		task := tasks[0]
		sle = &sectorListEntry{
			PipelineTask: tasks[0],
			AfterSeed:    task.SeedEpoch != nil && *task.SeedEpoch <= int64(epoch),

			ChainAlloc:    must.One(mbf.alloc.IsSet(uint64(task.SectorNumber))),
			ChainSector:   must.One(mbf.sectorSet.IsSet(uint64(task.SectorNumber))),
			ChainActive:   must.One(mbf.active.IsSet(uint64(task.SectorNumber))),
			ChainUnproven: must.One(mbf.unproven.IsSet(uint64(task.SectorNumber))),
			ChainFaulty:   must.One(mbf.faulty.IsSet(uint64(task.SectorNumber))),
		}
	}
	var sectorLocations []struct {
		CanSeal, CanStore bool
		FileType          storiface.SectorFileType `db:"sector_filetype"`
		StorageID         string                   `db:"storage_id"`
		Urls              string                   `db:"urls"`
	}

	err = a.deps.DB.Select(ctx, &sectorLocations, `SELECT p.can_seal, p.can_store, l.sector_filetype, l.storage_id, p.urls FROM sector_location l
    JOIN storage_path p ON l.storage_id = p.storage_id
    WHERE l.sector_num = $1 and l.miner_id = $2 ORDER BY p.can_seal, p.can_store, l.storage_id`, intid, spid)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch sector locations: %w", err)
	}

	locs := []LocationTable{}

	for i, loc := range sectorLocations {
		loc := loc

		urlList := strings.Split(loc.Urls, paths.URLSeparator)

		fLoc := FileLocations{
			StorageID: loc.StorageID,
			Urls:      urlList,
		}

		var pathTypeStr *string
		var fileTypeStr *string
		pathTypeRowSpan := 1
		fileTypeRowSpan := 1

		pathType := "None"
		if loc.CanSeal && loc.CanStore {
			pathType = "Seal/Store"
		} else if loc.CanSeal {
			pathType = "Seal"
		} else if loc.CanStore {
			pathType = "Store"
		}
		pathTypeStr = &pathType

		fileType := loc.FileType.String()
		fileTypeStr = &fileType

		if i > 0 {
			prevNonNilPathTypeLoc := i - 1
			for prevNonNilPathTypeLoc > 0 && locs[prevNonNilPathTypeLoc].PathType == nil {
				prevNonNilPathTypeLoc--
			}
			if *locs[prevNonNilPathTypeLoc].PathType == *pathTypeStr {
				pathTypeRowSpan = 0
				pathTypeStr = nil
				locs[prevNonNilPathTypeLoc].PathTypeRowSpan++
				// only if we have extended path type we may need to extend file type

				prevNonNilFileTypeLoc := i - 1
				for prevNonNilFileTypeLoc > 0 && locs[prevNonNilFileTypeLoc].FileType == nil {
					prevNonNilFileTypeLoc--
				}
				if *locs[prevNonNilFileTypeLoc].FileType == *fileTypeStr {
					fileTypeRowSpan = 0
					fileTypeStr = nil
					locs[prevNonNilFileTypeLoc].FileTypeRowSpan++
				}
			}
		}

		locTable := LocationTable{
			PathType:        pathTypeStr,
			PathTypeRowSpan: pathTypeRowSpan,
			FileType:        fileTypeStr,
			FileTypeRowSpan: fileTypeRowSpan,
			Locations:       []FileLocations{fLoc},
		}

		locs = append(locs, locTable)

	}

	var pieces []SectorPieceMeta

	err = a.deps.DB.Select(ctx, &pieces, `SELECT piece_index, piece_cid, piece_size,
       data_url, data_raw_size, data_delete_on_finalize,
       f05_publish_cid, f05_deal_id, direct_piece_activation_manifest FROM sectors_sdr_initial_pieces WHERE sp_id = $1 AND sector_number = $2`, spid, intid)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch sector pieces: %w", err)
	}

	for i := range pieces {
		pieces[i].StrPieceSize = types.SizeStr(types.NewInt(uint64(pieces[i].PieceSize)))
		pieces[i].StrDataRawSize = types.SizeStr(types.NewInt(uint64(pieces[i].DataRawSize)))

		id, isPiecePark := strings.CutPrefix(pieces[i].DataUrl, "pieceref:")
		if !isPiecePark {
			continue
		}

		intID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			log.Errorw("failed to parse piece park id", "id", id, "error", err)
			continue
		}

		var parkedPiece []struct {
			// parked_piece_refs
			PieceID int64  `db:"piece_id"`
			DataUrl string `db:"data_url"`

			// parked_pieces
			CreatedAt     time.Time `db:"created_at"`
			Complete      bool      `db:"complete"`
			ParkTaskID    *int64    `db:"task_id"`
			CleanupTaskID *int64    `db:"cleanup_task_id"`
		}

		err = a.deps.DB.Select(ctx, &parkedPiece, `SELECT ppr.piece_id, ppr.data_url, pp.created_at, pp.complete, pp.task_id, pp.cleanup_task_id FROM parked_piece_refs ppr
		INNER JOIN parked_pieces pp ON pp.id = ppr.piece_id
		WHERE ppr.ref_id = $1`, intID)
		if err != nil {
			return nil, xerrors.Errorf("failed to fetch parked piece: %w", err)
		}

		if len(parkedPiece) == 0 {
			pieces[i].IsParkedPieceFound = false
			continue
		}

		pieces[i].IsParkedPieceFound = true
		pieces[i].IsParkedPiece = true

		pieces[i].PieceParkID = parkedPiece[0].PieceID
		pieces[i].PieceParkDataUrl = parkedPiece[0].DataUrl
		pieces[i].PieceParkCreatedAt = parkedPiece[0].CreatedAt.Local()
		pieces[i].PieceParkComplete = parkedPiece[0].Complete
		pieces[i].PieceParkTaskID = parkedPiece[0].ParkTaskID
		pieces[i].PieceParkCleanupTaskID = parkedPiece[0].CleanupTaskID
	}

	// TaskIDs
	var htasks []SectorInfoTaskSummary
	if len(tasks) > 0 {
		taskIDs := map[int64]struct{}{}
		task := tasks[0]
		// get non-nil task IDs
		appendNonNil := func(id *int64) {
			if id != nil {
				taskIDs[*id] = struct{}{}
			}
		}
		appendNonNil(task.TaskSDR)
		appendNonNil(task.TaskTreeD)
		appendNonNil(task.TaskTreeC)
		appendNonNil(task.TaskTreeR)
		appendNonNil(task.TaskPrecommitMsg)
		appendNonNil(task.TaskPoRep)
		appendNonNil(task.TaskFinalize)
		appendNonNil(task.TaskMoveStorage)
		appendNonNil(task.TaskCommitMsg)

		if len(taskIDs) > 0 {
			ids := lo.Keys(taskIDs)

			var dbtasks []struct {
				OwnerID     *string   `db:"owner_id"`
				HostAndPort *string   `db:"host_and_port"`
				TaskID      int64     `db:"id"`
				Name        string    `db:"name"`
				UpdateTime  time.Time `db:"update_time"`
			}
			err = a.deps.DB.Select(ctx, &dbtasks, `SELECT t.owner_id, hm.host_and_port, t.id, t.name, t.update_time FROM harmony_task t LEFT JOIN curio.harmony_machines hm ON hm.id = t.owner_id WHERE t.id = ANY($1)`, ids)
			if err != nil {
				return nil, xerrors.Errorf("failed to fetch task names: %v", err)
			}

			for _, tn := range dbtasks {
				htasks = append(htasks, SectorInfoTaskSummary{
					Name:        tn.Name,
					SincePosted: time.Since(tn.UpdateTime.Local()).Round(time.Second).String(),
					Owner:       tn.HostAndPort,
					OwnerID:     tn.OwnerID,
					ID:          tn.TaskID,
				})
			}
		}
	}

	// Task history
	/*
		WITH task_ids AS (
		    SELECT unnest(get_sdr_pipeline_tasks(116147, 1)) AS task_id
		)
		SELECT ti.task_id pipeline_task_id, ht.id harmony_task_id, ht.name, ht.completed_by_host_and_port, ht.result, ht.err, ht.work_start, ht.work_end
		FROM task_ids ti
		         INNER JOIN harmony_task_history ht ON ti.task_id = ht.task_id
	*/

	var th []TaskHistory
	err = a.deps.DB.Select(ctx, &th, `
		WITH task_ids AS (
				SELECT unnest(get_sdr_pipeline_tasks($1, $2)) AS task_id
		)
		SELECT ti.task_id pipeline_task_id, ht.name, ht.completed_by_host_and_port, ht.result, ht.err, ht.work_start, ht.work_end
		FROM task_ids ti
						INNER JOIN harmony_task_history ht ON ti.task_id = ht.task_id`, spid, intid)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch task history: %w", err)
	}

	for i := range th {
		if th[i].WorkStart != nil && th[i].WorkEnd != nil {
			th[i].Took = th[i].WorkEnd.Sub(*th[i].WorkStart).Round(time.Second).String()
		}
	}

	var taskState []struct {
		PipelineID    int64  `db:"pipeline_id"`
		HarmonyTaskID *int64 `db:"harmony_task_id"`
	}
	err = a.deps.DB.Select(ctx, &taskState, `WITH task_ids AS (
	    SELECT unnest(get_sdr_pipeline_tasks($1, $2)) AS task_id
	)
	SELECT ti.task_id pipeline_id, ht.id harmony_task_id
	FROM task_ids ti
	         LEFT JOIN harmony_task ht ON ti.task_id = ht.id`, spid, intid)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch task history: %w", err)
	}

	var hasAnyStuckTask bool
	for _, ts := range taskState {
		if ts.HarmonyTaskID == nil {
			hasAnyStuckTask = true
			break
		}
	}

	return &SectorInfo{
		SectorNumber:  intid,
		SpID:          spid,
		PipelinePoRep: sle,

		Pieces:      pieces,
		Locations:   locs,
		Tasks:       htasks,
		TaskHistory: th,

		Resumable: hasAnyStuckTask,
	}, nil
}

func (a *WebRPC) SectorResume(ctx context.Context, spid, id int64) error {
	_, err := a.deps.DB.Exec(ctx, `SELECT unset_task_id($1, $2)`, spid, id)
	if err != nil {
		return xerrors.Errorf("failed to resume sector: %w", err)
	}
	return nil
}

func (a *WebRPC) SectorRemove(ctx context.Context, spid, id int64) error {
	_, err := a.deps.DB.Exec(ctx, `DELETE FROM batch_sector_refs WHERE sp_id = $1 AND sector_number = $2`, spid, id)
	if err != nil {
		return xerrors.Errorf("failed to remove sector batch refs: %w", err)
	}

	_, err = a.deps.DB.Exec(ctx, `DELETE FROM sectors_sdr_pipeline WHERE sp_id = $1 AND sector_number = $2`, spid, id)
	if err != nil {
		return xerrors.Errorf("failed to remove sector: %w", err)
	}

	_, err = a.deps.DB.Exec(ctx, `INSERT INTO storage_removal_marks (sp_id, sector_num, sector_filetype, storage_id, created_at, approved, approved_at)
		SELECT miner_id, sector_num, sector_filetype, storage_id, current_timestamp, FALSE, NULL FROM sector_location
		WHERE miner_id = $1 AND sector_num = $2`, spid, id)
	if err != nil {
		return xerrors.Errorf("failed to mark sector for removal: %w", err)
	}

	return nil
}
