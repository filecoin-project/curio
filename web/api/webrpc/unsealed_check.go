package webrpc

import (
	"context"
	"database/sql"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/lib/dealdata"
)

// UnsealedCheckResult contains the result of an unsealed data check
type UnsealedCheckResult struct {
	CheckID       int64  `json:"check_id"`
	SpID          int64  `json:"sp_id"`
	SectorNumber  int64  `json:"sector_number"`
	TaskID        *int64 `json:"task_id,omitempty"`
	ExpectedCommD string `json:"expected_commd"`
	ActualCommD   string `json:"actual_commd,omitempty"`
	OK            *bool  `json:"ok,omitempty"`
	Message       string `json:"message,omitempty"`
	CreateTime    string `json:"create_time,omitempty"`
	Complete      bool   `json:"complete"`
	Error         string `json:"error,omitempty"`
}

// SectorUnsealedCheckStart starts an unsealed data check for a sector
func (a *WebRPC) SectorUnsealedCheckStart(ctx context.Context, spStr string, sectorNum int64) (*UnsealedCheckResult, error) {
	maddr, err := address.NewFromString(spStr)
	if err != nil {
		return nil, xerrors.Errorf("invalid miner address: %w", err)
	}

	spID, err := address.IDFromAddress(maddr)
	if err != nil {
		return nil, xerrors.Errorf("getting miner ID: %w", err)
	}

	result := &UnsealedCheckResult{
		SpID:         int64(spID),
		SectorNumber: sectorNum,
	}

	// Get expected unsealed CID from pieces
	unsealedCid, err := dealdata.UnsealedCidFromPieces(ctx, a.deps.DB, int64(spID), sectorNum)
	if err != nil {
		return nil, xerrors.Errorf("getting expected unsealed CID: %w", err)
	}
	result.ExpectedCommD = unsealedCid.String()

	// Create the check task
	var checkID int64
	err = a.deps.DB.QueryRow(ctx, `
		INSERT INTO scrub_unseal_commd_check (sp_id, sector_number, expected_unsealed_cid)
		VALUES ($1, $2, $3)
		RETURNING check_id
	`, spID, sectorNum, unsealedCid.String()).Scan(&checkID)
	if err != nil {
		return nil, xerrors.Errorf("creating check task: %w", err)
	}

	result.CheckID = checkID
	return result, nil
}

// SectorUnsealedCheckStatus checks the status of an unsealed data check
func (a *WebRPC) SectorUnsealedCheckStatus(ctx context.Context, checkID int64) (*UnsealedCheckResult, error) {
	var row struct {
		SpID          int64          `db:"sp_id"`
		SectorNumber  int64          `db:"sector_number"`
		CreateTime    time.Time      `db:"create_time"`
		TaskID        sql.NullInt64  `db:"task_id"`
		ExpectedCommD string         `db:"expected_unsealed_cid"`
		OK            sql.NullBool   `db:"ok"`
		ActualCommD   sql.NullString `db:"actual_unsealed_cid"`
		Message       sql.NullString `db:"message"`
	}

	err := a.deps.DB.QueryRow(ctx, `
		SELECT sp_id, sector_number, create_time, task_id, expected_unsealed_cid, ok, actual_unsealed_cid, message
		FROM scrub_unseal_commd_check
		WHERE check_id = $1
	`, checkID).Scan(&row.SpID, &row.SectorNumber, &row.CreateTime, &row.TaskID, &row.ExpectedCommD, &row.OK, &row.ActualCommD, &row.Message)
	if err != nil {
		return nil, xerrors.Errorf("querying check status: %w", err)
	}

	result := &UnsealedCheckResult{
		CheckID:       checkID,
		SpID:          row.SpID,
		SectorNumber:  row.SectorNumber,
		ExpectedCommD: row.ExpectedCommD,
		CreateTime:    row.CreateTime.Format(time.RFC3339),
		Complete:      row.OK.Valid,
	}

	if row.TaskID.Valid {
		result.TaskID = &row.TaskID.Int64
	}
	if row.OK.Valid {
		result.OK = &row.OK.Bool
	}
	if row.ActualCommD.Valid {
		result.ActualCommD = row.ActualCommD.String
	}
	if row.Message.Valid {
		result.Message = row.Message.String
	}

	return result, nil
}

// SectorUnsealedCheckList lists recent unsealed checks for a sector
func (a *WebRPC) SectorUnsealedCheckList(ctx context.Context, spStr string, sectorNum int64) ([]*UnsealedCheckResult, error) {
	maddr, err := address.NewFromString(spStr)
	if err != nil {
		return nil, xerrors.Errorf("invalid miner address: %w", err)
	}

	spID, err := address.IDFromAddress(maddr)
	if err != nil {
		return nil, xerrors.Errorf("getting miner ID: %w", err)
	}

	var rows []struct {
		CheckID       int64          `db:"check_id"`
		CreateTime    time.Time      `db:"create_time"`
		ExpectedCommD string         `db:"expected_unsealed_cid"`
		OK            sql.NullBool   `db:"ok"`
		ActualCommD   sql.NullString `db:"actual_unsealed_cid"`
		Message       sql.NullString `db:"message"`
	}

	err = a.deps.DB.Select(ctx, &rows, `
		SELECT check_id, create_time, expected_unsealed_cid, ok, actual_unsealed_cid, message
		FROM scrub_unseal_commd_check
		WHERE sp_id = $1 AND sector_number = $2
		ORDER BY create_time DESC
		LIMIT 10
	`, spID, sectorNum)
	if err != nil {
		return nil, xerrors.Errorf("querying check list: %w", err)
	}

	results := make([]*UnsealedCheckResult, len(rows))
	for i, row := range rows {
		result := &UnsealedCheckResult{
			CheckID:       row.CheckID,
			SpID:          int64(spID),
			SectorNumber:  sectorNum,
			ExpectedCommD: row.ExpectedCommD,
			CreateTime:    row.CreateTime.Format(time.RFC3339),
			Complete:      row.OK.Valid,
		}
		if row.OK.Valid {
			result.OK = &row.OK.Bool
		}
		if row.ActualCommD.Valid {
			result.ActualCommD = row.ActualCommD.String
		}
		if row.Message.Valid {
			result.Message = row.Message.String
		}
		results[i] = result
	}

	return results, nil
}
