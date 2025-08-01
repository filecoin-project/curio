package proofshare

import (
	"context"

	"golang.org/x/xerrors"
)

func (t *TaskClientPoll) finalizeSectorSnap(ctx context.Context, clientRequest *ClientRequest, proofData []byte) error {
	// Update sector with proof
	_, err := t.db.Exec(ctx, `
		UPDATE sectors_snap_pipeline
		SET after_prove = TRUE, 
			proof = $3, 
			task_id_prove = NULL
		WHERE sp_id = $1 AND sector_number = $2
	`, clientRequest.SpID, clientRequest.SectorNumber, proofData)
	if err != nil {
		return xerrors.Errorf("failed to update sector: %w", err)
	}

	log.Infow("remote snap prove completed successfully",
		"spID", clientRequest.SpID,
		"sectorNumber", clientRequest.SectorNumber,
		"proofSize", len(proofData))
	return nil
}
