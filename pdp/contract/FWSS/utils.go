package FWSS

import (
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonydb"
)

func EnsureServiceTermination(tx *harmonydb.Tx, dataSetID int64) error {
	n, err := tx.Exec(`INSERT INTO pdp_delete_data_set (id) VALUES ($1) ON CONFLICT (id) DO NOTHING`, dataSetID)
	if err != nil {
		return xerrors.Errorf("failed to insert into pdp_delete_data_set: %w", err)
	}
	if n != 1 && n != 0 {
		return xerrors.Errorf("expected to insert 0 or 1 rows, inserted %d", n)
	}
	return nil
}
