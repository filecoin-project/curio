package webrpc

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/ipfs/go-cid"
	"github.com/samber/lo"
	"github.com/snadrus/must"
	"golang.org/x/sync/errgroup"
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
	TaskEncode        NullInt64  `db:"task_id_encode"`      // 16 bytes
	AfterEncode       bool       `db:"after_encode"`        // 1 byte
	UpdateUnsealedCID NullString `db:"update_unsealed_cid"` // 24 bytes
	TaskProve         NullInt64  `db:"task_id_prove"`       // 16 bytes
	AfterProve        bool       `db:"after_prove"`         // 1 byte
	// Cache line 3 (bytes 128-192): Submit and message stages
	UpdateSealedCID      NullString `db:"update_sealed_cid"`       // 24 bytes
	TaskSubmit           NullInt64  `db:"task_id_submit"`          // 16 bytes
	AfterSubmit          bool       `db:"after_submit"`            // 1 byte
	AfterProveMsgSuccess bool       `db:"after_prove_msg_success"` // 1 byte
	UpdateMsgCid         NullString `db:"prove_msg_cid"`           // 24 bytes (crosses into cache line 4)
	// Cache line 4 (bytes 192-256): Storage and timing
	TaskMoveStorage  NullInt64 `db:"task_id_move_storage"` // 16 bytes
	AfterMoveStorage bool      `db:"after_move_storage"`   // 1 byte
	SubmitAfter      NullTime  `db:"submit_after"`         // 32 bytes
	// Failure info (only accessed when Failed=true)
	FailedAt NullTime `db:"failed_at"` // 32 bytes (crosses into cache line 5)
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
	PipelineTaskID int64    `db:"pipeline_task_id"` // 8 bytes (0-8)
	WorkStart      NullTime `db:"work_start"`       // 32 bytes (8-40)
	WorkEnd        NullTime `db:"work_end"`         // 32 bytes (40-72, crosses to cache line 2)
	// Cache line 2 (bytes 64-128): Task details
	Name        NullString `db:"name"`                       // 24 bytes
	CompletedBy NullString `db:"completed_by_host_and_port"` // 24 bytes
	Result      NullBool   `db:"result"`                     // 2 bytes
	// Cache line 3 (bytes 128+): Error info and display fields (only accessed when needed)
	Err  NullString `db:"err"` // 24 bytes - only accessed when Result is false
	Took string     `db:"-"`   // 16 bytes - display only, computed field
}

// Pieces
type SectorPieceMeta struct {
	// Cache line 1 (bytes 0-64): Hot path - piece identification and size
	PieceIndex  int64     `db:"piece_index"`   // 8 bytes (0-8)
	PieceSize   int64     `db:"piece_size"`    // 8 bytes (8-16)
	PieceCid    string    `db:"piece_cid"`     // 16 bytes (16-32)
	PieceCidV2  string    `db:"-"`             // 16 bytes (32-48) - computed field
	DataRawSize NullInt64 `db:"data_raw_size"` // 16 bytes (48-64)
	// Cache line 2 (bytes 64-128): Deal identification
	F05DealID   NullInt64  `db:"f05_deal_id"` // 16 bytes
	DealID      NullString `db:"deal_id"`     // 24 bytes
	IsSnapPiece bool       `db:"is_snap"`     // 1 byte - frequently checked with PieceIndex
	// Cache line 3 (bytes 128-192): Data access and F05 info
	DataUrl       NullString `db:"data_url"`        // 24 bytes
	F05PublishCid NullString `db:"f05_publish_cid"` // 24 bytes
	// Cache line 4 (bytes 192-256): DDO and display fields
	DDOPam         NullString `db:"direct_piece_activation_manifest"` // 24 bytes
	StrPieceSize   string     `db:"-"`                                // 16 bytes - display only
	StrDataRawSize string     `db:"-"`                                // 16 bytes - display only
	// Piece park fields (rarely accessed, only for parked pieces)
	PieceParkDataUrl       string    `db:"-"` // 16 bytes
	PieceParkCreatedAt     time.Time `db:"-"` // 24 bytes
	PieceParkID            int64     `db:"-"` // 8 bytes
	PieceParkTaskID        *int64    `db:"-"` // 8 bytes - still pointer (not from DB)
	PieceParkCleanupTaskID *int64    `db:"-"` // 8 bytes - still pointer (not from DB)
	// Bools: frequently checked first, rare ones at end (NullBool = 2 bytes each)
	MK12Deal           NullBool `db:"boost_deal"`              // 2 bytes - checked often
	LegacyDeal         NullBool `db:"legacy_deal"`             // 2 bytes - checked often
	DeleteOnFinalize   NullBool `db:"data_delete_on_finalize"` // 2 bytes - checked during finalize
	IsParkedPiece      bool     `db:"-"`                       // rare - only for UI display
	IsParkedPieceFound bool     `db:"-"`                       // rare - only for UI display
	PieceParkComplete  bool     `db:"-"`                       // rare - only for parked pieces
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
	PreCommitCid string     `db:"msg_cid_precommit"` // 16 bytes (64-80)
	CommitCid    string     `db:"msg_cid_commit"`    // 16 bytes (80-96)
	UpdateCid    NullString `db:"msg_cid_update"`    // 24 bytes (96-120) - null for non-snap sectors
	// Cache line 3 (bytes 128-192): On-chain metadata (NullInt64 = 16 bytes each)
	ExpirationEpoch NullInt64 `db:"expiration_epoch"` // 16 bytes
	Deadline        NullInt64 `db:"deadline"`         // 16 bytes
	Partition       NullInt64 `db:"partition"`        // 16 bytes
	// Bools (NullBool = 2 bytes each)
	IsCC          NullBool `db:"is_cc"`               // 2 bytes
	UnsealedState NullBool `db:"target_unseal_state"` // 2 bytes
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
			AfterSeed:    task.SeedEpoch != nil && *task.SeedEpoch <= int64(epoch),

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
			CreatedAt     time.Time `db:"created_at"`
			Complete      bool      `db:"complete"`
			ParkTaskID    NullInt64 `db:"task_id"`
			CleanupTaskID NullInt64 `db:"cleanup_task_id"`
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

	appendNullInt64 := func(n NullInt64) {
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
		PipelineID    int64     `db:"pipeline_id"`
		HarmonyTaskID NullInt64 `db:"harmony_task_id"`
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

// Sector Dashboard API

type SPSectorStats struct {
	SpID       int64  `json:"sp_id"`
	SPAddress  string `json:"sp_address"`
	TotalCount int64  `json:"total_count"`
	CCCount    int64  `json:"cc_count"`
	NonCCCount int64  `json:"non_cc_count"`
}

func (a *WebRPC) SectorSPStats(ctx context.Context) ([]SPSectorStats, error) {
	var stats []struct {
		SpID       int64 `db:"sp_id"`
		TotalCount int64 `db:"total_count"`
		CCCount    int64 `db:"cc_count"`
	}

	err := a.deps.DB.Select(ctx, &stats, `
		SELECT 
			sm.sp_id,
			COUNT(*) as total_count,
			COUNT(*) FILTER (WHERE sm.is_cc = true) as cc_count
		FROM sectors_meta sm
		GROUP BY sm.sp_id
		ORDER BY sm.sp_id`)
	if err != nil {
		return nil, xerrors.Errorf("failed to query SP sector stats: %w", err)
	}

	result := make([]SPSectorStats, 0, len(stats))
	for _, s := range stats {
		addr := must.One(address.NewIDAddress(uint64(s.SpID)))
		result = append(result, SPSectorStats{
			SpID:       s.SpID,
			SPAddress:  addr.String(),
			TotalCount: s.TotalCount,
			CCCount:    s.CCCount,
			NonCCCount: s.TotalCount - s.CCCount,
		})
	}

	return result, nil
}

type SectorPipelineStats struct {
	PipelineType string `json:"pipeline_type"`
	Stage        string `json:"stage"`
	Count        int64  `json:"count"`
}

func (a *WebRPC) SectorPipelineStats(ctx context.Context) ([]SectorPipelineStats, error) {
	var result []SectorPipelineStats

	// PoRep pipeline stats
	var porepStats []struct {
		Stage string `db:"stage"`
		Count int64  `db:"count"`
	}

	err := a.deps.DB.Select(ctx, &porepStats, `
		SELECT stage, COUNT(*) as count
		FROM (
			SELECT 
				CASE 
					WHEN NOT after_sdr THEN 'SDR'
					WHEN NOT after_tree_d THEN 'TreeD'
					WHEN NOT after_tree_c THEN 'TreeC'
					WHEN NOT after_tree_r THEN 'TreeR'
					WHEN after_synth IS NOT NULL AND NOT after_synth THEN 'Synthetic'
					WHEN NOT after_precommit_msg THEN 'PreCommit Msg'
					WHEN NOT after_precommit_msg_success THEN 'Wait Seed'
					WHEN NOT after_porep THEN 'PoRep'
					WHEN NOT after_finalize THEN 'Finalize'
					WHEN NOT after_move_storage THEN 'Move Storage'
					WHEN NOT after_commit_msg THEN 'Commit Msg'
					WHEN NOT after_commit_msg_success THEN 'Wait Commit'
					ELSE 'Complete'
				END as stage,
				CASE 
					WHEN NOT after_sdr THEN 1
					WHEN NOT after_tree_d THEN 2
					WHEN NOT after_tree_c THEN 3
					WHEN NOT after_tree_r THEN 4
					WHEN after_synth IS NOT NULL AND NOT after_synth THEN 5
					WHEN NOT after_precommit_msg THEN 6
					WHEN NOT after_precommit_msg_success THEN 7
					WHEN NOT after_porep THEN 8
					WHEN NOT after_finalize THEN 9
					WHEN NOT after_move_storage THEN 10
					WHEN NOT after_commit_msg THEN 11
					WHEN NOT after_commit_msg_success THEN 12
					ELSE 13
				END as sort_order
			FROM sectors_sdr_pipeline
			WHERE NOT failed
		) sub
		GROUP BY stage, sort_order
		ORDER BY sort_order`)
	if err != nil {
		return nil, xerrors.Errorf("failed to query PoRep pipeline stats: %w", err)
	}

	for _, s := range porepStats {
		result = append(result, SectorPipelineStats{
			PipelineType: "PoRep",
			Stage:        s.Stage,
			Count:        s.Count,
		})
	}

	// Snap pipeline stats
	var snapStats []struct {
		Stage string `db:"stage"`
		Count int64  `db:"count"`
	}

	err = a.deps.DB.Select(ctx, &snapStats, `
		SELECT stage, COUNT(*) as count
		FROM (
			SELECT 
				CASE 
					WHEN NOT data_assigned THEN 'Data Assignment'
					WHEN NOT after_encode THEN 'Encode'
					WHEN NOT after_prove THEN 'Prove'
					WHEN NOT after_submit THEN 'Submit'
					WHEN NOT after_prove_msg_success THEN 'Wait Prove Msg'
					WHEN NOT after_move_storage THEN 'Move Storage'
					ELSE 'Complete'
				END as stage,
				CASE 
					WHEN NOT data_assigned THEN 1
					WHEN NOT after_encode THEN 2
					WHEN NOT after_prove THEN 3
					WHEN NOT after_submit THEN 4
					WHEN NOT after_prove_msg_success THEN 5
					WHEN NOT after_move_storage THEN 6
					ELSE 7
				END as sort_order
			FROM sectors_snap_pipeline
			WHERE NOT failed
		) sub
		GROUP BY stage, sort_order
		ORDER BY sort_order`)
	if err != nil {
		return nil, xerrors.Errorf("failed to query Snap pipeline stats: %w", err)
	}

	for _, s := range snapStats {
		result = append(result, SectorPipelineStats{
			PipelineType: "Snap",
			Stage:        s.Stage,
			Count:        s.Count,
		})
	}

	// Failed sectors
	var failedCount struct {
		PoRepFailed int64 `db:"porep_failed"`
		SnapFailed  int64 `db:"snap_failed"`
	}

	err = a.deps.DB.QueryRow(ctx, `
		SELECT 
			(SELECT COUNT(*) FROM sectors_sdr_pipeline WHERE failed) as porep_failed,
			(SELECT COUNT(*) FROM sectors_snap_pipeline WHERE failed) as snap_failed`).Scan(&failedCount.PoRepFailed, &failedCount.SnapFailed)
	if err != nil {
		return nil, xerrors.Errorf("failed to query failed sectors: %w", err)
	}

	if failedCount.PoRepFailed > 0 {
		result = append(result, SectorPipelineStats{
			PipelineType: "PoRep",
			Stage:        "Failed",
			Count:        failedCount.PoRepFailed,
		})
	}

	if failedCount.SnapFailed > 0 {
		result = append(result, SectorPipelineStats{
			PipelineType: "Snap",
			Stage:        "Failed",
			Count:        failedCount.SnapFailed,
		})
	}

	return result, nil
}

type DeadlineStats struct {
	SpID              int64  `json:"sp_id"`
	SPAddress         string `json:"sp_address"`
	Deadline          int64  `json:"deadline"`
	Count             int64  `json:"count"`
	AllSectors        int64  `json:"all_sectors"`
	FaultySectors     int64  `json:"faulty_sectors"`
	RecoveringSectors int64  `json:"recovering_sectors"`
	LiveSectors       int64  `json:"live_sectors"`
	ActiveSectors     int64  `json:"active_sectors"`
	PostSubmissions   string `json:"post_submissions"`
}

func (a *WebRPC) SectorDeadlineStats(ctx context.Context) ([]DeadlineStats, error) {
	var stats []struct {
		SpID     int64 `db:"sp_id"`
		Deadline int64 `db:"deadline"`
		Count    int64 `db:"count"`
	}

	err := a.deps.DB.Select(ctx, &stats, `
		SELECT sp_id, deadline, COUNT(*) as count
		FROM sectors_meta
		WHERE deadline IS NOT NULL
		GROUP BY sp_id, deadline
		ORDER BY sp_id, deadline`)
	if err != nil {
		return nil, xerrors.Errorf("failed to query deadline stats: %w", err)
	}

	// Group by SP for parallel fetching
	type spDeadlines struct {
		spID      int64
		spAddr    address.Address
		deadlines []int64
	}
	spMap := make(map[int64]*spDeadlines)
	for _, s := range stats {
		if _, ok := spMap[s.SpID]; !ok {
			addr := must.One(address.NewIDAddress(uint64(s.SpID)))
			spMap[s.SpID] = &spDeadlines{
				spID:      s.SpID,
				spAddr:    addr,
				deadlines: []int64{},
			}
		}
		spMap[s.SpID].deadlines = append(spMap[s.SpID].deadlines, s.Deadline)
	}

	// Fetch deadline info from chain in parallel
	type deadlineInfo struct {
		spID              int64
		deadline          int64
		allSectors        int64
		faultySectors     int64
		recoveringSectors int64
		liveSectors       int64
		activeSectors     int64
		postSubmissions   string
	}

	var eg errgroup.Group
	eg.SetLimit(10) // Limit concurrent requests

	deadlineInfoChan := make(chan deadlineInfo, len(stats))

	for _, sp := range spMap {
		sp := sp
		eg.Go(func() error {
			// Get deadlines for this miner
			deadlines, err := a.deps.Chain.StateMinerDeadlines(ctx, sp.spAddr, types.EmptyTSK)
			if err != nil {
				// If we can't get deadline info, continue without it
				log.Warnw("failed to get deadlines", "miner", sp.spAddr, "error", err)
				return nil
			}

			// For each deadline we're interested in
			for _, dlIdx := range sp.deadlines {
				if dlIdx >= int64(len(deadlines)) {
					continue
				}

				dl := deadlines[dlIdx]

				// Get partitions for this deadline
				parts, err := a.deps.Chain.StateMinerPartitions(ctx, sp.spAddr, uint64(dlIdx), types.EmptyTSK)
				if err != nil {
					log.Warnw("failed to get partitions", "miner", sp.spAddr, "deadline", dlIdx, "error", err)
					continue
				}

				info := deadlineInfo{
					spID:     sp.spID,
					deadline: dlIdx,
				}

				// Aggregate partition stats
				for _, part := range parts {
					allCount := must.One(part.AllSectors.Count())
					faultyCount := must.One(part.FaultySectors.Count())
					recoveringCount := must.One(part.RecoveringSectors.Count())
					liveCount := must.One(part.LiveSectors.Count())
					activeCount := must.One(part.ActiveSectors.Count())

					info.allSectors += int64(allCount)
					info.faultySectors += int64(faultyCount)
					info.recoveringSectors += int64(recoveringCount)
					info.liveSectors += int64(liveCount)
					info.activeSectors += int64(activeCount)
				}

				// Get post submissions bitfield representation
				postCount := must.One(dl.PostSubmissions.Count())
				info.postSubmissions = fmt.Sprintf("%d/%d", postCount, len(parts))

				deadlineInfoChan <- info
			}

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, xerrors.Errorf("failed to fetch deadline info: %w", err)
	}
	close(deadlineInfoChan)

	// Merge chain info with DB stats
	chainInfoMap := make(map[string]deadlineInfo)
	for info := range deadlineInfoChan {
		key := fmt.Sprintf("%d-%d", info.spID, info.deadline)
		chainInfoMap[key] = info
	}

	result := make([]DeadlineStats, 0, len(stats))
	for _, s := range stats {
		addr := must.One(address.NewIDAddress(uint64(s.SpID)))
		ds := DeadlineStats{
			SpID:      s.SpID,
			SPAddress: addr.String(),
			Deadline:  s.Deadline,
			Count:     s.Count,
		}

		// Add chain info if available
		key := fmt.Sprintf("%d-%d", s.SpID, s.Deadline)
		if info, ok := chainInfoMap[key]; ok {
			ds.AllSectors = info.allSectors
			ds.FaultySectors = info.faultySectors
			ds.RecoveringSectors = info.recoveringSectors
			ds.LiveSectors = info.liveSectors
			ds.ActiveSectors = info.activeSectors
			ds.PostSubmissions = info.postSubmissions
		}

		result = append(result, ds)
	}

	return result, nil
}

type SectorFileTypeStats struct {
	FileType string `json:"file_type"`
	Count    int64  `json:"count"`
}

func (a *WebRPC) SectorFileTypeStats(ctx context.Context) ([]SectorFileTypeStats, error) {
	var stats []struct {
		FileType int   `db:"sector_filetype"`
		Count    int64 `db:"count"`
	}

	err := a.deps.DB.Select(ctx, &stats, `
		SELECT sector_filetype, COUNT(DISTINCT (miner_id, sector_num)) as count
		FROM sector_location
		GROUP BY sector_filetype
		ORDER BY sector_filetype`)
	if err != nil {
		return nil, xerrors.Errorf("failed to query file type stats: %w", err)
	}

	result := make([]SectorFileTypeStats, 0, len(stats))
	for _, s := range stats {
		result = append(result, SectorFileTypeStats{
			FileType: storiface.SectorFileType(s.FileType).String(),
			Count:    s.Count,
		})
	}

	return result, nil
}

// Deadline Detail Page

type DeadlinePartitionInfo struct {
	Partition         uint64 `json:"partition"`
	AllSectors        uint64 `json:"all_sectors"`
	FaultySectors     uint64 `json:"faulty_sectors"`
	RecoveringSectors uint64 `json:"recovering_sectors"`
	LiveSectors       uint64 `json:"live_sectors"`
	ActiveSectors     uint64 `json:"active_sectors"`
}

type DeadlineDetail struct {
	SpID                 int64                   `json:"sp_id"`
	SPAddress            string                  `json:"sp_address"`
	Deadline             uint64                  `json:"deadline"`
	PostSubmissions      string                  `json:"post_submissions"`
	DisputableProofCount uint64                  `json:"disputable_proof_count"`
	Partitions           []DeadlinePartitionInfo `json:"partitions"`
}

func (a *WebRPC) DeadlineDetail(ctx context.Context, sp string, deadlineIdx uint64) (*DeadlineDetail, error) {
	maddr, err := address.NewFromString(sp)
	if err != nil {
		return nil, xerrors.Errorf("invalid sp address: %w", err)
	}
	spid, err := address.IDFromAddress(maddr)
	if err != nil {
		return nil, xerrors.Errorf("id from sp address: %w", err)
	}

	// Get deadline info from chain
	deadlines, err := a.deps.Chain.StateMinerDeadlines(ctx, maddr, types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("failed to get deadlines: %w", err)
	}

	if deadlineIdx >= uint64(len(deadlines)) {
		return nil, xerrors.Errorf("deadline %d does not exist", deadlineIdx)
	}

	dl := deadlines[deadlineIdx]

	// Get partitions for this deadline
	parts, err := a.deps.Chain.StateMinerPartitions(ctx, maddr, deadlineIdx, types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("failed to get partitions: %w", err)
	}

	partitions := make([]DeadlinePartitionInfo, 0, len(parts))
	for i, part := range parts {
		partitions = append(partitions, DeadlinePartitionInfo{
			Partition:         uint64(i),
			AllSectors:        must.One(part.AllSectors.Count()),
			FaultySectors:     must.One(part.FaultySectors.Count()),
			RecoveringSectors: must.One(part.RecoveringSectors.Count()),
			LiveSectors:       must.One(part.LiveSectors.Count()),
			ActiveSectors:     must.One(part.ActiveSectors.Count()),
		})
	}

	postCount := must.One(dl.PostSubmissions.Count())
	return &DeadlineDetail{
		SpID:                 int64(spid),
		SPAddress:            maddr.String(),
		Deadline:             deadlineIdx,
		PostSubmissions:      fmt.Sprintf("%d/%d", postCount, len(parts)),
		DisputableProofCount: dl.DisputableProofCount,
		Partitions:           partitions,
	}, nil
}

// Partition Detail Page

type PartitionSectorInfo struct {
	SectorNumber uint64 `json:"sector_number"`
	IsFaulty     bool   `json:"is_faulty"`
	IsRecovering bool   `json:"is_recovering"`
	IsActive     bool   `json:"is_active"`
	IsLive       bool   `json:"is_live"`
}

type StoragePathStat struct {
	StorageID string   `json:"storage_id"`
	PathType  string   `json:"path_type"`
	Urls      []string `json:"urls"`
	Count     int      `json:"count"`
}

type PartitionDetail struct {
	SpID                   int64                 `json:"sp_id"`
	SPAddress              string                `json:"sp_address"`
	Deadline               uint64                `json:"deadline"`
	Partition              uint64                `json:"partition"`
	AllSectorsCount        uint64                `json:"all_sectors_count"`
	FaultySectorsCount     uint64                `json:"faulty_sectors_count"`
	RecoveringSectorsCount uint64                `json:"recovering_sectors_count"`
	LiveSectorsCount       uint64                `json:"live_sectors_count"`
	ActiveSectorsCount     uint64                `json:"active_sectors_count"`
	Sectors                []PartitionSectorInfo `json:"sectors"`
	FaultyStoragePaths     []StoragePathStat     `json:"faulty_storage_paths"`
}

// Sector Expiration Buckets API

type SectorExpBucket struct {
	LessThanDays int `json:"less_than_days" db:"less_than_days"`
}

func (a *WebRPC) SectorExpBuckets(ctx context.Context) ([]SectorExpBucket, error) {
	var buckets []SectorExpBucket
	err := a.deps.DB.Select(ctx, &buckets, `SELECT less_than_days FROM sectors_exp_buckets ORDER BY less_than_days`)
	if err != nil {
		return nil, xerrors.Errorf("failed to query sector expiration buckets: %w", err)
	}
	return buckets, nil
}

func (a *WebRPC) SectorExpBucketAdd(ctx context.Context, lessThanDays int) error {
	if lessThanDays <= 0 {
		return xerrors.Errorf("lessThanDays must be positive")
	}
	_, err := a.deps.DB.Exec(ctx, `INSERT INTO sectors_exp_buckets (less_than_days) VALUES ($1) ON CONFLICT DO NOTHING`, lessThanDays)
	if err != nil {
		return xerrors.Errorf("failed to add sector expiration bucket: %w", err)
	}
	return nil
}

func (a *WebRPC) SectorExpBucketDelete(ctx context.Context, lessThanDays int) error {
	_, err := a.deps.DB.Exec(ctx, `DELETE FROM sectors_exp_buckets WHERE less_than_days = $1`, lessThanDays)
	if err != nil {
		return xerrors.Errorf("failed to delete sector expiration bucket: %w", err)
	}
	return nil
}

// Sector Expiration Manager Presets API

type SectorExpManagerPreset struct {
	Name                    string `json:"name" db:"name"`
	ActionType              string `json:"action_type" db:"action_type"`
	InfoBucketAboveDays     int    `json:"info_bucket_above_days" db:"info_bucket_above_days"`
	InfoBucketBelowDays     int    `json:"info_bucket_below_days" db:"info_bucket_below_days"`
	TargetExpirationDays    *int64 `json:"target_expiration_days" db:"target_expiration_days"`
	MaxCandidateDays        *int64 `json:"max_candidate_days" db:"max_candidate_days"`
	TopUpCountLowWaterMark  *int64 `json:"top_up_count_low_water_mark" db:"top_up_count_low_water_mark"`
	TopUpCountHighWaterMark *int64 `json:"top_up_count_high_water_mark" db:"top_up_count_high_water_mark"`
	CC                      *bool  `json:"cc" db:"cc"`
	DropClaims              bool   `json:"drop_claims" db:"drop_claims"`
}

func (a *WebRPC) SectorExpManagerPresets(ctx context.Context) ([]SectorExpManagerPreset, error) {
	var presets []SectorExpManagerPreset
	err := a.deps.DB.Select(ctx, &presets, `SELECT * FROM sectors_exp_manager_presets ORDER BY name`)
	if err != nil {
		return nil, xerrors.Errorf("failed to query sector expiration manager presets: %w", err)
	}
	return presets, nil
}

func (a *WebRPC) SectorExpManagerPresetAdd(ctx context.Context, preset SectorExpManagerPreset) error {
	_, err := a.deps.DB.Exec(ctx, `
		INSERT INTO sectors_exp_manager_presets 
		(name, action_type, info_bucket_above_days, info_bucket_below_days, 
		 target_expiration_days, max_candidate_days, 
		 top_up_count_low_water_mark, top_up_count_high_water_mark, cc, drop_claims)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		preset.Name, preset.ActionType, preset.InfoBucketAboveDays, preset.InfoBucketBelowDays,
		preset.TargetExpirationDays, preset.MaxCandidateDays,
		preset.TopUpCountLowWaterMark, preset.TopUpCountHighWaterMark, preset.CC, preset.DropClaims)
	if err != nil {
		return xerrors.Errorf("failed to add sector expiration manager preset: %w", err)
	}
	return nil
}

func (a *WebRPC) SectorExpManagerPresetUpdate(ctx context.Context, preset SectorExpManagerPreset) error {
	_, err := a.deps.DB.Exec(ctx, `
		UPDATE sectors_exp_manager_presets 
		SET action_type = $2, info_bucket_above_days = $3, info_bucket_below_days = $4,
		    target_expiration_days = $5, max_candidate_days = $6,
		    top_up_count_low_water_mark = $7, top_up_count_high_water_mark = $8, 
		    cc = $9, drop_claims = $10
		WHERE name = $1`,
		preset.Name, preset.ActionType, preset.InfoBucketAboveDays, preset.InfoBucketBelowDays,
		preset.TargetExpirationDays, preset.MaxCandidateDays,
		preset.TopUpCountLowWaterMark, preset.TopUpCountHighWaterMark, preset.CC, preset.DropClaims)
	if err != nil {
		return xerrors.Errorf("failed to update sector expiration manager preset: %w", err)
	}
	return nil
}

func (a *WebRPC) SectorExpManagerPresetDelete(ctx context.Context, name string) error {
	_, err := a.deps.DB.Exec(ctx, `DELETE FROM sectors_exp_manager_presets WHERE name = $1`, name)
	if err != nil {
		return xerrors.Errorf("failed to delete sector expiration manager preset: %w", err)
	}
	return nil
}

// Sector Expiration Manager SP Assignments API

type SectorExpManagerSP struct {
	SpID                int64   `json:"sp_id" db:"sp_id"`
	SPAddress           string  `json:"sp_address"`
	PresetName          string  `json:"preset_name" db:"preset_name"`
	Enabled             bool    `json:"enabled" db:"enabled"`
	LastRunAt           *string `json:"last_run_at" db:"last_run_at"`
	LastMessageCID      *string `json:"last_message_cid" db:"last_message_cid"`
	LastMessageLandedAt *string `json:"last_message_landed_at" db:"last_message_landed_at"`
}

func (a *WebRPC) SectorExpManagerSPs(ctx context.Context) ([]SectorExpManagerSP, error) {
	var rows []struct {
		SpID                int64      `db:"sp_id"`
		PresetName          string     `db:"preset_name"`
		Enabled             bool       `db:"enabled"`
		LastRunAt           NullTime   `db:"last_run_at"`
		LastMessageCID      NullString `db:"last_message_cid"`
		LastMessageLandedAt NullTime   `db:"last_message_landed_at"`
	}
	err := a.deps.DB.Select(ctx, &rows, `SELECT * FROM sectors_exp_manager_sp ORDER BY sp_id, preset_name`)
	if err != nil {
		return nil, xerrors.Errorf("failed to query sector expiration manager SP assignments: %w", err)
	}

	result := make([]SectorExpManagerSP, 0, len(rows))
	for _, r := range rows {
		addr := must.One(address.NewIDAddress(uint64(r.SpID)))
		item := SectorExpManagerSP{
			SpID:       r.SpID,
			SPAddress:  addr.String(),
			PresetName: r.PresetName,
			Enabled:    r.Enabled,
		}
		if r.LastRunAt.Valid {
			t := r.LastRunAt.Time.Format("2006-01-02 15:04:05")
			item.LastRunAt = &t
		}
		if r.LastMessageCID.Valid {
			item.LastMessageCID = &r.LastMessageCID.String
		}
		if r.LastMessageLandedAt.Valid {
			t := r.LastMessageLandedAt.Time.Format("2006-01-02 15:04:05")
			item.LastMessageLandedAt = &t
		}
		result = append(result, item)
	}
	return result, nil
}

func (a *WebRPC) SectorExpManagerSPAdd(ctx context.Context, spAddress string, presetName string) error {
	maddr, err := address.NewFromString(spAddress)
	if err != nil {
		return xerrors.Errorf("invalid sp address: %w", err)
	}
	spid, err := address.IDFromAddress(maddr)
	if err != nil {
		return xerrors.Errorf("id from sp address: %w", err)
	}

	_, err = a.deps.DB.Exec(ctx, `
		INSERT INTO sectors_exp_manager_sp (sp_id, preset_name, enabled)
		VALUES ($1, $2, false)
		ON CONFLICT (sp_id, preset_name) DO NOTHING`,
		spid, presetName)
	if err != nil {
		return xerrors.Errorf("failed to add sector expiration manager SP assignment: %w", err)
	}
	return nil
}

func (a *WebRPC) SectorExpManagerSPToggle(ctx context.Context, spAddress string, presetName string, enabled bool) error {
	maddr, err := address.NewFromString(spAddress)
	if err != nil {
		return xerrors.Errorf("invalid sp address: %w", err)
	}
	spid, err := address.IDFromAddress(maddr)
	if err != nil {
		return xerrors.Errorf("id from sp address: %w", err)
	}

	_, err = a.deps.DB.Exec(ctx, `
		UPDATE sectors_exp_manager_sp SET enabled = $3
		WHERE sp_id = $1 AND preset_name = $2`,
		spid, presetName, enabled)
	if err != nil {
		return xerrors.Errorf("failed to toggle sector expiration manager SP assignment: %w", err)
	}
	return nil
}

func (a *WebRPC) SectorExpManagerSPDelete(ctx context.Context, spAddress string, presetName string) error {
	maddr, err := address.NewFromString(spAddress)
	if err != nil {
		return xerrors.Errorf("invalid sp address: %w", err)
	}
	spid, err := address.IDFromAddress(maddr)
	if err != nil {
		return xerrors.Errorf("id from sp address: %w", err)
	}

	_, err = a.deps.DB.Exec(ctx, `
		DELETE FROM sectors_exp_manager_sp 
		WHERE sp_id = $1 AND preset_name = $2`,
		spid, presetName)
	if err != nil {
		return xerrors.Errorf("failed to delete sector expiration manager SP assignment: %w", err)
	}
	return nil
}

func (a *WebRPC) SectorExpManagerSPEvalCondition(ctx context.Context, spAddress string, presetName string) (bool, error) {
	maddr, err := address.NewFromString(spAddress)
	if err != nil {
		return false, xerrors.Errorf("invalid sp address: %w", err)
	}
	spid, err := address.IDFromAddress(maddr)
	if err != nil {
		return false, xerrors.Errorf("id from sp address: %w", err)
	}

	head, err := a.deps.Chain.ChainHead(ctx)
	if err != nil {
		return false, xerrors.Errorf("failed to get chain head: %w", err)
	}
	currentEpoch := head.Height()

	var needsAction bool
	err = a.deps.DB.QueryRow(ctx, `
		SELECT eval_ext_mgr_sp_condition($1, $2, $3, 2880)`,
		spid, presetName, currentEpoch).Scan(&needsAction)
	if err != nil {
		return false, xerrors.Errorf("failed to evaluate condition: %w", err)
	}

	return needsAction, nil
}

type SectorExpBucketCount struct {
	SpID         int64  `json:"sp_id" db:"sp_id"`
	SPAddress    string `json:"sp_address"`
	LessThanDays int    `json:"less_than_days" db:"less_than_days"`
	TotalCount   int64  `json:"total_count" db:"total_count"`
	CCCount      int64  `json:"cc_count" db:"cc_count"`
	DealCount    int64  `json:"deal_count" db:"deal_count"`
}

func (a *WebRPC) SectorExpBucketCounts(ctx context.Context) ([]SectorExpBucketCount, error) {
	head, err := a.deps.Chain.ChainHead(ctx)
	if err != nil {
		return nil, xerrors.Errorf("failed to get chain head: %w", err)
	}
	currentEpoch := head.Height()

	var results []struct {
		SpID         int64 `db:"sp_id"`
		LessThanDays int   `db:"less_than_days"`
		TotalCount   int64 `db:"total_count"`
		CCCount      int64 `db:"cc_count"`
	}

	// Calculate counts per SP and bucket
	// Bucket logic: sectors expiring in ranges between buckets
	// The query returns cumulative counts (< N days), UI will calculate ranges
	err = a.deps.DB.Select(ctx, &results, `
		WITH buckets AS (
			SELECT less_than_days FROM sectors_exp_buckets
		),
		sector_buckets AS (
			SELECT 
				sm.sp_id,
				b.less_than_days,
				COUNT(*) as total_count,
				COUNT(*) FILTER (WHERE sm.is_cc = true) as cc_count
			FROM sectors_meta sm
			CROSS JOIN buckets b
			WHERE sm.expiration_epoch IS NOT NULL
				AND sm.expiration_epoch > $1
				AND sm.expiration_epoch < $1 + (b.less_than_days * 2880)
			GROUP BY sm.sp_id, b.less_than_days
		)
		SELECT * FROM sector_buckets
		ORDER BY sp_id, less_than_days`, currentEpoch)
	if err != nil {
		return nil, xerrors.Errorf("failed to query sector expiration bucket counts: %w", err)
	}

	// Add open-ended bucket for sectors beyond the last bucket
	var openEndedResults []struct {
		SpID       int64 `db:"sp_id"`
		TotalCount int64 `db:"total_count"`
		CCCount    int64 `db:"cc_count"`
	}

	err = a.deps.DB.Select(ctx, &openEndedResults, `
		SELECT 
			sm.sp_id,
			COUNT(*) as total_count,
			COUNT(*) FILTER (WHERE sm.is_cc = true) as cc_count
		FROM sectors_meta sm
		WHERE sm.expiration_epoch IS NOT NULL
			AND sm.expiration_epoch > $1 + (
				SELECT COALESCE(MAX(less_than_days), 0) * 2880 FROM sectors_exp_buckets
			)
		GROUP BY sm.sp_id
		ORDER BY sp_id`, currentEpoch)
	if err != nil {
		return nil, xerrors.Errorf("failed to query open-ended bucket counts: %w", err)
	}

	// Convert to output format with address
	output := make([]SectorExpBucketCount, 0, len(results)+len(openEndedResults))
	for _, r := range results {
		addr := must.One(address.NewIDAddress(uint64(r.SpID)))
		output = append(output, SectorExpBucketCount{
			SpID:         r.SpID,
			SPAddress:    addr.String(),
			LessThanDays: r.LessThanDays,
			TotalCount:   r.TotalCount,
			CCCount:      r.CCCount,
			DealCount:    r.TotalCount - r.CCCount,
		})
	}

	// Add open-ended results with special marker (-1)
	for _, r := range openEndedResults {
		addr := must.One(address.NewIDAddress(uint64(r.SpID)))
		output = append(output, SectorExpBucketCount{
			SpID:         r.SpID,
			SPAddress:    addr.String(),
			LessThanDays: -1, // Special marker for open-ended bucket
			TotalCount:   r.TotalCount,
			CCCount:      r.CCCount,
			DealCount:    r.TotalCount - r.CCCount,
		})
	}

	return output, nil
}

func (a *WebRPC) PartitionDetail(ctx context.Context, sp string, deadlineIdx uint64, partitionIdx uint64) (*PartitionDetail, error) {
	maddr, err := address.NewFromString(sp)
	if err != nil {
		return nil, xerrors.Errorf("invalid sp address: %w", err)
	}
	spid, err := address.IDFromAddress(maddr)
	if err != nil {
		return nil, xerrors.Errorf("id from sp address: %w", err)
	}

	// Get partitions for this deadline
	parts, err := a.deps.Chain.StateMinerPartitions(ctx, maddr, deadlineIdx, types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("failed to get partitions: %w", err)
	}

	if partitionIdx >= uint64(len(parts)) {
		return nil, xerrors.Errorf("partition %d does not exist in deadline %d", partitionIdx, deadlineIdx)
	}

	part := parts[partitionIdx]

	// Convert bitfields to maps for quick lookup
	faultyMap := make(map[uint64]bool)
	recoveringMap := make(map[uint64]bool)
	activeMap := make(map[uint64]bool)
	liveMap := make(map[uint64]bool)

	if err := part.FaultySectors.ForEach(func(i uint64) error {
		faultyMap[i] = true
		return nil
	}); err != nil {
		return nil, xerrors.Errorf("failed to iterate faulty sectors: %w", err)
	}

	if err := part.RecoveringSectors.ForEach(func(i uint64) error {
		recoveringMap[i] = true
		return nil
	}); err != nil {
		return nil, xerrors.Errorf("failed to iterate recovering sectors: %w", err)
	}

	if err := part.ActiveSectors.ForEach(func(i uint64) error {
		activeMap[i] = true
		return nil
	}); err != nil {
		return nil, xerrors.Errorf("failed to iterate active sectors: %w", err)
	}

	if err := part.LiveSectors.ForEach(func(i uint64) error {
		liveMap[i] = true
		return nil
	}); err != nil {
		return nil, xerrors.Errorf("failed to iterate live sectors: %w", err)
	}

	// Get all sectors in partition
	sectors := make([]PartitionSectorInfo, 0)
	faultySectorNums := make([]uint64, 0)

	if err := part.AllSectors.ForEach(func(sectorNum uint64) error {
		isFaulty := faultyMap[sectorNum]
		if isFaulty {
			faultySectorNums = append(faultySectorNums, sectorNum)
		}

		sectors = append(sectors, PartitionSectorInfo{
			SectorNumber: sectorNum,
			IsFaulty:     isFaulty,
			IsRecovering: recoveringMap[sectorNum],
			IsActive:     activeMap[sectorNum],
			IsLive:       liveMap[sectorNum],
		})
		return nil
	}); err != nil {
		return nil, xerrors.Errorf("failed to iterate all sectors: %w", err)
	}

	// Get storage path stats for faulty sectors
	var pathStats []StoragePathStat
	if len(faultySectorNums) > 0 {
		type pathRow struct {
			StorageID string `db:"storage_id"`
			CanSeal   bool   `db:"can_seal"`
			CanStore  bool   `db:"can_store"`
			Urls      string `db:"urls"`
			Count     int    `db:"count"`
		}
		var pathRows []pathRow

		err = a.deps.DB.Select(ctx, &pathRows, `
			SELECT 
				sl.storage_id,
				sp.can_seal,
				sp.can_store,
				sp.urls,
				COUNT(DISTINCT sl.sector_num) as count
			FROM sector_location sl
			JOIN storage_path sp ON sl.storage_id = sp.storage_id
			WHERE sl.miner_id = $1 
				AND sl.sector_num = ANY($2)
			GROUP BY sl.storage_id, sp.can_seal, sp.can_store, sp.urls
			ORDER BY count DESC`, spid, faultySectorNums)
		if err != nil {
			return nil, xerrors.Errorf("failed to query storage paths: %w", err)
		}

		pathStats = make([]StoragePathStat, 0, len(pathRows))
		for _, p := range pathRows {
			pathType := "None"
			if p.CanSeal && p.CanStore {
				pathType = "Seal/Store"
			} else if p.CanSeal {
				pathType = "Seal"
			} else if p.CanStore {
				pathType = "Store"
			}

			urls := strings.Split(p.Urls, paths.URLSeparator)
			pathStats = append(pathStats, StoragePathStat{
				StorageID: p.StorageID,
				PathType:  pathType,
				Urls:      urls,
				Count:     p.Count,
			})
		}
	}

	return &PartitionDetail{
		SpID:                   int64(spid),
		SPAddress:              maddr.String(),
		Deadline:               deadlineIdx,
		Partition:              partitionIdx,
		AllSectorsCount:        must.One(part.AllSectors.Count()),
		FaultySectorsCount:     must.One(part.FaultySectors.Count()),
		RecoveringSectorsCount: must.One(part.RecoveringSectors.Count()),
		LiveSectorsCount:       must.One(part.LiveSectors.Count()),
		ActiveSectorsCount:     must.One(part.ActiveSectors.Count()),
		Sectors:                sectors,
		FaultyStoragePaths:     pathStats,
	}, nil
}
