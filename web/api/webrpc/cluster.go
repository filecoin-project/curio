package webrpc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
)

type MachineSummary struct {
	Address      string
	ID           int64
	Name         string
	SinceContact string

	Tasks          string
	Cpu            int
	RamHumanized   string
	Gpu            int
	Layers         string
	Uptime         string
	Unschedulable  bool
	RestartRequest string
	Restarting     bool
	RunningTasks   int
}

func (a *WebRPC) ClusterMachines(ctx context.Context) ([]MachineSummary, error) {
	// Then machine summary
	rows, err := a.deps.DB.Query(ctx, `
						SELECT 
							hm.id,
							hm.host_and_port,
							CURRENT_TIMESTAMP - hm.last_contact AS last_contact,
							hm.cpu,
							hm.ram,
							hm.gpu,
							hm.unschedulable,
							hm.restart_request,
							hmd.machine_name,
							hmd.tasks,
							hmd.layers,
							hmd.startup_time
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

	var summaries []MachineSummary
	for rows.Next() {
		var m MachineSummary
		var lastContact time.Duration
		var ram int64
		var uptime time.Time
		var restartRequest *time.Time

		if err := rows.Scan(&m.ID, &m.Address, &lastContact, &m.Cpu, &ram, &m.Gpu, &m.Unschedulable, &restartRequest, &m.Name, &m.Tasks, &m.Layers, &uptime); err != nil {
			return nil, err // Handle error
		}
		m.SinceContact = lastContact.Round(time.Second).String()
		m.RamHumanized = humanize.Bytes(uint64(ram))
		m.Uptime = humanize.Time(uptime)
		m.Tasks = strings.TrimSuffix(strings.TrimPrefix(m.Tasks, ","), ",")
		m.Layers = strings.TrimSuffix(strings.TrimPrefix(m.Layers, ","), ",")

		if m.Unschedulable {
			var runningTasks int
			if err := a.deps.DB.QueryRow(ctx, "SELECT COUNT(*) FROM harmony_task WHERE owner_id=$1", m.ID).Scan(&runningTasks); err != nil {
				return nil, err
			}
			m.RunningTasks = runningTasks
		}

		if restartRequest != nil {
			m.RestartRequest = humanize.Time(*restartRequest)
			m.Restarting = true
		}

		summaries = append(summaries, m)
	}
	return summaries, nil
}

type TaskHistorySummary struct {
	Name   string
	TaskID int64

	Posted, Start, Queued, Took string

	Result bool
	Err    string

	CompletedBy string
}

func (a *WebRPC) ClusterTaskHistory(ctx context.Context, limit, offset int) ([]TaskHistorySummary, error) {
	rows, err := a.deps.DB.Query(ctx, "SELECT name, task_id, posted, work_start, work_end, result, err, completed_by_host_and_port FROM harmony_task_history ORDER BY work_end DESC LIMIT $1 OFFSET $2", limit, offset)
	if err != nil {
		return nil, err // Handle error
	}
	defer rows.Close()

	var summaries []TaskHistorySummary
	for rows.Next() {
		var t TaskHistorySummary
		var posted, start, end time.Time

		if err := rows.Scan(&t.Name, &t.TaskID, &posted, &start, &end, &t.Result, &t.Err, &t.CompletedBy); err != nil {
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

type MachineInfo struct {
	Info struct {
		Name           string
		Host           string
		ID             int64
		LastContact    string
		CPU            int64
		Memory         int64
		GPU            float64
		Layers         string
		Unschedulable  bool
		RunningTasks   int
		Tasks          string
		Miners         string
		StartupTime    *time.Time
		RestartRequest *time.Time
	}

	// Storage
	Storage []struct {
		ID            string
		URLs          string
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

	// Storage URL Liveness
	StorageURLs []struct {
		StorageID      string
		URL            string
		LastChecked    time.Time
		LastLive       *time.Time
		LastDead       *time.Time
		LastDeadReason *string
	}

	// Tasks
	RunningTasks []struct {
		ID           int64
		Task         string
		Posted       string
		UpdateTime   string
		InitiatedBy  *int64
		AddedBy      int64
		PreviousTask *int64
		Retries      int64

		PoRepSector, PoRepSectorSP *int64
		PoRepSectorMiner           string
	}

	FinishedTasks []struct {
		ID      int64
		TaskID  int64
		Task    string
		Posted  string
		Start   string
		End     string
		Queued  string
		Took    string
		Result  bool
		Outcome string
		Message string
	}
}

func (a *WebRPC) ClusterNodeInfo(ctx context.Context, id int64) (*MachineInfo, error) {
	rows, err := a.deps.DB.Query(ctx, `
						SELECT 
							hm.id,
							hm.host_and_port,
							hm.last_contact,
							hm.cpu,
							hm.ram,
							hm.gpu,
							hm.unschedulable,
							hm.restart_request,
							hmd.machine_name,
							hmd.layers,
							hmd.tasks,
							hmd.miners,
							hmd.startup_time
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

	var summaries []MachineInfo
	if rows.Next() {
		var m MachineInfo
		var lastContact time.Time

		if err := rows.Scan(&m.Info.ID, &m.Info.Host, &lastContact, &m.Info.CPU, &m.Info.Memory, &m.Info.GPU, &m.Info.Unschedulable, &m.Info.RestartRequest, &m.Info.Name, &m.Info.Layers, &m.Info.Tasks, &m.Info.Miners, &m.Info.StartupTime); err != nil {
			return nil, err
		}

		m.Info.LastContact = time.Since(lastContact).Round(time.Second).String()

		summaries = append(summaries, m)
	}

	if len(summaries) == 0 {
		return nil, xerrors.Errorf("machine not found")
	}

	// query storage info
	rows2, err := a.deps.DB.Query(ctx, "SELECT storage_id, urls, weight, max_storage, can_seal, can_store, groups, allow_to, allow_types, deny_types, capacity, available, fs_available, reserved, used, allow_miners, deny_miners, last_heartbeat, heartbeat_err FROM storage_path WHERE urls LIKE '%' || $1 || '%'", summaries[0].Info.Host)
	if err != nil {
		return nil, err
	}

	defer rows2.Close()

	for rows2.Next() {
		var s struct {
			ID            string
			URLs          string
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
		if err := rows2.Scan(&s.ID, &s.URLs, &s.Weight, &s.MaxStorage, &s.CanSeal, &s.CanStore, &s.Groups, &s.AllowTo, &s.AllowTypes, &s.DenyTypes, &s.Capacity, &s.Available, &s.FSAvailable, &s.Reserved, &s.Used, &s.AllowMiners, &s.DenyMiners, &s.LastHeartbeat, &s.HeartbeatErr); err != nil {
			return nil, err
		}

		s.UsedPercent = float64(s.Capacity-s.FSAvailable) * 100 / float64(s.Capacity)
		//s.ReservedPercent = float64(s.Capacity-(s.FSAvailable+s.Reserved))*100/float64(s.Capacity) - s.UsedPercent
		s.ReservedPercent = float64(s.Reserved) * 100 / float64(s.Capacity)

		summaries[0].Storage = append(summaries[0].Storage, s)
	}

	// query storage URL liveness
	rowsURL, err := a.deps.DB.Query(ctx, `SELECT sp.storage_id, spul.url, spul.last_checked, spul.last_live, spul.last_dead, spul.last_dead_reason 
		FROM storage_path sp 
		INNER JOIN sector_path_url_liveness spul ON sp.storage_id = spul.storage_id 
		WHERE sp.urls LIKE '%' || $1 || '%'`, summaries[0].Info.Host)
	if err != nil {
		return nil, err
	}
	defer rowsURL.Close()

	for rowsURL.Next() {
		var sul struct {
			StorageID      string
			URL            string
			LastChecked    time.Time
			LastLive       *time.Time
			LastDead       *time.Time
			LastDeadReason *string
		}
		if err := rowsURL.Scan(&sul.StorageID, &sul.URL, &sul.LastChecked, &sul.LastLive, &sul.LastDead, &sul.LastDeadReason); err != nil {
			return nil, err
		}
		summaries[0].StorageURLs = append(summaries[0].StorageURLs, sul)
	}

	// tasks
	rows3, err := a.deps.DB.Query(ctx, "SELECT id, name, posted_time, update_time, initiated_by, added_by, previous_task, retries FROM harmony_task WHERE owner_id=$1", summaries[0].Info.ID)
	if err != nil {
		return nil, err
	}

	defer rows3.Close()

	for rows3.Next() {
		var t struct {
			ID           int64
			Task         string
			Posted       string
			UpdateTime   string
			InitiatedBy  *int64
			AddedBy      int64
			PreviousTask *int64
			Retries      int64

			PoRepSector      *int64
			PoRepSectorSP    *int64
			PoRepSectorMiner string
		}

		var posted, updateTime time.Time
		if err := rows3.Scan(&t.ID, &t.Task, &posted, &updateTime, &t.InitiatedBy, &t.AddedBy, &t.PreviousTask, &t.Retries); err != nil {
			return nil, err
		}
		t.Posted = time.Since(posted).Round(time.Second).String()
		t.UpdateTime = time.Since(updateTime).Round(time.Second).String()

		{
			// try to find in the porep pipeline
			rows4, err := a.deps.DB.Query(ctx, `SELECT sp_id, sector_number FROM sectors_sdr_pipeline 
            	WHERE task_id_sdr=$1
				OR task_id_tree_d=$1
				OR task_id_tree_c=$1
				OR task_id_tree_r=$1
				OR task_id_synth=$1
				OR task_id_precommit_msg=$1
				OR task_id_porep=$1
				OR task_id_commit_msg=$1
				OR task_id_finalize=$1
				OR task_id_move_storage=$1`, t.ID)
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
				maddr, err := address.NewIDAddress(uint64(spid))
				if err != nil {
					return nil, err
				}
				t.PoRepSectorMiner = maddr.String()
			}

			rows4.Close()
		}

		summaries[0].RunningTasks = append(summaries[0].RunningTasks, t)
		summaries[0].Info.RunningTasks++
	}

	rows5, err := a.deps.DB.Query(ctx, `SELECT id, name, task_id, posted, work_start, work_end, result, err FROM harmony_task_history WHERE completed_by_host_and_port = $1 ORDER BY work_end DESC LIMIT 15`, summaries[0].Info.Host)
	if err != nil {
		return nil, err
	}
	defer rows5.Close()

	for rows5.Next() {
		var ft struct {
			ID      int64
			TaskID  int64
			Task    string
			Posted  string
			Start   string
			End     string
			Queued  string
			Took    string
			Result  bool
			Outcome string
			Message string
		}

		var posted, start, end time.Time
		if err := rows5.Scan(&ft.ID, &ft.Task, &ft.TaskID, &posted, &start, &end, &ft.Result, &ft.Message); err != nil {
			return nil, err
		}

		ft.Outcome = "Success"
		if !ft.Result {
			ft.Outcome = "Failed"
		}

		// Format the times and durations
		ft.Posted = posted.Format("02 Jan 06 15:04 MST")
		ft.Start = start.Format("02 Jan 06 15:04 MST")
		ft.End = end.Format("02 Jan 06 15:04 MST")
		ft.Queued = start.Sub(posted).Round(time.Second).String()
		ft.Took = end.Sub(start).Round(time.Second).String()

		summaries[0].FinishedTasks = append(summaries[0].FinishedTasks, ft)
	}

	return &summaries[0], nil
}

func (a *WebRPC) Cordon(ctx context.Context, id int64) error {
	_, err := a.deps.DB.Exec(ctx, `UPDATE harmony_machines SET unschedulable = $1 WHERE id = $2`, true, id)
	if err != nil {
		return xerrors.Errorf("cordon failed: %w", err)
	}
	return nil
}

func (a *WebRPC) Uncordon(ctx context.Context, id int64) error {
	_, err := a.deps.DB.Exec(ctx, `UPDATE harmony_machines SET unschedulable = $1 WHERE id = $2`, false, id)
	if err != nil {
		return xerrors.Errorf("uncordon failed: %w", err)
	}
	return nil
}

func (a *WebRPC) Restart(ctx context.Context, id int64) error {
	_, err := a.deps.DB.Exec(ctx, `UPDATE harmony_machines SET restart_request = NOW() WHERE id = $1`, id)
	if err != nil {
		return xerrors.Errorf("restart failed: %w", err)
	}
	return nil
}

func (a *WebRPC) AbortRestart(ctx context.Context, id int64) error {
	_, err := a.deps.DB.Exec(ctx, `UPDATE harmony_machines SET restart_request = NULL WHERE id = $1`, id)
	if err != nil {
		return xerrors.Errorf("abort restart failed: %w", err)
	}
	return nil
}

func (a *WebRPC) ClusterNodeMetrics(ctx context.Context, id int64) (string, error) {
	var hostPort string
	err := a.deps.DB.QueryRow(ctx, `SELECT host_and_port FROM harmony_machines WHERE id = $1`, id).Scan(&hostPort)
	if err != nil {
		return "", xerrors.Errorf("failed to get host_and_port for machine %d: %w", id, err)
	}

	url := fmt.Sprintf("http://%s/debug/metrics", hostPort)

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, "GET", url, nil)
	if err != nil {
		return "", xerrors.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", xerrors.Errorf("failed to fetch metrics from %s: %w", url, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return "", xerrors.Errorf("metrics endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", xerrors.Errorf("failed to read metrics response: %w", err)
	}

	return string(body), nil
}
