package webrpc

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/ipfs/go-cid"
	"github.com/samber/lo"
	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	commcid "github.com/filecoin-project/go-fil-commcid"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/chain/types"
)

const verifiedPowerGainMul = 9

type SectorInfo struct {
	SectorNumber       int64
	SpID               uint64
	Miner              string
	PreCommitMsg       string
	CommitMsg          string
	ActivationEpoch    abi.ChainEpoch
	ExpirationEpoch    *int64
	DealWeight         string
	Deadline           *int64
	Partition          *int64
	UnsealedCid        string
	SealedCid          string
	UpdatedUnsealedCid string
	UpdatedSealedCid   string
	IsSnap             bool
	UpdateMsg          string
	UnsealedState      *bool
	HasUnsealed        bool

	PipelinePoRep *sectorListEntry
	PipelineSnap  *sectorSnapListEntry

	Pieces      []SectorPieceMeta
	Locations   []LocationTable
	Tasks       []SectorInfoTaskSummary
	TaskHistory []TaskHistory

	Resumable bool
	Restart   bool
}

type sectorSnapListEntry struct {
	SnapPipelineTask
}

type SnapPipelineTask struct {
	// Cache line 1 (bytes 0-64): Hot path - identification and early checks
	SpID         int64     `db:"sp_id"`         // 8 bytes (0-8)
	SectorNumber int64     `db:"sector_number"` // 8 bytes (8-16)
	StartTime    time.Time `db:"start_time"`    // 24 bytes (16-40)
	UpgradeProof int       `db:"upgrade_proof"` // 8 bytes (40-48)
	Failed       bool      `db:"failed"`        // 1 byte (48-49) - checked early
	DataAssigned bool      `db:"data_assigned"` // 1 byte (49-50) - checked with sector number
	// Cache line 2 (bytes 64-128): Encode and Prove stages (accessed together)
	TaskEncode        sql.NullInt64  `db:"task_id_encode"`      // 16 bytes
	AfterEncode       bool           `db:"after_encode"`        // 1 byte
	UpdateUnsealedCID sql.NullString `db:"update_unsealed_cid"` // 24 bytes
	TaskProve         sql.NullInt64  `db:"task_id_prove"`       // 16 bytes
	AfterProve        bool           `db:"after_prove"`         // 1 byte
	// Cache line 3 (bytes 128-192): Submit and message stages
	UpdateSealedCID      sql.NullString `db:"update_sealed_cid"`       // 24 bytes
	TaskSubmit           sql.NullInt64  `db:"task_id_submit"`          // 16 bytes
	AfterSubmit          bool           `db:"after_submit"`            // 1 byte
	AfterProveMsgSuccess bool           `db:"after_prove_msg_success"` // 1 byte
	UpdateMsgCid         sql.NullString `db:"prove_msg_cid"`           // 24 bytes (crosses into cache line 4)
	// Cache line 4 (bytes 192-256): Storage and timing
	TaskMoveStorage  sql.NullInt64 `db:"task_id_move_storage"` // 16 bytes
	AfterMoveStorage bool          `db:"after_move_storage"`   // 1 byte
	SubmitAfter      sql.NullTime  `db:"submit_after"`         // 32 bytes
	// Failure info (only accessed when Failed=true)
	FailedAt sql.NullTime `db:"failed_at"` // 32 bytes (crosses into cache line 5)
	// Rarely accessed fields at end
	FailedReason    string `db:"failed_reason"`     // 16 bytes - only when Failed=true
	FailedReasonMsg string `db:"failed_reason_msg"` // 16 bytes - only when Failed=true
	ProveMsgTsk     []byte `db:"prove_msg_tsk"`     // 24 bytes - only in specific stages
}
type SectorInfoTaskSummary struct {
	Name           string
	SincePosted    string
	Owner, OwnerID *string
	ID             int64
}

type TaskHistory struct {
	// Cache line 1 (bytes 0-64): Identification and key timing
	PipelineTaskID int64        `db:"pipeline_task_id"` // 8 bytes (0-8)
	WorkStart      sql.NullTime `db:"work_start"`       // 32 bytes (8-40)
	WorkEnd        sql.NullTime `db:"work_end"`         // 32 bytes (40-72, crosses to cache line 2)
	// Cache line 2 (bytes 64-128): Task details
	Name        sql.NullString `db:"name"`                       // 24 bytes
	CompletedBy sql.NullString `db:"completed_by_host_and_port"` // 24 bytes
	Result      sql.NullBool   `db:"result"`                     // 2 bytes
	// Cache line 3 (bytes 128+): Error info and display fields (only accessed when needed)
	Err  sql.NullString `db:"err"` // 24 bytes - only accessed when Result is false
	Took string         `db:"-"`   // 16 bytes - display only, computed field
}

// Pieces
type SectorPieceMeta struct {
	// Cache line 1 (bytes 0-64): Hot path - piece identification and size
	PieceIndex  int64         `db:"piece_index"`   // 8 bytes (0-8)
	PieceSize   int64         `db:"piece_size"`    // 8 bytes (8-16)
	PieceCid    string        `db:"piece_cid"`     // 16 bytes (16-32)
	PieceCidV2  string        `db:"-"`             // 16 bytes (32-48) - computed field
	DataRawSize sql.NullInt64 `db:"data_raw_size"` // 16 bytes (48-64)
	// Cache line 2 (bytes 64-128): Deal identification
	F05DealID   sql.NullInt64  `db:"f05_deal_id"` // 16 bytes
	DealID      sql.NullString `db:"deal_id"`     // 24 bytes
	IsSnapPiece bool           `db:"is_snap"`     // 1 byte - frequently checked with PieceIndex
	// Cache line 3 (bytes 128-192): Data access and F05 info
	DataUrl       sql.NullString `db:"data_url"`        // 24 bytes
	F05PublishCid sql.NullString `db:"f05_publish_cid"` // 24 bytes
	// Cache line 4 (bytes 192-256): DDO and display fields
	DDOPam         sql.NullString `db:"direct_piece_activation_manifest"` // 24 bytes
	StrPieceSize   string         `db:"-"`                                // 16 bytes - display only
	StrDataRawSize string         `db:"-"`                                // 16 bytes - display only
	// Piece park fields (rarely accessed, only for parked pieces)
	PieceParkDataUrl       string    `db:"-"` // 16 bytes
	PieceParkCreatedAt     time.Time `db:"-"` // 24 bytes
	PieceParkID            int64     `db:"-"` // 8 bytes
	PieceParkTaskID        *int64    `db:"-"` // 8 bytes - still pointer (not from DB)
	PieceParkCleanupTaskID *int64    `db:"-"` // 8 bytes - still pointer (not from DB)
	// Bools: frequently checked first, rare ones at end (sql.NullBool = 2 bytes each)
	MK12Deal           sql.NullBool `db:"boost_deal"`              // 2 bytes - checked often
	LegacyDeal         sql.NullBool `db:"legacy_deal"`             // 2 bytes - checked often
	DeleteOnFinalize   sql.NullBool `db:"data_delete_on_finalize"` // 2 bytes - checked during finalize
	IsParkedPiece      bool         `db:"-"`                       // rare - only for UI display
	IsParkedPieceFound bool         `db:"-"`                       // rare - only for UI display
	PieceParkComplete  bool         `db:"-"`                       // rare - only for parked pieces
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

type SectorMeta struct {
	// Cache line 1 (bytes 0-64): Original and updated CIDs (accessed together for sector comparison)
	OrigUnsealedCid    string `db:"orig_unsealed_cid"` // 16 bytes (0-16)
	OrigSealedCid      string `db:"orig_sealed_cid"`   // 16 bytes (16-32)
	UpdatedUnsealedCid string `db:"cur_unsealed_cid"`  // 16 bytes (32-48)
	UpdatedSealedCid   string `db:"cur_sealed_cid"`    // 16 bytes (48-64)
	// Cache line 2 (bytes 64-128): Message CIDs (accessed for on-chain tracking)
	PreCommitCid string         `db:"msg_cid_precommit"` // 16 bytes (64-80)
	CommitCid    string         `db:"msg_cid_commit"`    // 16 bytes (80-96)
	UpdateCid    sql.NullString `db:"msg_cid_update"`    // 24 bytes (96-120) - null for non-snap sectors
	// Cache line 3 (bytes 128-192): On-chain metadata (sql.NullInt64 = 16 bytes each)
	ExpirationEpoch sql.NullInt64 `db:"expiration_epoch"` // 16 bytes
	Deadline        sql.NullInt64 `db:"deadline"`         // 16 bytes
	Partition       sql.NullInt64 `db:"partition"`        // 16 bytes
	// Bools (sql.NullBool = 2 bytes each)
	IsCC          sql.NullBool `db:"is_cc"`               // 2 bytes
	UnsealedState sql.NullBool `db:"target_unseal_state"` // 2 bytes
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

	si := &SectorInfo{
		SpID:         spid,
		Miner:        maddr.String(),
		SectorNumber: intid,
	}

	var tasks []PipelineTask

	// Fetch PoRep pipeline data
	err = a.deps.DB.Select(ctx, &tasks, `SELECT 
       sp_id, sector_number,
       create_time,
       task_id_sdr, after_sdr,
       task_id_tree_d, after_tree_d, tree_d_cid,
       task_id_tree_c, after_tree_c,
       task_id_tree_r, after_tree_r, tree_r_cid,
       task_id_synth, after_synth,
       task_id_precommit_msg, after_precommit_msg,
       after_precommit_msg_success, precommit_msg_cid, seed_epoch,
       task_id_porep, porep_proof, after_porep,
       task_id_finalize, after_finalize,
       task_id_move_storage, after_move_storage,
       task_id_commit_msg, after_commit_msg,
       after_commit_msg_success, commit_msg_cid,
       failed, failed_reason
    FROM sectors_sdr_pipeline WHERE sp_id = $1 AND sector_number = $2`, spid, intid)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch pipeline task info: %w", err)
	}

	// Fetch SnapDeals pipeline data
	var snapTasks []SnapPipelineTask

	err = a.deps.DB.Select(ctx, &snapTasks, `SELECT
        sp_id, sector_number,
        start_time,
        upgrade_proof,
        data_assigned,
        update_unsealed_cid,
        update_sealed_cid,
        task_id_encode, after_encode,
        task_id_prove, after_prove,
        task_id_submit, after_submit,
        after_prove_msg_success, prove_msg_tsk,
        task_id_move_storage, after_move_storage, prove_msg_cid,
        failed, failed_at, failed_reason, failed_reason_msg,
        submit_after
    FROM sectors_snap_pipeline WHERE sp_id = $1 AND sector_number = $2`, spid, intid)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch snap pipeline task info: %w", err)
	}

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
		if task.PreCommitMsgCid.Valid {
			si.PreCommitMsg = task.PreCommitMsgCid.String
		} else {
			si.PreCommitMsg = ""
		}

		if task.CommitMsgCid.Valid {
			si.CommitMsg = task.CommitMsgCid.String
		} else {
			si.CommitMsg = ""
		}

		if task.TreeD.Valid {
			si.UnsealedCid = task.TreeD.String
			si.UpdatedUnsealedCid = task.TreeD.String
		} else {
			si.UnsealedCid = ""
			si.UpdatedUnsealedCid = ""
		}

		if task.TreeR.Valid {
			si.SealedCid = task.TreeR.String
			si.UpdatedSealedCid = task.TreeR.String
		} else {
			si.SealedCid = ""
			si.UpdatedSealedCid = ""
		}
		sle = &sectorListEntry{
			PipelineTask: tasks[0],
			AfterSeed:    task.SeedEpoch.Valid && task.SeedEpoch.Int64 <= int64(epoch),

			ChainAlloc:    must.One(mbf.alloc.IsSet(uint64(task.SectorNumber))),
			ChainSector:   must.One(mbf.sectorSet.IsSet(uint64(task.SectorNumber))),
			ChainActive:   must.One(mbf.active.IsSet(uint64(task.SectorNumber))),
			ChainUnproven: must.One(mbf.unproven.IsSet(uint64(task.SectorNumber))),
			ChainFaulty:   must.One(mbf.faulty.IsSet(uint64(task.SectorNumber))),
		}
	}

	// Create SnapDeals pipeline entry
	var sleSnap *sectorSnapListEntry
	if len(snapTasks) > 0 {
		task := snapTasks[0]
		if task.UpdateUnsealedCID.Valid {
			si.UpdatedUnsealedCid = task.UpdateUnsealedCID.String
		} else {
			si.UpdatedUnsealedCid = ""
		}
		if task.UpdateSealedCID.Valid {
			si.UpdatedSealedCid = task.UpdateSealedCID.String
		} else {
			si.UpdatedSealedCid = ""
		}
		if task.UpdateMsgCid.Valid {
			si.UpdateMsg = task.UpdateMsgCid.String
		} else {
			si.UpdateMsg = ""
		}
		si.IsSnap = true
		sleSnap = &sectorSnapListEntry{
			SnapPipelineTask: task,
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
		if loc.FileType == storiface.FTUnsealed {
			si.HasUnsealed = true
		}

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

	var sectorMetas []SectorMeta

	// Fetch SectorMeta from DB
	err = a.deps.DB.Select(ctx, &sectorMetas, `SELECT orig_sealed_cid, 
       orig_unsealed_cid, cur_sealed_cid, cur_unsealed_cid, 
       msg_cid_precommit, msg_cid_commit, msg_cid_update, 
       expiration_epoch, deadline, partition, target_unseal_state, 
       is_cc FROM sectors_meta 
             WHERE sp_id = $1 AND sector_num = $2`, spid, intid)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch sector metadata: %w", err)
	}

	if len(sectorMetas) > 0 {
		sectormeta := sectorMetas[0]
		si.UnsealedCid = sectormeta.OrigUnsealedCid
		si.SealedCid = sectormeta.OrigSealedCid
		si.UpdatedUnsealedCid = sectormeta.UpdatedUnsealedCid
		si.UpdatedSealedCid = sectormeta.UpdatedSealedCid
		si.PreCommitMsg = sectormeta.PreCommitCid
		si.CommitMsg = sectormeta.CommitCid
		if sectormeta.UpdateCid.Valid {
			si.UpdateMsg = sectormeta.UpdateCid.String
		}
		if sectormeta.IsCC.Valid {
			si.IsSnap = !sectormeta.IsCC.Bool
		} else {
			si.IsSnap = false
		}

		if sectormeta.ExpirationEpoch.Valid {
			e := sectormeta.ExpirationEpoch.Int64
			si.ExpirationEpoch = &e
		}
		if sectormeta.Deadline.Valid {
			d := sectormeta.Deadline.Int64
			si.Deadline = &d
		}
		if sectormeta.Partition.Valid {
			p := sectormeta.Partition.Int64
			si.Partition = &p
		}
		if sectormeta.UnsealedState.Valid {
			u := sectormeta.UnsealedState.Bool
			si.UnsealedState = &u
		}
	}

	var pieces []SectorPieceMeta

	err = a.deps.DB.Select(ctx, &pieces, `SELECT piece_index, combined.piece_cid, combined.piece_size,
													   data_url, data_raw_size, data_delete_on_finalize,
													   f05_deal_id, direct_piece_activation_manifest,
													   mpd.id AS deal_id, -- Extracted id from market_piece_deal
													   mpd.boost_deal, -- Retrieved boost_deal from market_piece_deal
       												   mpd.legacy_deal, -- Retrieved legacy_deal from market_piece_deal
													   is_snap -- New column indicating whether the piece is a snap deal
												FROM (
													-- Meta table entries (permanent, prioritized)
													SELECT meta.piece_num AS piece_index, meta.piece_cid, meta.piece_size,
														   NULL AS data_url, meta.raw_data_size AS data_raw_size,
														   NOT meta.requested_keep_data AS data_delete_on_finalize,
														   meta.f05_deal_id, meta.ddo_pam AS direct_piece_activation_manifest,
														   meta.sp_id,
														   NOT sm.is_cc AS is_snap -- is_snap based on is_cc from sectors_meta
													FROM sectors_meta_pieces meta
													JOIN sectors_meta sm ON meta.sp_id = sm.sp_id AND meta.sector_num = sm.sector_num
													WHERE meta.sp_id = $1 AND meta.sector_num = $2
												
													UNION ALL
												
													-- SDR pipeline entries (temporary, non-snap pieces)
													SELECT sdr.piece_index, sdr.piece_cid, sdr.piece_size,
														   sdr.data_url, sdr.data_raw_size, sdr.data_delete_on_finalize,
														   sdr.f05_deal_id, sdr.direct_piece_activation_manifest,
														   sdr.sp_id,
														   FALSE AS is_snap -- SDR pipeline pieces are never snap deals
													FROM sectors_sdr_initial_pieces sdr
													WHERE sdr.sp_id = $1 AND sdr.sector_number = $2
													  AND NOT EXISTS (
														  SELECT 1
														  FROM sectors_meta_pieces meta
														  WHERE meta.sp_id = sdr.sp_id AND meta.piece_cid = sdr.piece_cid
													  )
												
													UNION ALL
												
													-- Snap pipeline entries (temporary, always snap deals)
													SELECT snap.piece_index, snap.piece_cid, snap.piece_size,
														   snap.data_url, snap.data_raw_size, snap.data_delete_on_finalize,
														   NULL AS f05_deal_id, snap.direct_piece_activation_manifest,
														   snap.sp_id,
														   TRUE AS is_snap -- Snap pipeline pieces are always snap deals
													FROM sectors_snap_initial_pieces snap
													WHERE snap.sp_id = $1 AND snap.sector_number = $2
													  AND NOT EXISTS (
														  SELECT 1
														  FROM sectors_meta_pieces meta
														  WHERE meta.sp_id = snap.sp_id AND meta.piece_cid = snap.piece_cid
													  )
												) AS combined
												LEFT JOIN market_piece_deal mpd 
													   ON combined.sp_id = mpd.sp_id AND combined.piece_cid = mpd.piece_cid;
												`, spid, intid)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch sector pieces: %w", err)
	}

	for i := range pieces {
		pieces[i].StrPieceSize = types.SizeStr(types.NewInt(uint64(pieces[i].PieceSize)))
		rawSize := int64(0)
		if pieces[i].DataRawSize.Valid {
			rawSize = pieces[i].DataRawSize.Int64
		}
		pieces[i].StrDataRawSize = types.SizeStr(types.NewInt(uint64(rawSize)))

		pcid, err := cid.Parse(pieces[i].PieceCid)
		if err != nil {
			return nil, xerrors.Errorf("failed to parse piece cid: %w", err)
		}

		if pieces[i].DataRawSize.Valid {
			pcid2, err := commcid.PieceCidV2FromV1(pcid, uint64(pieces[i].DataRawSize.Int64))

			if err != nil {
				return nil, xerrors.Errorf("failed to generate piece cid v2: %w", err)
			}

			pieces[i].PieceCidV2 = pcid2.String()
		}

		dataUrl := ""
		if pieces[i].DataUrl.Valid {
			dataUrl = pieces[i].DataUrl.String
		}
		id, isPiecePark := strings.CutPrefix(dataUrl, "pieceref:")
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
			CreatedAt     time.Time     `db:"created_at"`
			Complete      bool          `db:"complete"`
			ParkTaskID    sql.NullInt64 `db:"task_id"`
			CleanupTaskID sql.NullInt64 `db:"cleanup_task_id"`
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
		if parkedPiece[0].ParkTaskID.Valid {
			t := parkedPiece[0].ParkTaskID.Int64
			pieces[i].PieceParkTaskID = &t
		}
		if parkedPiece[0].CleanupTaskID.Valid {
			c := parkedPiece[0].CleanupTaskID.Int64
			pieces[i].PieceParkCleanupTaskID = &c
		}
	}

	// TaskIDs
	var htasks []SectorInfoTaskSummary
	taskIDs := map[int64]struct{}{}

	appendNullInt64 := func(n sql.NullInt64) {
		if n.Valid {
			taskIDs[n.Int64] = struct{}{}
		}
	}

	// Append PoRep task IDs
	if len(tasks) > 0 {
		task := tasks[0]
		appendNullInt64(task.TaskSDR)
		appendNullInt64(task.TaskTreeD)
		appendNullInt64(task.TaskTreeC)
		appendNullInt64(task.TaskTreeR)
		appendNullInt64(task.TaskPrecommitMsg)
		appendNullInt64(task.TaskPoRep)
		appendNullInt64(task.TaskFinalize)
		appendNullInt64(task.TaskMoveStorage)
		appendNullInt64(task.TaskCommitMsg)
	}

	// Append SnapDeals task IDs
	if len(snapTasks) > 0 {
		task := snapTasks[0]
		appendNullInt64(task.TaskEncode)
		appendNullInt64(task.TaskProve)
		appendNullInt64(task.TaskSubmit)
		appendNullInt64(task.TaskMoveStorage)
	}

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

	// Task history
	var taskIDsList []struct {
		TaskID int64 `db:"task_id"`
	}
	// Fetch PoRep task IDs
	err = a.deps.DB.Select(ctx, &taskIDsList, `SELECT unnest(get_sdr_pipeline_tasks($1, $2)) AS task_id`, spid, intid)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch task history: %w", err)
	}

	// Fetch SnapDeals task IDs
	var snapTaskIDs []struct {
		TaskID int64 `db:"task_id"`
	}
	err = a.deps.DB.Select(ctx, &snapTaskIDs, `SELECT unnest(get_snap_pipeline_tasks($1, $2)) AS task_id`, spid, intid)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch snap task history: %w", err)
	}

	// Combine task IDs
	taskIDsList = append(taskIDsList, snapTaskIDs...)

	var th []TaskHistory
	for _, taskID := range taskIDsList {
		var taskHistories []TaskHistory
		err = a.deps.DB.Select(ctx, &taskHistories, `
            SELECT ht.task_id pipeline_task_id, ht.name, ht.completed_by_host_and_port, ht.result, ht.err, ht.work_start, ht.work_end
            FROM harmony_task_history ht
            WHERE ht.task_id = $1`, taskID.TaskID)
		if err != nil {
			return nil, xerrors.Errorf("failed to fetch task history: %w", err)
		}
		th = append(th, taskHistories...)
	}

	for i := range th {
		if th[i].WorkStart.Valid && th[i].WorkEnd.Valid {
			th[i].Took = th[i].WorkEnd.Time.Sub(th[i].WorkStart.Time).Round(time.Second).String()
		}
	}

	var taskState []struct {
		PipelineID    int64         `db:"pipeline_id"`
		HarmonyTaskID sql.NullInt64 `db:"harmony_task_id"`
	}
	err = a.deps.DB.Select(ctx, &taskState, `WITH task_ids AS (
        SELECT unnest(get_sdr_pipeline_tasks($1, $2)) AS task_id
        UNION
        SELECT unnest(get_snap_pipeline_tasks($1, $2)) AS task_id
    )
    SELECT ti.task_id pipeline_id, ht.id harmony_task_id
    FROM task_ids ti
             LEFT JOIN harmony_task ht ON ti.task_id = ht.id`, spid, intid)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch task state: %w", err)
	}

	var hasAnyStuckTask bool
	for _, ts := range taskState {
		if !ts.HarmonyTaskID.Valid {
			hasAnyStuckTask = true
			break
		}
	}

	onChainInfo, err := a.deps.Chain.StateSectorGetInfo(ctx, maddr, abi.SectorNumber(intid), types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("failed to get on chain info for the sector: %w", err)
	}

	if onChainInfo != nil {
		dw, vp := .0, .0
		dealWeight := "CC"
		{
			rdw := big.Add(onChainInfo.DealWeight, onChainInfo.VerifiedDealWeight)
			dw = float64(big.Div(rdw, big.NewInt(int64(onChainInfo.Expiration-onChainInfo.PowerBaseEpoch))).Uint64())
			vp = float64(big.Div(big.Mul(onChainInfo.VerifiedDealWeight, big.NewInt(verifiedPowerGainMul)), big.NewInt(int64(onChainInfo.Expiration-onChainInfo.PowerBaseEpoch))).Uint64())
			if vp > 0 {
				dw = vp
			}
			if dw > 0 {
				dealWeight = units.BytesSize(dw)
			}
		}

		if si.Deadline == nil || si.Partition == nil {
			part, err := a.deps.Chain.StateSectorPartition(ctx, maddr, abi.SectorNumber(intid), types.EmptyTSK)
			if err != nil {
				return nil, xerrors.Errorf("failed to get partition info for the sector: %w", err)
			}

			d := int64(part.Deadline)
			si.Deadline = &d

			p := int64(part.Partition)
			si.Partition = &p
		}

		si.ActivationEpoch = onChainInfo.Activation
		expr := int64(onChainInfo.Expiration)
		if si.ExpirationEpoch == nil || *si.ExpirationEpoch != expr {
			si.ExpirationEpoch = &expr
		}
		si.DealWeight = dealWeight
	}

	si.PipelinePoRep = sle
	si.PipelineSnap = sleSnap

	si.Pieces = pieces
	si.Locations = locs
	si.Tasks = htasks
	si.TaskHistory = th

	si.Resumable = hasAnyStuckTask
	si.Restart = hasAnyStuckTask && (sle == nil || !sle.AfterSynthetic)

	return si, nil
}

func (a *WebRPC) SectorResume(ctx context.Context, spid, id int64) error {
	// Resume PoRep tasks
	_, err := a.deps.DB.Exec(ctx, `SELECT unset_task_id($1, $2)`, spid, id)
	if err != nil {
		return xerrors.Errorf("failed to resume PoRep sector: %w", err)
	}

	// Resume SnapDeals tasks
	_, err = a.deps.DB.Exec(ctx, `SELECT unset_task_id_snap($1, $2)`, spid, id)
	if err != nil {
		return xerrors.Errorf("failed to resume SnapDeals sector: %w", err)
	}
	return nil
}

func (a *WebRPC) SectorRemove(ctx context.Context, spid, id int64) error {
	// Remove sector from batch_sector_refs
	_, err := a.deps.DB.Exec(ctx, `DELETE FROM batch_sector_refs WHERE sp_id = $1 AND sector_number = $2`, spid, id)
	if err != nil {
		return xerrors.Errorf("failed to remove sector batch refs: %w", err)
	}

	// Remove from sectors_sdr_pipeline
	_, err = a.deps.DB.Exec(ctx, `DELETE FROM sectors_sdr_pipeline WHERE sp_id = $1 AND sector_number = $2`, spid, id)
	if err != nil {
		return xerrors.Errorf("failed to remove PoRep sector: %w", err)
	}

	// Remove from sectors_snap_pipeline
	_, err = a.deps.DB.Exec(ctx, `DELETE FROM sectors_snap_pipeline WHERE sp_id = $1 AND sector_number = $2`, spid, id)
	if err != nil {
		return xerrors.Errorf("failed to remove SnapDeals sector: %w", err)
	}

	// Mark sector for removal
	_, err = a.deps.DB.Exec(ctx, `INSERT INTO storage_removal_marks (sp_id, sector_num, sector_filetype, storage_id, created_at, approved, approved_at)
        SELECT miner_id, sector_num, sector_filetype, storage_id, current_timestamp, FALSE, NULL FROM sector_location
        WHERE miner_id = $1 AND sector_num = $2`, spid, id)
	if err != nil {
		return xerrors.Errorf("failed to mark sector for removal: %w", err)
	}

	return nil
}

func (a *WebRPC) SectorRestart(ctx context.Context, spid, id int64) error {
	// Reset PoRep sector state
	_, err := a.deps.DB.Exec(ctx, `UPDATE sectors_sdr_pipeline SET after_sdr = false, after_tree_d = false, after_tree_c = false,
                                    after_tree_r = false WHERE sp_id = $1 AND sector_number = $2`, spid, id)
	if err != nil {
		return xerrors.Errorf("failed to reset PoRep sector state: %w", err)
	}

	// Reset SnapDeals sector state
	_, err = a.deps.DB.Exec(ctx, `UPDATE sectors_snap_pipeline SET after_encode = false, after_prove = false, after_submit = false,
                                    after_move_storage = false WHERE sp_id = $1 AND sector_number = $2`, spid, id)
	if err != nil {
		return xerrors.Errorf("failed to reset SnapDeals sector state: %w", err)
	}

	// Remove sector files
	err = a.deps.Stor.Remove(ctx, abi.SectorID{Miner: abi.ActorID(spid), Number: abi.SectorNumber(id)}, storiface.FTCache, true, nil)
	if err != nil {
		return xerrors.Errorf("failed to remove cache file: %w", err)
	}
	err = a.deps.Stor.Remove(ctx, abi.SectorID{Miner: abi.ActorID(spid), Number: abi.SectorNumber(id)}, storiface.FTSealed, true, nil)
	if err != nil {
		return xerrors.Errorf("failed to remove sealed file: %w", err)
	}

	// Unset task IDs for both pipelines
	_, err = a.deps.DB.Exec(ctx, `SELECT unset_task_id($1, $2)`, spid, id)
	if err != nil {
		return xerrors.Errorf("failed to resume PoRep sector: %w", err)
	}
	_, err = a.deps.DB.Exec(ctx, `SELECT unset_task_id_snap($1, $2)`, spid, id)
	if err != nil {
		return xerrors.Errorf("failed to resume SnapDeals sector: %w", err)
	}
	return nil
}

type SectorCCScheduler struct {
	SpID         int64
	ToSeal       int64
	Weight       int64
	DurationDays int64
	Enabled      bool

	// computed
	SPAddress     string
	SectorSize    int64
	RequestedSize string
}

func (a *WebRPC) SectorCCScheduler(ctx context.Context) ([]SectorCCScheduler, error) {
	var rows []struct {
		SpID         int64 `db:"sp_id"`
		ToSeal       int64 `db:"to_seal"`
		Weight       int64 `db:"weight"`
		DurationDays int64 `db:"duration_days"`
		Enabled      bool  `db:"enabled"`
	}

	err := a.deps.DB.Select(ctx, &rows, `SELECT sp_id, to_seal, weight, duration_days, enabled FROM sectors_cc_scheduler ORDER BY sp_id`)
	if err != nil {
		return nil, xerrors.Errorf("failed to list cc scheduler entries: %w", err)
	}

	out := make([]SectorCCScheduler, 0, len(rows))
	for _, r := range rows {
		addr := must.One(address.NewIDAddress(uint64(r.SpID)))
		mi, err := a.deps.Chain.StateMinerInfo(ctx, addr, types.EmptyTSK)
		if err != nil {
			return nil, xerrors.Errorf("failed to get miner info for %s: %w", addr, err)
		}
		out = append(out, SectorCCScheduler{
			SpID:          r.SpID,
			ToSeal:        r.ToSeal,
			Weight:        r.Weight,
			DurationDays:  r.DurationDays,
			Enabled:       r.Enabled,
			SPAddress:     addr.String(),
			SectorSize:    int64(mi.SectorSize),
			RequestedSize: types.SizeStr(big.Mul(big.NewInt(r.ToSeal), big.NewInt(int64(mi.SectorSize)))),
		})
	}
	return out, nil
}

func (a *WebRPC) SectorCCSchedulerEdit(ctx context.Context, sp string, toSeal int64, weight int64, durationDays int64, enabled bool) error {
	spaddr, err := address.NewFromString(sp)
	if err != nil {
		return xerrors.Errorf("invalid sp address: %w", err)
	}
	spid, err := address.IDFromAddress(spaddr)
	if err != nil {
		return xerrors.Errorf("id from sp address: %w", err)
	}

	if toSeal < 0 {
		return xerrors.Errorf("toSeal cannot be negative")
	}
	if weight <= 0 {
		return xerrors.Errorf("weight must be positive")
	}

	_, err = a.deps.DB.Exec(ctx, `INSERT INTO sectors_cc_scheduler (sp_id, to_seal, weight, duration_days, enabled) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (sp_id) DO UPDATE SET to_seal = EXCLUDED.to_seal, weight = EXCLUDED.weight, duration_days = EXCLUDED.duration_days, enabled = EXCLUDED.enabled`, spid, toSeal, weight, durationDays, enabled)
	if err != nil {
		return xerrors.Errorf("failed to upsert cc scheduler: %w", err)
	}
	return nil
}

func (a *WebRPC) SectorCCSchedulerDelete(ctx context.Context, sp string) error {
	spaddr, err := address.NewFromString(sp)
	if err != nil {
		return xerrors.Errorf("invalid sp address: %w", err)
	}
	spid, err := address.IDFromAddress(spaddr)
	if err != nil {
		return xerrors.Errorf("id from sp address: %w", err)
	}
	_, err = a.deps.DB.Exec(ctx, `DELETE FROM sectors_cc_scheduler WHERE sp_id = $1`, spid)
	if err != nil {
		return xerrors.Errorf("failed to delete cc scheduler entry: %w", err)
	}
	return nil
}

func derefOrZero[T any](a *T) T {
	if a == nil {
		return *new(T)
	}
	return *a
}
