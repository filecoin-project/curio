package main

import (
	"fmt"
	"time"

	"github.com/ipfs/go-datastore"
	"github.com/samber/lo"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	miner12 "github.com/filecoin-project/go-state-types/builtin/v12/miner"

	"github.com/filecoin-project/curio/cmd/curio/guidedsetup"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/reqcontext"
	"github.com/filecoin-project/curio/tasks/seal"

	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/node/repo"
)

var sealCmd = &cli.Command{
	Name:  "seal",
	Usage: "Manage the sealing pipeline",
	Subcommands: []*cli.Command{
		sealStartCmd,
		sealMigrateLMSectorsCmd,
		sealEventsCmd,
	},
}

var sealStartCmd = &cli.Command{
	Name:  "start",
	Usage: "Start new sealing operations manually",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "actor",
			Usage:    "Specify actor address to start sealing sectors for",
			Required: true,
		},
		&cli.BoolFlag{
			Name:  "now",
			Usage: "Start sealing sectors for all actors now (not on schedule)",
		},
		&cli.BoolFlag{
			Name:  "cc",
			Usage: "Start sealing new CC sectors",
		},
		&cli.IntFlag{
			Name:  "count",
			Usage: "Number of sectors to start",
			Value: 1,
		},
		&cli.BoolFlag{
			Name:  "synthetic",
			Usage: "Use synthetic PoRep",
			Value: false,
		},
		&cli.StringSliceFlag{
			Name:  "layers",
			Usage: "list of layers to be interpreted (atop defaults). Default: base",
		},
		&cli.IntFlag{
			Name:        "duration-days",
			Aliases:     []string{"d"},
			Usage:       "How long to commit sectors for",
			DefaultText: "1278 (3.5 years)",
		},
	},
	Action: func(cctx *cli.Context) error {
		if !cctx.Bool("now") {
			return xerrors.Errorf("schedule not implemented, use --now")
		}
		if !cctx.IsSet("actor") {
			return cli.ShowCommandHelp(cctx, "start")
		}
		if !cctx.Bool("cc") {
			return xerrors.Errorf("only CC sectors supported for now")
		}

		act, err := address.NewFromString(cctx.String("actor"))
		if err != nil {
			return xerrors.Errorf("parsing --actor: %w", err)
		}

		ctx := reqcontext.ReqContext(cctx)
		dep, err := deps.GetDepsCLI(ctx, cctx)
		if err != nil {
			return err
		}

		/*
			create table sectors_sdr_pipeline (
			    sp_id bigint not null,
			    sector_number bigint not null,

			    -- at request time
			    create_time timestamp not null,
			    reg_seal_proof int not null,
			    comm_d_cid text not null,

			    [... other not relevant fields]
		*/

		mid, err := address.IDFromAddress(act)
		if err != nil {
			return xerrors.Errorf("getting miner id: %w", err)
		}

		mi, err := dep.Chain.StateMinerInfo(ctx, act, types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("getting miner info: %w", err)
		}

		nv, err := dep.Chain.StateNetworkVersion(ctx, types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("getting network version: %w", err)
		}

		wpt := mi.WindowPoStProofType
		spt, err := miner.PreferredSealProofTypeFromWindowPoStType(nv, wpt, cctx.Bool("synthetic"))
		if err != nil {
			return xerrors.Errorf("getting seal proof type: %w", err)
		}

		var userDuration *int64
		if cctx.IsSet("duration-days") {
			days := cctx.Int("duration-days")
			userDuration = new(int64)
			*userDuration = int64(days) * builtin.EpochsInDay

			if miner12.MaxSectorExpirationExtension < *userDuration {
				return xerrors.Errorf("duration exceeds max allowed: %d > %d", *userDuration, miner12.MaxSectorExpirationExtension)
			}
			if miner12.MinSectorExpiration > *userDuration {
				return xerrors.Errorf("duration is too short: %d < %d", *userDuration, miner12.MinSectorExpiration)
			}
		}

		num, err := seal.AllocateSectorNumbers(ctx, dep.Chain, dep.DB, act, cctx.Int("count"), func(tx *harmonydb.Tx, numbers []abi.SectorNumber) (bool, error) {
			for _, n := range numbers {
				_, err := tx.Exec("insert into sectors_sdr_pipeline (sp_id, sector_number, reg_seal_proof, user_sector_duration_epochs) values ($1, $2, $3, $4)", mid, n, spt, userDuration)
				if err != nil {
					return false, xerrors.Errorf("inserting into sectors_sdr_pipeline: %w", err)
				}
			}
			return true, nil
		})
		if err != nil {
			return xerrors.Errorf("allocating sector numbers: %w", err)
		}

		for _, number := range num {
			fmt.Println(number)
		}

		return nil
	},
}

var sealMigrateLMSectorsCmd = &cli.Command{
	Name:   "migrate-lm-sectors",
	Usage:  "(debug tool) Copy LM sector metadata into Curio DB",
	Hidden: true, // only needed in advanced cases where manual repair is needed
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "miner-repo",
			Usage: "Path to miner repo",
			Value: "~/.lotusminer",
		},
		&cli.BoolFlag{
			Name:    "seal-ignore",
			Usage:   "Ignore sectors that cannot be migrated",
			Value:   false,
			EnvVars: []string{"CURUO_MIGRATE_SEAL_IGNORE"},
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := reqcontext.ReqContext(cctx)
		db, err := deps.MakeDB(cctx)
		if err != nil {
			return err
		}

		r, err := repo.NewFS(cctx.String("miner-repo"))
		if err != nil {
			return err
		}

		ok, err := r.Exists()
		if err != nil {
			return err
		}

		if !ok {
			return fmt.Errorf("repo not initialized at: %s", cctx.String("miner-repo"))
		}

		lr, err := r.LockRO(guidedsetup.StorageMiner)
		if err != nil {
			return fmt.Errorf("locking repo: %w", err)
		}
		defer func() {
			err = lr.Close()
			if err != nil {
				fmt.Println("error closing repo: ", err)
			}
		}()

		mmeta, err := lr.Datastore(ctx, "/metadata")
		if err != nil {
			return xerrors.Errorf("opening miner metadata datastore: %w", err)
		}

		maddrBytes, err := mmeta.Get(ctx, datastore.NewKey("miner-address"))
		if err != nil {
			return xerrors.Errorf("getting miner address datastore entry: %w", err)
		}

		addr, err := address.NewFromBytes(maddrBytes)
		if err != nil {
			return xerrors.Errorf("parsing miner actor address: %w", err)
		}

		unmigSectorShouldFail := func() bool { return !cctx.Bool("seal-ignore") }

		err = guidedsetup.MigrateSectors(ctx, addr, mmeta, db, func(n int) {
			fmt.Printf("Migrating %d sectors\n", n)
		}, unmigSectorShouldFail)
		if err != nil {
			return xerrors.Errorf("migrating sectors: %w", err)
		}

		return nil
	},
}

var sealEventsCmd = &cli.Command{
	Name:  "events",
	Usage: "List pipeline events",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "actor",
			Usage: "Filter events by actor address; lists all if not specified",
		},
		&cli.IntFlag{
			Name:  "sector",
			Usage: "Filter events by sector number; requires --actor to be specified",
		},
		&cli.UintFlag{
			Name:  "last",
			Usage: "Limit output to the last N events",
			Value: 100,
		},
	},
	Action: func(cctx *cli.Context) error {

		var actorID uint64
		var sector abi.SectorNumber
		if cctx.IsSet("actor") {
			act, err := address.NewFromString(cctx.String("actor"))
			if err != nil {
				return fmt.Errorf("parsing --actor: %w", err)
			}
			mid, err := address.IDFromAddress(act)
			if err != nil {
				return fmt.Errorf("getting miner id: %w", err)
			}
			actorID = mid
		}
		if cctx.IsSet("sector") {
			sector = abi.SectorNumber(cctx.Int("sector"))
			if !cctx.IsSet("actor") {
				return fmt.Errorf("the --actor flag is required when using --sector")
			}
		}

		ctx := reqcontext.ReqContext(cctx)
		dep, err := deps.GetDepsCLI(ctx, cctx)
		if err != nil {
			return err
		}

		var events []struct {
			ActorID   int       `db:"sp_id"`
			Sector    int       `db:"sector_number"`
			ID        int       `db:"id"`
			TaskID    int       `db:"task_id"`
			Name      string    `db:"name"`
			Posted    time.Time `db:"posted"`
			WorkStart time.Time `db:"work_start"`
			WorkEnd   time.Time `db:"work_end"`
			Result    bool      `db:"result"`
			Err       string    `db:"err"`
			ByHost    string    `db:"completed_by_host_and_port"`
		}

		if !cctx.IsSet("actor") {
			// list for all actors
			err = dep.DB.Select(ctx, &events, `SELECT s.sp_id, s.sector_number, h.*
				FROM harmony_task_history h
         			JOIN sectors_pipeline_events s ON h.id = s.task_history_id
				ORDER BY h.work_end DESC LIMIT $1;`, cctx.Int("last"))
		} else if cctx.IsSet("sector") {
			// list for specific actor and sector
			err = dep.DB.Select(ctx, &events, `SELECT s.sp_id, s.sector_number, h.*
				FROM harmony_task_history h
         			JOIN sectors_pipeline_events s ON h.id = s.task_history_id
				WHERE s.sp_id = $1 AND s.sector_number = $2 ORDER BY h.work_end DESC LIMIT $3;`, actorID, sector, cctx.Int("last"))
		} else {
			fmt.Println(cctx.IsSet("actor"), cctx.IsSet("sector"))
			// list for specific actor
			err = dep.DB.Select(ctx, &events, `SELECT s.sp_id, s.sector_number, h.*
				FROM harmony_task_history h
         			JOIN sectors_pipeline_events s ON h.id = s.task_history_id
				WHERE s.sp_id = $1 ORDER BY h.work_end DESC LIMIT $2;`, actorID, cctx.Int("last"))
		}

		if err != nil {
			return fmt.Errorf("getting events: %w", err)
		}
		fmt.Printf("Total Events: %d\n", len(events))

		fmt.Printf("%-10s %-10s %-18s %-10s %-28s %-28s %-20s %-20s %-10s %-20s \n", "ActorID", "Sector", "Task", "HistoryID", "Posted", "Start", "Took", "Machine", "Status", "Error")
		for _, e := range events {
			fmt.Printf("%-10d %-10d %-18s %-10d %-28s %-28s %-20s %-20s %-10s %-20s \n",
				e.ActorID,
				e.Sector,
				e.Name,
				e.TaskID,
				e.Posted.Format(time.RFC3339),
				e.WorkStart.Format(time.RFC3339),
				e.WorkEnd.Sub(e.WorkStart),
				e.ByHost,
				lo.Ternary(e.Result, "Success", "Failure"),
				e.Err,
			)
		}
		return nil
	},
}
