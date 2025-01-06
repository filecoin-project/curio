package webrpc

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/samber/lo"
	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

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
	UnsealedState      bool

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
	SpID         int64     `db:"sp_id"`
	SectorNumber int64     `db:"sector_number"`
	StartTime    time.Time `db:"start_time"`

	UpgradeProof int  `db:"upgrade_proof"`
	DataAssigned bool `db:"data_assigned"`

	UpdateUnsealedCID *string `db:"update_unsealed_cid"`
	UpdateSealedCID   *string `db:"update_sealed_cid"`

	TaskEncode           *int64  `db:"task_id_encode"`
	AfterEncode          bool    `db:"after_encode"`
	TaskProve            *int64  `db:"task_id_prove"`
	AfterProve           bool    `db:"after_prove"`
	TaskSubmit           *int64  `db:"task_id_submit"`
	AfterSubmit          bool    `db:"after_submit"`
	AfterProveMsgSuccess bool    `db:"after_prove_msg_success"`
	ProveMsgTsk          []byte  `db:"prove_msg_tsk"`
	UpdateMsgCid         *string `db:"prove_msg_cid"`

	TaskMoveStorage  *int64 `db:"task_id_move_storage"`
	AfterMoveStorage bool   `db:"after_move_storage"`

	Failed          bool       `db:"failed"`
	FailedAt        *time.Time `db:"failed_at"`
	FailedReason    string     `db:"failed_reason"`
	FailedReasonMsg string     `db:"failed_reason_msg"`

	SubmitAfter *time.Time `db:"submit_after"`
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

	IsSnapPiece bool `db:"-"`
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
	OrigUnsealedCid string `db:"orig_unsealed_cid"`
	OrigSealedCid   string `db:"orig_unsealed_cid"`

	UpdatedUnsealedCid string `db:"cur_unsealed_cid"`
	UpdatedSealedCid   string `db:"cur_sealed_cid"`

	PreCommitCid string `db:"msg_cid_precommit"`
	CommitCid    string `db:"msg_cid_commit"`
	UpdateCid    string `db:"msg_cid_update"`

	IsCC            bool   `db:"is_cc"`
	ExpirationEpoch *int64 `db:"expiration_epoch"`

	Deadline  *int64 `db:"deadline"`
	Partition *int64 `db:"partition"`

	UnsealedState bool `db:"target_unseal_state"`
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
		if task.PreCommitMsgCid != nil {
			si.PreCommitMsg = *task.PreCommitMsgCid
		} else {
			si.PreCommitMsg = ""
		}

		if task.CommitMsgCid != nil {
			si.CommitMsg = *task.CommitMsgCid
		} else {
			si.CommitMsg = ""
		}

		if task.TreeD != nil {
			si.UnsealedCid = *task.TreeD
			si.UpdatedUnsealedCid = *task.TreeD
		} else {
			si.UnsealedCid = ""
			si.UpdatedUnsealedCid = ""
		}

		if task.TreeR != nil {
			si.SealedCid = *task.TreeR
			si.UpdatedSealedCid = *task.TreeR
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
		if task.UpdateUnsealedCID != nil {
			si.UpdatedUnsealedCid = *task.UpdateUnsealedCID
		} else {
			si.UpdatedUnsealedCid = ""
		}
		if task.UpdateUnsealedCID != nil {
			si.UpdatedSealedCid = *task.UpdateUnsealedCID
		} else {
			si.UpdatedSealedCid = ""
		}
		if task.UpdateMsgCid != nil {
			si.UpdateMsg = *task.UpdateMsgCid
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
       is_cc FROM sectors_sdr_meta 
             WHERE sp_id = $1 AND sector_number = $2`, spid, intid)
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
		si.UpdateMsg = sectormeta.UpdateCid
		si.IsSnap = !sectormeta.IsCC
		if sectormeta.ExpirationEpoch != nil {
			si.ExpirationEpoch = sectormeta.ExpirationEpoch
		}
		if sectormeta.Deadline != nil {
			d := *sectormeta.Deadline
			si.Deadline = &d
		}
		if sectormeta.Partition != nil {
			p := *sectormeta.Partition
			si.Partition = &p
		}
		si.UnsealedState = sectormeta.UnsealedState
	}

	var pieces []SectorPieceMeta

	// Fetch PoRep pieces
	err = a.deps.DB.Select(ctx, &pieces, `SELECT piece_index, piece_cid, piece_size,
           data_url, data_raw_size, data_delete_on_finalize,
           f05_publish_cid, f05_deal_id, direct_piece_activation_manifest FROM sectors_sdr_initial_pieces WHERE sp_id = $1 AND sector_number = $2`, spid, intid)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch sector pieces: %w", err)
	}

	// Fetch SnapDeals pieces
	var snapPieces []SectorPieceMeta

	err = a.deps.DB.Select(ctx, &snapPieces, `SELECT piece_index, piece_cid, piece_size,
           data_url, data_raw_size, data_delete_on_finalize,
           NULL as f05_publish_cid, NULL as f05_deal_id, direct_piece_activation_manifest FROM sectors_snap_initial_pieces WHERE sp_id = $1 AND sector_number = $2`, spid, intid)
	if err != nil {
		return nil, xerrors.Errorf("failed to fetch snap sector pieces: %w", err)
	}

	// Mark SnapDeals pieces
	for i := range snapPieces {
		snapPieces[i].IsSnapPiece = true
	}

	// Combine both slices
	pieces = append(pieces, snapPieces...)

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
	taskIDs := map[int64]struct{}{}

	appendNonNil := func(id *int64) {
		if id != nil {
			taskIDs[*id] = struct{}{}
		}
	}

	// Append PoRep task IDs
	if len(tasks) > 0 {
		task := tasks[0]
		appendNonNil(task.TaskSDR)
		appendNonNil(task.TaskTreeD)
		appendNonNil(task.TaskTreeC)
		appendNonNil(task.TaskTreeR)
		appendNonNil(task.TaskPrecommitMsg)
		appendNonNil(task.TaskPoRep)
		appendNonNil(task.TaskFinalize)
		appendNonNil(task.TaskMoveStorage)
		appendNonNil(task.TaskCommitMsg)
	}

	// Append SnapDeals task IDs
	if len(snapTasks) > 0 {
		task := snapTasks[0]
		appendNonNil(task.TaskEncode)
		appendNonNil(task.TaskProve)
		appendNonNil(task.TaskSubmit)
		appendNonNil(task.TaskMoveStorage)
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
		if ts.HarmonyTaskID == nil {
			hasAnyStuckTask = true
			break
		}
	}

	onChainInfo, err := a.deps.Chain.StateSectorGetInfo(ctx, maddr, abi.SectorNumber(intid), types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("failed to get on chain info for the sector: %w", err)
	}

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
	if si.ExpirationEpoch == nil || *si.ExpirationEpoch != int64(onChainInfo.Expiration) {
		expr := int64(onChainInfo.Expiration)
		si.ExpirationEpoch = &expr
	}
	si.DealWeight = dealWeight

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
