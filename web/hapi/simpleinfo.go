package hapi

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/gorilla/mux"
	"github.com/samber/lo"
	"github.com/snadrus/must"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/paths"

	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/sealer/storiface"
)

type app struct {
	db   *harmonydb.DB
	deps *deps.Deps
	t    *template.Template
	stor adt.Store
}

func (a *app) indexMachines(w http.ResponseWriter, r *http.Request) {
	s, err := a.clusterMachineSummary(r.Context())
	if err != nil {
		log.Errorf("cluster machine summary: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	a.executeTemplate(w, "cluster_machines", s)
}

func (a *app) indexTasksHistory(w http.ResponseWriter, r *http.Request) {
	s, err := a.clusterTaskHistorySummary(r.Context())
	if err != nil {
		log.Errorf("cluster task history summary: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	a.executeTemplate(w, "cluster_task_history", s)
}

func (a *app) indexPipelinePorep(w http.ResponseWriter, r *http.Request) {
	s, err := a.porepPipelineSummary(r.Context())
	if err != nil {
		log.Errorf("porep pipeline summary: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	a.executeTemplate(w, "pipeline_porep", s)
}

func (a *app) nodeInfo(writer http.ResponseWriter, request *http.Request) {
	params := mux.Vars(request)

	id, ok := params["id"]
	if !ok {
		http.Error(writer, "missing id", http.StatusBadRequest)
		return
	}

	intid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		http.Error(writer, "invalid id", http.StatusBadRequest)
		return
	}

	mi, err := a.clusterNodeInfo(request.Context(), intid)
	if err != nil {
		log.Errorf("machine info: %v", err)
		http.Error(writer, "internal server error", http.StatusInternalServerError)
		return
	}

	a.executePageTemplate(writer, "node_info", "Node Info", mi)
}

func (a *app) sectorInfo(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)

	id, ok := params["id"]
	if !ok {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	intid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	sp, ok := params["sp"]
	if !ok {
		http.Error(w, "missing sp", http.StatusBadRequest)
		return
	}

	maddr, err := address.NewFromString(sp)
	if err != nil {
		http.Error(w, "invalid sp", http.StatusBadRequest)
		return
	}

	spid, err := address.IDFromAddress(maddr)
	if err != nil {
		http.Error(w, "invalid sp", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	var tasks []PipelineTask

	err = a.db.Select(ctx, &tasks, `SELECT 
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
		http.Error(w, xerrors.Errorf("failed to fetch pipeline task info: %w", err).Error(), http.StatusInternalServerError)
		return
	}

	if len(tasks) == 0 {
		http.Error(w, "sector not found", http.StatusInternalServerError)
		return
	}

	head, err := a.deps.Chain.ChainHead(ctx)
	if err != nil {
		http.Error(w, xerrors.Errorf("failed to fetch chain head: %w", err).Error(), http.StatusInternalServerError)
		return
	}
	epoch := head.Height()

	mbf, err := a.getMinerBitfields(ctx, maddr, a.stor)
	if err != nil {
		http.Error(w, xerrors.Errorf("failed to load miner bitfields: %w", err).Error(), http.StatusInternalServerError)
		return
	}

	task := tasks[0]

	afterSeed := task.SeedEpoch != nil && *task.SeedEpoch <= int64(epoch)

	var sectorLocations []struct {
		CanSeal, CanStore bool
		FileType          storiface.SectorFileType `db:"sector_filetype"`
		StorageID         string                   `db:"storage_id"`
		Urls              string                   `db:"urls"`
	}

	err = a.db.Select(ctx, &sectorLocations, `SELECT p.can_seal, p.can_store, l.sector_filetype, l.storage_id, p.urls FROM sector_location l
    JOIN storage_path p ON l.storage_id = p.storage_id
    WHERE l.sector_num = $1 and l.miner_id = $2 ORDER BY p.can_seal, p.can_store, l.storage_id`, intid, spid)
	if err != nil {
		http.Error(w, xerrors.Errorf("failed to fetch sector locations: %w", err).Error(), http.StatusInternalServerError)
		return
	}

	type fileLocations struct {
		StorageID string
		Urls      []string
	}

	type locationTable struct {
		PathType        *string
		PathTypeRowSpan int

		FileType        *string
		FileTypeRowSpan int

		Locations []fileLocations
	}
	locs := []locationTable{}

	for i, loc := range sectorLocations {
		loc := loc

		urlList := strings.Split(loc.Urls, paths.URLSeparator)

		fLoc := fileLocations{
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

		locTable := locationTable{
			PathType:        pathTypeStr,
			PathTypeRowSpan: pathTypeRowSpan,
			FileType:        fileTypeStr,
			FileTypeRowSpan: fileTypeRowSpan,
			Locations:       []fileLocations{fLoc},
		}

		locs = append(locs, locTable)

	}

	// Pieces
	type sectorPieceMeta struct {
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
	var pieces []sectorPieceMeta

	err = a.db.Select(ctx, &pieces, `SELECT piece_index, piece_cid, piece_size,
       data_url, data_raw_size, data_delete_on_finalize,
       f05_publish_cid, f05_deal_id, direct_piece_activation_manifest FROM sectors_sdr_initial_pieces WHERE sp_id = $1 AND sector_number = $2`, spid, intid)
	if err != nil {
		http.Error(w, xerrors.Errorf("failed to fetch sector pieces: %w", err).Error(), http.StatusInternalServerError)
		return
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

		err = a.db.Select(ctx, &parkedPiece, `SELECT ppr.piece_id, ppr.data_url, pp.created_at, pp.complete, pp.task_id, pp.cleanup_task_id FROM parked_piece_refs ppr
		INNER JOIN parked_pieces pp ON pp.id = ppr.piece_id
		WHERE ppr.ref_id = $1`, intID)
		if err != nil {
			http.Error(w, xerrors.Errorf("failed to fetch parked piece: %w", err).Error(), http.StatusInternalServerError)
			return
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
	taskIDs := map[int64]struct{}{}
	var htasks []taskSummary
	{
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
			err = a.db.Select(ctx, &dbtasks, `SELECT t.owner_id, hm.host_and_port, t.id, t.name, t.update_time FROM harmony_task t LEFT JOIN curio.harmony_machines hm ON hm.id = t.owner_id WHERE t.id = ANY($1)`, ids)
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to fetch task names: %v", err), http.StatusInternalServerError)
				return
			}

			for _, tn := range dbtasks {
				htasks = append(htasks, taskSummary{
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

	type taskHistory struct {
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

	var th []taskHistory
	err = a.db.Select(ctx, &th, `WITH task_ids AS (
	    SELECT unnest(get_sdr_pipeline_tasks($1, $2)) AS task_id
	)
	SELECT ti.task_id pipeline_task_id, ht.name, ht.completed_by_host_and_port, ht.result, ht.err, ht.work_start, ht.work_end
	FROM task_ids ti
	         INNER JOIN harmony_task_history ht ON ti.task_id = ht.task_id`, spid, intid)
	if err != nil {
		http.Error(w, xerrors.Errorf("failed to fetch task history: %w", err).Error(), http.StatusInternalServerError)
		return
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
	err = a.db.Select(ctx, &taskState, `WITH task_ids AS (
	    SELECT unnest(get_sdr_pipeline_tasks($1, $2)) AS task_id
	)
	SELECT ti.task_id pipeline_id, ht.id harmony_task_id
	FROM task_ids ti
	         LEFT JOIN harmony_task ht ON ti.task_id = ht.id`, spid, intid)
	if err != nil {
		http.Error(w, xerrors.Errorf("failed to fetch task history: %w", err).Error(), http.StatusInternalServerError)
		return
	}

	var hasAnyStuckTask bool
	for _, ts := range taskState {
		if ts.HarmonyTaskID == nil {
			hasAnyStuckTask = true
			break
		}
	}

	mi := struct {
		SectorNumber  int64
		SpID          uint64
		PipelinePoRep sectorListEntry

		Pieces    []sectorPieceMeta
		Locations []locationTable
		Tasks     []taskSummary

		TaskHistory []taskHistory

		Resumable bool
	}{
		SectorNumber: intid,
		SpID:         spid,
		PipelinePoRep: sectorListEntry{
			PipelineTask: tasks[0],
			AfterSeed:    afterSeed,

			ChainAlloc:    must.One(mbf.alloc.IsSet(uint64(task.SectorNumber))),
			ChainSector:   must.One(mbf.sectorSet.IsSet(uint64(task.SectorNumber))),
			ChainActive:   must.One(mbf.active.IsSet(uint64(task.SectorNumber))),
			ChainUnproven: must.One(mbf.unproven.IsSet(uint64(task.SectorNumber))),
			ChainFaulty:   must.One(mbf.faulty.IsSet(uint64(task.SectorNumber))),
		},

		Pieces:      pieces,
		Locations:   locs,
		Tasks:       htasks,
		TaskHistory: th,

		Resumable: hasAnyStuckTask,
	}

	a.executePageTemplate(w, "sector_info", "Sector Info", mi)
}

func (a *app) sectorResume(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)

	id, ok := params["id"]
	if !ok {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	intid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	sp, ok := params["sp"]
	if !ok {
		http.Error(w, "missing sp", http.StatusBadRequest)
		return
	}

	maddr, err := address.NewFromString(sp)
	if err != nil {
		http.Error(w, "invalid sp", http.StatusBadRequest)
		return
	}

	spid, err := address.IDFromAddress(maddr)
	if err != nil {
		http.Error(w, "invalid sp", http.StatusBadRequest)
		return
	}

	// call CREATE OR REPLACE FUNCTION unset_task_id(sp_id_param bigint, sector_number_param bigint)

	_, err = a.db.Exec(r.Context(), `SELECT unset_task_id($1, $2)`, spid, intid)
	if err != nil {
		http.Error(w, xerrors.Errorf("failed to resume sector: %w", err).Error(), http.StatusInternalServerError)
		return
	}

	// redir back to /hapi/sector/{sp}/{id}

	http.Redirect(w, r, fmt.Sprintf("/hapi/sector/%s/%d", sp, intid), http.StatusSeeOther)
}

func (a *app) sectorRemove(w http.ResponseWriter, r *http.Request) {
	params := mux.Vars(r)

	id, ok := params["id"]
	if !ok {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	intid, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	sp, ok := params["sp"]
	if !ok {
		http.Error(w, "missing sp", http.StatusBadRequest)
		return
	}

	maddr, err := address.NewFromString(sp)
	if err != nil {
		http.Error(w, "invalid sp", http.StatusBadRequest)
		return
	}

	spid, err := address.IDFromAddress(maddr)
	if err != nil {
		http.Error(w, "invalid sp", http.StatusBadRequest)
		return
	}

	_, err = a.db.Exec(r.Context(), `DELETE FROM sectors_sdr_pipeline WHERE sp_id = $1 AND sector_number = $2`, spid, intid)
	if err != nil {
		http.Error(w, xerrors.Errorf("failed to resume sector: %w", err).Error(), http.StatusInternalServerError)
		return
	}

	_, err = a.db.Exec(r.Context(), `INSERT INTO storage_removal_marks (sp_id, sector_num, sector_filetype, storage_id, created_at, approved, approved_at)
		SELECT miner_id, sector_num, sector_filetype, storage_id, current_timestamp, FALSE, NULL FROM sector_location
		WHERE miner_id = $1 AND sector_num = $2`, spid, intid)
	if err != nil {
		http.Error(w, xerrors.Errorf("failed to mark sector for removal: %w", err).Error(), http.StatusInternalServerError)
		return
	}

	// redir back to /pipeline_porep.html

	http.Redirect(w, r, "/pipeline_porep.html", http.StatusSeeOther)
}

var templateDev = os.Getenv("CURIO_WEB_DEV") == "1"

func (a *app) executeTemplate(w http.ResponseWriter, name string, data interface{}) {
	if templateDev {
		fs := os.DirFS("./curiosrc/web/hapi/web")
		a.t = template.Must(makeTemplate().ParseFS(fs, "*"))
	}
	if err := a.t.ExecuteTemplate(w, name, data); err != nil {
		log.Errorf("execute template %s: %v", name, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

func (a *app) executePageTemplate(w http.ResponseWriter, name, title string, data interface{}) {
	if templateDev {
		fs := os.DirFS("./curiosrc/web/hapi/web")
		a.t = template.Must(makeTemplate().ParseFS(fs, "*"))
	}
	var contentBuf bytes.Buffer
	if err := a.t.ExecuteTemplate(&contentBuf, name, data); err != nil {
		log.Errorf("execute template %s: %v", name, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
	a.executeTemplate(w, "root", map[string]interface{}{
		"PageTitle": title,
		"Content":   contentBuf.String(),
	})
}

type machineSummary struct {
	Address      string
	ID           int64
	Name         string
	SinceContact string

	Tasks        string
	Cpu          int
	RamHumanized string
	Gpu          int
	Layers       string
}

type taskSummary struct {
	Name           string
	SincePosted    string
	Owner, OwnerID *string
	ID             int64
}

type taskHistorySummary struct {
	Name   string
	TaskID int64

	Posted, Start, Queued, Took string

	Result bool
	Err    string

	CompletedBy string
}

func (a *app) clusterMachineSummary(ctx context.Context) ([]machineSummary, error) {
	// Then machine summary
	rows, err := a.db.Query(ctx, `
						SELECT 
							hm.id,
							hm.host_and_port,
							CURRENT_TIMESTAMP - hm.last_contact AS last_contact,
							hm.cpu,
							hm.ram,
							hm.gpu,
							hmd.machine_name,
							hmd.tasks,
							hmd.layers
						FROM 
							harmony_machines hm
						LEFT JOIN 
							harmony_machine_details hmd ON hm.id = hmd.machine_id
						ORDER BY 
							hmd.machine_name ASC;`)
	if err != nil {
		return nil, err // Handle error
	}
	defer rows.Close()

	var summaries []machineSummary
	for rows.Next() {
		var m machineSummary
		var lastContact time.Duration
		var ram int64

		if err := rows.Scan(&m.ID, &m.Address, &lastContact, &m.Cpu, &ram, &m.Gpu, &m.Name, &m.Tasks, &m.Layers); err != nil {
			return nil, err // Handle error
		}
		m.SinceContact = lastContact.Round(time.Second).String()
		m.RamHumanized = humanize.Bytes(uint64(ram))
		m.Tasks = strings.TrimSuffix(strings.TrimPrefix(m.Tasks, ","), ",")
		m.Layers = strings.TrimSuffix(strings.TrimPrefix(m.Layers, ","), ",")

		summaries = append(summaries, m)
	}
	return summaries, nil
}

func (a *app) clusterTaskHistorySummary(ctx context.Context) ([]taskHistorySummary, error) {
	rows, err := a.db.Query(ctx, "SELECT id, name, task_id, posted, work_start, work_end, result, err, completed_by_host_and_port FROM harmony_task_history ORDER BY work_end DESC LIMIT 15")
	if err != nil {
		return nil, err // Handle error
	}
	defer rows.Close()

	var summaries []taskHistorySummary
	for rows.Next() {
		var t taskHistorySummary
		var posted, start, end time.Time

		if err := rows.Scan(&t.TaskID, &t.Name, &t.TaskID, &posted, &start, &end, &t.Result, &t.Err, &t.CompletedBy); err != nil {
			return nil, err // Handle error
		}

		t.Posted = posted.Local().Round(time.Second).Format("02 Jan 06 15:04")
		t.Start = start.Local().Round(time.Second).Format("02 Jan 06 15:04")
		//t.End = end.Local().Round(time.Second).Format("02 Jan 06 15:04")

		t.Queued = start.Sub(posted).Round(time.Second).String()
		if t.Queued == "0s" {
			t.Queued = start.Sub(posted).Round(time.Millisecond).String()
		}

		t.Took = end.Sub(start).Round(time.Second).String()
		if t.Took == "0s" {
			t.Took = end.Sub(start).Round(time.Millisecond).String()
		}

		summaries = append(summaries, t)
	}
	return summaries, nil
}

type porepPipelineSummary struct {
	Actor string

	CountSDR          int
	CountTrees        int
	CountPrecommitMsg int
	CountWaitSeed     int
	CountPoRep        int
	CountCommitMsg    int
	CountDone         int
	CountFailed       int
}

func (a *app) porepPipelineSummary(ctx context.Context) ([]porepPipelineSummary, error) {

	head, err := a.deps.Chain.ChainHead(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := a.db.Query(ctx, `
	SELECT 
		sp_id,
		COUNT(*) FILTER (WHERE after_sdr = false) as CountSDR,
		COUNT(*) FILTER (WHERE (after_tree_d = false OR after_tree_c = false OR after_tree_r = false) AND after_sdr = true) as CountTrees,
		COUNT(*) FILTER (WHERE after_tree_r = true and after_precommit_msg = false) as CountPrecommitMsg,
		COUNT(*) FILTER (WHERE after_precommit_msg_success = true AND seed_epoch > $1) as CountWaitSeed,
		COUNT(*) FILTER (WHERE after_porep = false AND after_precommit_msg_success = true) as CountPoRep,
		COUNT(*) FILTER (WHERE after_commit_msg_success = false AND after_porep = true) as CountCommitMsg,
		COUNT(*) FILTER (WHERE after_commit_msg_success = true) as CountDone,
		COUNT(*) FILTER (WHERE failed = true) as CountFailed
	FROM 
		sectors_sdr_pipeline
	GROUP BY sp_id`, head.Height())
	if err != nil {
		return nil, xerrors.Errorf("query: %w", err)
	}
	defer rows.Close()

	var summaries []porepPipelineSummary
	for rows.Next() {
		var summary porepPipelineSummary
		var actor int64
		if err := rows.Scan(&actor, &summary.CountSDR, &summary.CountTrees, &summary.CountPrecommitMsg, &summary.CountWaitSeed, &summary.CountPoRep, &summary.CountCommitMsg, &summary.CountDone, &summary.CountFailed); err != nil {
			return nil, xerrors.Errorf("scan: %w", err)
		}

		sactor, err := address.NewIDAddress(uint64(actor))
		if err != nil {
			return nil, xerrors.Errorf("failed to create actor address: %w", err)
		}

		summary.Actor = sactor.String()

		summaries = append(summaries, summary)
	}
	return summaries, nil
}

type machineInfo struct {
	Info struct {
		Name        string
		Host        string
		ID          int64
		LastContact string
		CPU         int64
		Memory      int64
		GPU         int64
	}

	// Storage
	Storage []struct {
		ID            string
		Weight        int64
		MaxStorage    int64
		CanSeal       bool
		CanStore      bool
		Groups        string
		AllowTo       string
		AllowTypes    string
		DenyTypes     string
		Capacity      int64
		Available     int64
		FSAvailable   int64
		Reserved      int64
		Used          int64
		AllowMiners   string
		DenyMiners    string
		LastHeartbeat time.Time
		HeartbeatErr  *string

		UsedPercent     float64
		ReservedPercent float64
	}

	/*TotalStorage struct {
		MaxStorage  int64
		UsedStorage int64

		MaxSealStorage  int64
		UsedSealStorage int64

		MaxStoreStorage  int64
		UsedStoreStorage int64
	}*/

	// Tasks
	RunningTasks []struct {
		ID     int64
		Task   string
		Posted string

		PoRepSector, PoRepSectorSP *int64
	}

	FinishedTasks []struct {
		ID      int64
		Task    string
		Posted  string
		Start   string
		Queued  string
		Took    string
		Outcome string
		Message string
	}
}

func (a *app) clusterNodeInfo(ctx context.Context, id int64) (*machineInfo, error) {
	rows, err := a.db.Query(ctx, `
						SELECT 
							hm.id,
							hm.host_and_port,
							hm.last_contact,
							hm.cpu,
							hm.ram,
							hm.gpu,
							hmd.machine_name
						FROM 
							harmony_machines hm
						LEFT JOIN 
							harmony_machine_details hmd ON hm.id = hmd.machine_id 
						WHERE 
						    hm.id=$1
						ORDER BY 
							hmd.machine_name ASC;
						`, id)
	if err != nil {
		return nil, err // Handle error
	}
	defer rows.Close()

	var summaries []machineInfo
	if rows.Next() {
		var m machineInfo
		var lastContact time.Time

		if err := rows.Scan(&m.Info.ID, &m.Info.Host, &lastContact, &m.Info.CPU, &m.Info.Memory, &m.Info.GPU, &m.Info.Name); err != nil {
			return nil, err
		}

		m.Info.LastContact = time.Since(lastContact).Round(time.Second).String()

		summaries = append(summaries, m)
	}

	if len(summaries) == 0 {
		return nil, xerrors.Errorf("machine not found")
	}

	// query storage info
	rows2, err := a.db.Query(ctx, "SELECT storage_id, weight, max_storage, can_seal, can_store, groups, allow_to, allow_types, deny_types, capacity, available, fs_available, reserved, used, allow_miners, deny_miners, last_heartbeat, heartbeat_err FROM storage_path WHERE urls LIKE '%' || $1 || '%'", summaries[0].Info.Host)
	if err != nil {
		return nil, err
	}

	defer rows2.Close()

	for rows2.Next() {
		var s struct {
			ID            string
			Weight        int64
			MaxStorage    int64
			CanSeal       bool
			CanStore      bool
			Groups        string
			AllowTo       string
			AllowTypes    string
			DenyTypes     string
			Capacity      int64
			Available     int64
			FSAvailable   int64
			Reserved      int64
			Used          int64
			AllowMiners   string
			DenyMiners    string
			LastHeartbeat time.Time
			HeartbeatErr  *string

			UsedPercent     float64
			ReservedPercent float64
		}
		if err := rows2.Scan(&s.ID, &s.Weight, &s.MaxStorage, &s.CanSeal, &s.CanStore, &s.Groups, &s.AllowTo, &s.AllowTypes, &s.DenyTypes, &s.Capacity, &s.Available, &s.FSAvailable, &s.Reserved, &s.Used, &s.AllowMiners, &s.DenyMiners, &s.LastHeartbeat, &s.HeartbeatErr); err != nil {
			return nil, err
		}

		s.UsedPercent = float64(s.Capacity-s.FSAvailable) * 100 / float64(s.Capacity)
		//s.ReservedPercent = float64(s.Capacity-(s.FSAvailable+s.Reserved))*100/float64(s.Capacity) - s.UsedPercent
		s.ReservedPercent = float64(s.Reserved) * 100 / float64(s.Capacity)

		summaries[0].Storage = append(summaries[0].Storage, s)
	}

	// tasks
	rows3, err := a.db.Query(ctx, "SELECT id, name, posted_time FROM harmony_task WHERE owner_id=$1", summaries[0].Info.ID)
	if err != nil {
		return nil, err
	}

	defer rows3.Close()

	for rows3.Next() {
		var t struct {
			ID     int64
			Task   string
			Posted string

			PoRepSector   *int64
			PoRepSectorSP *int64
		}

		var posted time.Time
		if err := rows3.Scan(&t.ID, &t.Task, &posted); err != nil {
			return nil, err
		}
		t.Posted = time.Since(posted).Round(time.Second).String()

		{
			// try to find in the porep pipeline
			rows4, err := a.db.Query(ctx, `SELECT sp_id, sector_number FROM sectors_sdr_pipeline 
            	WHERE task_id_sdr=$1
								OR task_id_tree_d=$1
								OR task_id_tree_c=$1
								OR task_id_tree_r=$1
								OR task_id_precommit_msg=$1
								OR task_id_porep=$1	
								OR task_id_commit_msg=$1
								OR task_id_finalize=$1
								OR task_id_move_storage=$1
            	    `, t.ID)
			if err != nil {
				return nil, err
			}

			if rows4.Next() {
				var spid int64
				var sector int64
				if err := rows4.Scan(&spid, &sector); err != nil {
					return nil, err
				}
				t.PoRepSector = &sector
				t.PoRepSectorSP = &spid
			}

			rows4.Close()
		}

		summaries[0].RunningTasks = append(summaries[0].RunningTasks, t)
	}

	rows5, err := a.db.Query(ctx, `SELECT name, task_id, posted, work_start, work_end, result, err FROM harmony_task_history WHERE completed_by_host_and_port = $1 ORDER BY work_end DESC LIMIT 15`, summaries[0].Info.Host)
	if err != nil {
		return nil, err
	}
	defer rows5.Close()

	for rows5.Next() {
		var ft struct {
			ID      int64
			Task    string
			Posted  string
			Start   string
			Queued  string
			Took    string
			Outcome string

			Message string
		}

		var posted, start, end time.Time
		var result bool
		if err := rows5.Scan(&ft.Task, &ft.ID, &posted, &start, &end, &result, &ft.Message); err != nil {
			return nil, err
		}

		ft.Outcome = "Success"
		if !result {
			ft.Outcome = "Failed"
		}

		// Format the times and durations
		ft.Posted = posted.Format("02 Jan 06 15:04 MST")
		ft.Start = start.Format("02 Jan 06 15:04 MST")
		ft.Queued = start.Sub(posted).Round(time.Second).String()
		ft.Took = end.Sub(start).Round(time.Second).String()

		summaries[0].FinishedTasks = append(summaries[0].FinishedTasks, ft)
	}

	return &summaries[0], nil
}
