package main

import (
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/reqcontext"
	"github.com/filecoin-project/go-state-types/builtin/v9/market"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
)

var testDebugCmd = &cli.Command{
	Name:  "debug",
	Usage: "Collection of debugging utilities",
	Subcommands: []*cli.Command{
		testDebugIpniChunks,
		testDebugMigPcid,
	},
}

var testDebugMigPcid = &cli.Command{
	Name: "mig-pcid",
	Action: func(cctx *cli.Context) error {
		ctx := reqcontext.ReqContext(cctx)
		dep, err := deps.GetDepsCLI(ctx, cctx)
		if err != nil {
			return err
		}

		_, err = dep.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			var bdeals []struct {
				Prop json.RawMessage `db:"proposal"`
				UUID string          `db:"uuid"`
			}

			err = tx.Select(&bdeals, `SELECT 
										uuid,
										proposal
									FROM 
										market_mk12_deals`)
			if err != nil {
				return false, xerrors.Errorf("getting deals from db: %w", err)
			}

			for _, d := range bdeals {
				var prop market.DealProposal
				err = json.Unmarshal(d.Prop, &prop)
				if err != nil {
					return false, xerrors.Errorf("unmarshal proposal: %w", err)
				}

				pcid, err := prop.Cid()
				if err != nil {
					return false, xerrors.Errorf("get cid: %w", err)
				}

				fmt.Println(d.UUID, pcid)

				n, err := tx.Exec(`UPDATE market_mk12_deals SET proposal_cid = $1 WHERE uuid = $2`, pcid, d.UUID)
				if err != nil {
					return false, xerrors.Errorf("update deals: %w", err)
				}
				if n == 0 {
					return false, xerrors.Errorf("update deals: deal not found")
				}
			}
			return true, nil
		})

		return err
	},
}
