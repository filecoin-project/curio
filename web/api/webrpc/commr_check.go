package webrpc

import (
	"context"
	"database/sql"
	"time"

	"github.com/filecoin-project/go-address"
	"golang.org/x/xerrors"
)

// CommRCheckResult contains the result of a CommR (sealed/update) data check
type CommRCheckResult struct {
	CheckID      int64  `json:"check_id"`
	SpID         int64  `json:"sp_id"`
	SectorNumber int64  `json:"sector_number"`
	TaskID       *int64 `json:"task_id,omitempty"`
	FileType     string `json:"file_type"`
	ExpectedCID  string `json:"expected_comm_r"`
	ActualCID    string `json:"actual_comm_r,omitempty"`
	OK           *bool  `json:"ok,omitempty"`
	Message      string `json:"message,omitempty"`
	CreateTime   string `json:"create_time,omitempty"`
	Complete     bool   `json:"complete"`
	Error        string `json:"error,omitempty"`
}

// SectorCommRCheckStart starts a CommR check for a sector
// fileType is either "sealed" or "update"
func (a *WebRPC) SectorCommRCheckStart(ctx context.Context, spStr string, sectorNum int64, fileType string) (*CommRCheckResult, error) {
	maddr, err := address.NewFromString(spStr)
	if err != nil {
		return nil, xerrors.Errorf("invalid miner address: %w", err)
	}

	spID, err := address.IDFromAddress(maddr)
	if err != nil {
		return nil, xerrors.Errorf("getting miner ID: %w", err)
	}

	// Validate file type
	if fileType != "sealed" && fileType != "update" {
		return nil, xerrors.Errorf("invalid file type: must be 'sealed' or 'update'")
	}

	result := &CommRCheckResult{
		SpID:         int64(spID),
		SectorNumber: sectorNum,
		FileType:     fileType,
	}

	// Get expected CommR from sectors_meta
	// For "sealed" file type: use orig_sealed_cid (original sealed file CommR)
	// For "update" file type: use cur_sealed_cid (snap-upgrade update CommR)
	var expectedCID string
	if fileType == "sealed" {
		// For sealed files, we need the original CommR (before any snap upgrades)
		// orig_sealed_cid is set for snap-upgraded sectors, cur_sealed_cid for non-upgraded
		err = a.deps.DB.QueryRow(ctx, `
			SELECT COALESCE(orig_sealed_cid, cur_sealed_cid) FROM sectors_meta WHERE sp_id = $1 AND sector_num = $2
		`, spID, sectorNum).Scan(&expectedCID)
	} else {
		// For update files, we need the current CommR (the snap-upgrade CommR)
		err = a.deps.DB.QueryRow(ctx, `
			SELECT cur_sealed_cid FROM sectors_meta WHERE sp_id = $1 AND sector_num = $2
		`, spID, sectorNum).Scan(&expectedCID)
	}
	if err != nil {
		return nil, xerrors.Errorf("getting expected CommR from sectors_meta: %w", err)
	}
	result.ExpectedCID = expectedCID

	// Create the check task
	var checkID int64
	err = a.deps.DB.QueryRow(ctx, `
		INSERT INTO scrub_commr_check (sp_id, sector_number, file_type, expected_comm_r)
		VALUES ($1, $2, $3, $4)
		RETURNING check_id
	`, spID, sectorNum, fileType, expectedCID).Scan(&checkID)
	if err != nil {
		return nil, xerrors.Errorf("creating check task: %w", err)
	}

	result.CheckID = checkID
	return result, nil
}

// SectorCommRCheckStatus checks the status of a CommR check
func (a *WebRPC) SectorCommRCheckStatus(ctx context.Context, checkID int64) (*CommRCheckResult, error) {
	var row struct {
		SpID         int64          `db:"sp_id"`
		SectorNumber int64          `db:"sector_number"`
		CreateTime   time.Time      `db:"create_time"`
		TaskID       sql.NullInt64  `db:"task_id"`
		FileType     string         `db:"file_type"`
		ExpectedCID  string         `db:"expected_comm_r"`
		OK           sql.NullBool   `db:"ok"`
		ActualCID    sql.NullString `db:"actual_comm_r"`
		Message      sql.NullString `db:"message"`
	}

	err := a.deps.DB.QueryRow(ctx, `
		SELECT sp_id, sector_number, create_time, task_id, file_type, expected_comm_r, ok, actual_comm_r, message
		FROM scrub_commr_check
		WHERE check_id = $1
	`, checkID).Scan(&row.SpID, &row.SectorNumber, &row.CreateTime, &row.TaskID, &row.FileType, &row.ExpectedCID, &row.OK, &row.ActualCID, &row.Message)
	if err != nil {
		return nil, xerrors.Errorf("querying check status: %w", err)
	}

	result := &CommRCheckResult{
		CheckID:      checkID,
		SpID:         row.SpID,
		SectorNumber: row.SectorNumber,
		FileType:     row.FileType,
		ExpectedCID:  row.ExpectedCID,
		CreateTime:   row.CreateTime.Format(time.RFC3339),
		Complete:     row.OK.Valid,
	}

	if row.TaskID.Valid {
		result.TaskID = &row.TaskID.Int64
	}
	if row.OK.Valid {
		result.OK = &row.OK.Bool
	}
	if row.ActualCID.Valid {
		result.ActualCID = row.ActualCID.String
	}
	if row.Message.Valid {
		result.Message = row.Message.String
	}

	return result, nil
}

// SectorCommRCheckList lists recent CommR checks for a sector
func (a *WebRPC) SectorCommRCheckList(ctx context.Context, spStr string, sectorNum int64) ([]*CommRCheckResult, error) {
	maddr, err := address.NewFromString(spStr)
	if err != nil {
		return nil, xerrors.Errorf("invalid miner address: %w", err)
	}

	spID, err := address.IDFromAddress(maddr)
	if err != nil {
		return nil, xerrors.Errorf("getting miner ID: %w", err)
	}

	var rows []struct {
		CheckID     int64          `db:"check_id"`
		CreateTime  time.Time      `db:"create_time"`
		TaskID      sql.NullInt64  `db:"task_id"`
		FileType    string         `db:"file_type"`
		ExpectedCID string         `db:"expected_comm_r"`
		OK          sql.NullBool   `db:"ok"`
		ActualCID   sql.NullString `db:"actual_comm_r"`
		Message     sql.NullString `db:"message"`
	}

	err = a.deps.DB.Select(ctx, &rows, `
		SELECT check_id, create_time, task_id, file_type, expected_comm_r, ok, actual_comm_r, message
		FROM scrub_commr_check
		WHERE sp_id = $1 AND sector_number = $2
		ORDER BY create_time DESC
		LIMIT 10
	`, spID, sectorNum)
	if err != nil {
		return nil, xerrors.Errorf("querying check list: %w", err)
	}

	results := make([]*CommRCheckResult, len(rows))
	for i, row := range rows {
		result := &CommRCheckResult{
			CheckID:      row.CheckID,
			SpID:         int64(spID),
			SectorNumber: sectorNum,
			FileType:     row.FileType,
			ExpectedCID:  row.ExpectedCID,
			CreateTime:   row.CreateTime.Format(time.RFC3339),
			Complete:     row.OK.Valid,
		}
		if row.TaskID.Valid {
			result.TaskID = &row.TaskID.Int64
		}
		if row.OK.Valid {
			result.OK = &row.OK.Bool
		}
		if row.ActualCID.Valid {
			result.ActualCID = row.ActualCID.String
		}
		if row.Message.Valid {
			result.Message = row.Message.String
		}
		results[i] = result
	}

	return results, nil
}
