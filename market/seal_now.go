package market

import (
	"context"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/network"

	"github.com/filecoin-project/curio/harmony/harmonydb"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
)

type SealNowNodeApi interface {
	StateMinerInfo(ctx context.Context, addr address.Address, tsk types.TipSetKey) (api.MinerInfo, error)
	StateNetworkVersion(ctx context.Context, tsk types.TipSetKey) (networkVersion network.Version, err error)
}

func SealNow(ctx context.Context, node SealNowNodeApi, db *harmonydb.DB, act address.Address, sector abi.SectorNumber, synthetic bool) error {
	mid, err := address.IDFromAddress(act)
	if err != nil {
		return xerrors.Errorf("getting miner id: %w", err)
	}

	mi, err := node.StateMinerInfo(ctx, act, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getting miner info: %w", err)
	}

	nv, err := node.StateNetworkVersion(ctx, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("getting network version: %w", err)
	}

	wpt := mi.WindowPoStProofType
	spt, err := miner.PreferredSealProofTypeFromWindowPoStType(nv, wpt, synthetic)
	if err != nil {
		return xerrors.Errorf("getting seal proof type: %w", err)
	}

	comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		// Get current open sector pieces from DB
		var pieces []struct {
			Sector abi.SectorNumber    `db:"sector_number"`
			Size   abi.PaddedPieceSize `db:"piece_size"`
			Index  uint64              `db:"piece_index"`
			IsSnap bool                `db:"is_snap"`
		}
		err = tx.Select(&pieces, `
					SELECT
					    sector_number,
						piece_size,
						piece_index,
						is_snap
					FROM
						open_sector_pieces
					WHERE
						sp_id = $1 AND sector_number = $2
					ORDER BY
						piece_index DESC;`, mid, sector)
		if err != nil {
			return false, xerrors.Errorf("getting open sectors from DB: %w", err)
		}

		if len(pieces) < 1 {
			return false, xerrors.Errorf("sector %d is not waiting to be sealed", sector)
		}

		if pieces[0].IsSnap {
			// Upgrade
			upt, err := spt.RegisteredUpdateProof()
			if err != nil {
				return false, xerrors.Errorf("getting upgrade proof: %w", err)
			}

			cn, err := tx.Exec(`INSERT INTO sectors_snap_pipeline (sp_id, sector_number, upgrade_proof) VALUES ($1, $2, $3);`, mid, sector, upt)
			if err != nil {
				return false, xerrors.Errorf("adding sector to pipeline: %w", err)
			}

			if cn != 1 {
				return false, xerrors.Errorf("incorrect number of rows returned")
			}

			_, err = tx.Exec("SELECT transfer_and_delete_open_piece_snap($1, $2)", mid, sector)
			if err != nil {
				return false, xerrors.Errorf("adding sector to pipeline: %w", err)
			}

			return true, nil
		}

		cn, err := tx.Exec(`INSERT INTO sectors_sdr_pipeline (sp_id, sector_number, reg_seal_proof) VALUES ($1, $2, $3);`, mid, sector, spt)

		if err != nil {
			return false, xerrors.Errorf("adding sector to pipeline: %w", err)
		}

		if cn != 1 {
			return false, xerrors.Errorf("incorrect number of rows returned")
		}

		_, err = tx.Exec("SELECT transfer_and_delete_open_piece($1, $2)", mid, sector)
		if err != nil {
			return false, xerrors.Errorf("adding sector to pipeline: %w", err)
		}

		return true, nil

	}, harmonydb.OptionRetry())

	if err != nil {
		return xerrors.Errorf("start sealing sector: %w", err)
	}

	if !comm {
		return xerrors.Errorf("start sealing sector: commit failed")
	}

	return nil
}
