package proofshare

import (
	"context"

	"golang.org/x/xerrors"
)

func (t *TaskClientPoll) finalizeSectorPoRep(ctx context.Context, clientRequest *ClientRequest, proofData []byte) error {
	sectorInfo, err := getSectorInfoPoRep(ctx, t.db, clientRequest.SpID, clientRequest.SectorNumber)
	if err != nil {
		return err
	}

	// Get randomness again (yes, yes, it's the same)
	randomness, err := getRandomnessPoRep(ctx, t.api, sectorInfo)
	if err != nil {
		return err
	}

	// Update sector with proof
	_, err = t.db.Exec(ctx, `
		UPDATE sectors_sdr_pipeline
		SET after_porep = TRUE, 
			seed_value = $3, 
			porep_proof = $4, 
			task_id_porep = NULL
		WHERE sp_id = $1 AND sector_number = $2
	`, clientRequest.SpID, clientRequest.SectorNumber, randomness, proofData)
	if err != nil {
		return xerrors.Errorf("failed to update sector: %w", err)
	}

	log.Infow("remote porep completed successfully",
		"spID", clientRequest.SpID,
		"sectorNumber", clientRequest.SectorNumber,
		"proofSize", len(proofData))
	return nil
}
