package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/samber/lo"
	"github.com/snadrus/must"
	"github.com/urfave/cli/v2"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/dline"
	"github.com/filecoin-project/go-state-types/proof"

	"github.com/filecoin-project/curio/cmd/curio/internal/translations"
	"github.com/filecoin-project/curio/cmd/curio/tasks"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	cuproof "github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/storiface"
)

var testCmd = &cli.Command{
	Name:  "test",
	Usage: translations.T("Utility functions for testing"),
	Subcommands: []*cli.Command{
		//provingInfoCmd,
		wdPostCmd,
		testDebugCmd,
	},
	Before: func(cctx *cli.Context) error {
		return nil
	},
}

var wdPostCmd = &cli.Command{
	Name:    "window-post",
	Aliases: []string{"wd", "windowpost", "wdpost"},
	Usage:   translations.T("Compute a proof-of-spacetime for a sector (requires the sector to be pre-sealed). These will not send to the chain."),
	Subcommands: []*cli.Command{
		wdPostHereCmd,
		wdPostTaskCmd,
		wdPostVanillaCmd,
	},
}

// wdPostTaskCmd writes to harmony_task and wdpost_partition_tasks, then waits for the result.
// It is intended to be used to test the windowpost scheduler.
// The end of the compute task puts the task_id onto wdpost_proofs, which is read by the submit task.
// The submit task will not send test tasks to the chain, and instead will write the result to harmony_test.
// The result is read by this command, and printed to stdout.
var wdPostTaskCmd = &cli.Command{
	Name:    "task",
	Aliases: []string{"scheduled", "schedule", "async", "asynchronous"},
	Usage:   translations.T("Test the windowpost scheduler by running it on the next available curio. If tasks fail all retries, you will need to ctrl+c to exit."),
	Flags: []cli.Flag{
		&cli.Uint64Flag{
			Name:  "deadline",
			Usage: translations.T("deadline to compute WindowPoSt for "),
			Value: 0,
		},
		&cli.StringSliceFlag{
			Name:  "layers",
			Usage: translations.T("list of layers to be interpreted (atop defaults). Default: base"),
		},
		&cli.StringFlag{
			Name:  "addr",
			Usage: translations.T("SP ID to compute WindowPoSt for"),
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := context.Background()

		deps, err := deps.GetDepsCLI(ctx, cctx)
		if err != nil {
			return xerrors.Errorf("get config: %w", err)
		}

		ts, err := deps.Chain.ChainHead(ctx)
		if err != nil {
			return xerrors.Errorf("cannot get chainhead %w", err)
		}
		ht := ts.Height()

		var spAddr address.Address
		if cctx.IsSet("addr") {
			spAddr, err = address.NewFromString(cctx.String("addr"))
			if err != nil {
				return xerrors.Errorf("invalid sp address: %w", err)
			}
		}

		var taskIDs []int64
		for addr := range deps.Maddrs {
			maddr, err := address.IDFromAddress(address.Address(addr))
			if err != nil {
				return xerrors.Errorf("cannot get miner id %w", err)
			}
			if spAddr != address.Undef && address.Address(addr) != spAddr {
				continue
			}

			var taskId int64

			_, err = deps.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
				err = tx.QueryRow(`INSERT INTO harmony_task (name, posted_time, added_by) VALUES ('WdPost', CURRENT_TIMESTAMP, 123) RETURNING id`).Scan(&taskId)
				if err != nil {
					log.Error("inserting harmony_task: ", err)
					return false, xerrors.Errorf("inserting harmony_task: %w", err)
				}
				_, err = tx.Exec(`INSERT INTO wdpost_partition_tasks 
			(task_id, sp_id, proving_period_start, deadline_index, partition_index) VALUES ($1, $2, $3, $4, $5)`,
					taskId, maddr, ht, cctx.Uint64("deadline"), 0)
				if err != nil {
					log.Error("inserting wdpost_partition_tasks: ", err)
					return false, xerrors.Errorf("inserting wdpost_partition_tasks: %w", err)
				}
				_, err = tx.Exec("INSERT INTO harmony_test (task_id) VALUES ($1)", taskId)
				if err != nil {
					return false, xerrors.Errorf("inserting into harmony_tests: %w", err)
				}
				return true, nil
			}, harmonydb.OptionRetry())
			if err != nil {
				return xerrors.Errorf("writing SQL transaction: %w", err)
			}

			log.Infof("Inserted task %d for miner ID %s. Waiting for success ", taskId, address.Address(addr).String())
			taskIDs = append(taskIDs, taskId)
		}

		var taskID int64
		var result sql.NullString
		var historyIDs []int64

		for len(taskIDs) > 0 {
			time.Sleep(time.Second)
			err = deps.DB.QueryRow(ctx, `SELECT task_id, result FROM harmony_test WHERE task_id = ANY($1)`, taskIDs).Scan(&taskID, &result)
			if err != nil {
				return xerrors.Errorf("reading result from harmony_test: %w", err)
			}
			if result.Valid {
				log.Infof("Result for task %d: %s", taskID, result.String)
				// remove task from list
				taskIDs = lo.Filter(taskIDs, func(v int64, i int) bool {
					return v != taskID
				})
				continue
			}
			fmt.Print(".")

			{
				// look at history
				var hist []struct {
					HistID int64  `db:"id"`
					TaskID string `db:"task_id"`
					Result bool   `db:"result"`
					Err    string `db:"err"`
				}
				err = deps.DB.Select(ctx, &hist, `SELECT id, task_id, result, err FROM harmony_task_history WHERE task_id = ANY($1) ORDER BY work_end DESC`, taskIDs)
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return xerrors.Errorf("reading result from harmony_task_history: %w", err)
				}

				for _, h := range hist {
					if !lo.Contains(historyIDs, h.HistID) {
						historyIDs = append(historyIDs, h.HistID)
						var errstr string
						if len(h.Err) > 0 {
							errstr = h.Err
						}
						fmt.Printf("History for task %d historyID %d: %s\n", taskID, h.HistID, errstr)
					}
				}
			}

			{
				// look for fails
				var found []int64
				err = deps.DB.Select(ctx, &found, `SELECT id FROM harmony_task WHERE id = ANY($1)`, taskIDs)
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return xerrors.Errorf("reading result from harmony_task: %w", err)
				}

				log.Infof("Tasks found in harmony_task: %v", found)
			}
		}
		return nil
	},
}

// This command is intended to be used to verify PoSt compute performance.
// It will not send any messages to the chain. Since it can compute any deadline, output may be incorrectly timed for the chain.
// The entire processing happens in this process while you wait. It does not use the scheduler.
var wdPostHereCmd = &cli.Command{
	Name:    "here",
	Aliases: []string{"cli"},
	Usage:   translations.T("Compute WindowPoSt for performance and configuration testing."),
	Description: translations.T(`Note: This command is intended to be used to verify PoSt compute performance.
It will not send any messages to the chain. Since it can compute any deadline, output may be incorrectly timed for the chain.`),
	ArgsUsage: translations.T("[deadline index]"),
	Flags: []cli.Flag{
		&cli.Uint64Flag{
			Name:  "deadline",
			Usage: translations.T("deadline to compute WindowPoSt for "),
			Value: 0,
		},
		&cli.StringSliceFlag{
			Name:  "layers",
			Usage: translations.T("list of layers to be interpreted (atop defaults). Default: base"),
		},
		&cli.Uint64Flag{
			Name:  "partition",
			Usage: translations.T("partition to compute WindowPoSt for"),
			Value: 0,
		},
		&cli.StringFlag{
			Name:  "addr",
			Usage: translations.T("SP ID to compute WindowPoSt for"),
		},
	},
	Action: func(cctx *cli.Context) error {

		ctx := context.Background()
		deps, err := deps.GetDeps(ctx, cctx)
		if err != nil {
			return err
		}

		var spAddr address.Address
		if cctx.IsSet("addr") {
			spAddr, err = address.NewFromString(cctx.String("addr"))
			if err != nil {
				return xerrors.Errorf("invalid sp address: %w", err)
			}
		}

		wdPostTask, wdPoStSubmitTask, derlareRecoverTask, err := tasks.WindowPostScheduler(
			ctx, deps.Cfg.Fees, deps.Cfg.Proving, deps.Chain, deps.Verif, nil, nil, nil,
			deps.As, deps.Maddrs, deps.DB, deps.Stor, deps.Si, deps.Cfg.Subsystems.WindowPostMaxTasks)
		if err != nil {
			return err
		}
		_, _ = wdPoStSubmitTask, derlareRecoverTask

		if len(deps.Maddrs) == 0 {
			return errors.New("no miners to compute WindowPoSt for")
		}
		head, err := deps.Chain.ChainHead(ctx)
		if err != nil {
			return xerrors.Errorf("failed to get chain head: %w", err)
		}

		di := dline.NewInfo(head.Height(), cctx.Uint64("deadline"), 0, 0, 0, 10 /*challenge window*/, 0, 0)

		for maddr := range deps.Maddrs {
			if spAddr != address.Undef && address.Address(maddr) != spAddr {
				continue
			}

			out, err := wdPostTask.DoPartition(ctx, head, address.Address(maddr), di, cctx.Uint64("partition"), true)
			if err != nil {
				fmt.Println("Error computing WindowPoSt for miner", maddr, err)
				continue
			}
			fmt.Println("Computed WindowPoSt for miner", maddr, ":")
			err = json.NewEncoder(os.Stdout).Encode(out)
			if err != nil {
				fmt.Println("Could not encode WindowPoSt output for miner", maddr, err)
				continue
			}
		}

		return nil
	},
}

var wdPostVanillaCmd = &cli.Command{
	Name:  "vanilla",
	Usage: translations.T("Compute WindowPoSt vanilla proofs and verify them."),
	Flags: []cli.Flag{
		&cli.Uint64Flag{
			Name:  "deadline",
			Usage: translations.T("deadline to compute WindowPoSt for "),
			Value: 0,
		},
		&cli.StringSliceFlag{
			Name:  "layers",
			Usage: translations.T("list of layers to be interpreted (atop defaults). Default: base"),
		},
		&cli.Uint64Flag{
			Name:  "partition",
			Usage: translations.T("partition to compute WindowPoSt for"),
			Value: 0,
		},
		&cli.StringFlag{
			Name:  "addr",
			Usage: translations.T("SP ID to compute WindowPoSt for"),
		},
	},
	Action: func(cctx *cli.Context) error {
		start := time.Now()

		ctx := context.Background()
		deps, err := deps.GetDeps(ctx, cctx)
		if err != nil {
			return err
		}

		var spAddr address.Address
		if cctx.IsSet("addr") {
			spAddr, err = address.NewFromString(cctx.String("addr"))
			if err != nil {
				return xerrors.Errorf("invalid sp address: %w", err)
			}
		}

		wdPostTask, wdPoStSubmitTask, derlareRecoverTask, err := tasks.WindowPostScheduler(
			ctx, deps.Cfg.Fees, deps.Cfg.Proving, deps.Chain, deps.Verif, nil, nil, nil,
			deps.As, deps.Maddrs, deps.DB, deps.Stor, deps.Si, deps.Cfg.Subsystems.WindowPostMaxTasks)
		if err != nil {
			return err
		}
		_, _ = wdPoStSubmitTask, derlareRecoverTask

		if len(deps.Maddrs) == 0 {
			return errors.New("no miners to compute WindowPoSt for")
		}
		head, err := deps.Chain.ChainHead(ctx)
		if err != nil {
			return xerrors.Errorf("failed to get chain head: %w", err)
		}

		di := dline.NewInfo(head.Height(), cctx.Uint64("deadline"), 0, 0, 0, 10 /*challenge window*/, 0, 0)

		for maddr := range deps.Maddrs {
			if spAddr != address.Undef && address.Address(maddr) != spAddr {
				continue
			}

			maddr := address.Address(maddr)
			mid, err := address.IDFromAddress(maddr)
			if err != nil {
				return xerrors.Errorf("failed to get miner id: %w", err)
			}

			buf := new(bytes.Buffer)
			if err := maddr.MarshalCBOR(buf); err != nil {
				return xerrors.Errorf("failed to marshal address to cbor: %w", err)
			}

			headTs, err := deps.Chain.ChainHead(ctx)
			if err != nil {
				return xerrors.Errorf("getting current head: %w", err)
			}

			rand, err := deps.Chain.StateGetRandomnessFromBeacon(ctx, crypto.DomainSeparationTag_WindowedPoStChallengeSeed, di.Challenge, buf.Bytes(), headTs.Key())
			if err != nil {
				return xerrors.Errorf("failed to get chain randomness from beacon for window post (ts=%d; deadline=%d): %w", headTs.Height(), di, err)
			}
			rand[31] &= 0x3f

			parts, err := deps.Chain.StateMinerPartitions(ctx, maddr, di.Index, headTs.Key())
			if err != nil {
				return xerrors.Errorf("getting partitions: %w", err)
			}

			if cctx.Uint64("partition") >= uint64(len(parts)) {
				return xerrors.Errorf("invalid partition: %d", cctx.Uint64("partition"))
			}

			partition := parts[cctx.Uint64("partition")]

			log.Infow("Getting sectors for proof", "partition", cctx.Uint64("partition"), "liveSectors", must.One(partition.LiveSectors.Count()), "allSectors", must.One(partition.AllSectors.Count()))

			xsinfos, err := wdPostTask.SectorsForProof(ctx, maddr, partition.LiveSectors, partition.AllSectors, headTs)
			if err != nil {
				return xerrors.Errorf("getting sectors for proof: %w", err)
			}

			if len(xsinfos) == 0 {
				return xerrors.Errorf("no sectors to prove")
			}

			nv, err := deps.Chain.StateNetworkVersion(ctx, headTs.Key())
			if err != nil {
				return xerrors.Errorf("getting network version: %w", err)
			}

			ppt, err := xsinfos[0].SealProof.RegisteredWindowPoStProofByNetworkVersion(nv)
			if err != nil {
				return xerrors.Errorf("failed to get window post type: %w", err)
			}

			sectorNums := make([]abi.SectorNumber, len(xsinfos))
			sectorMap := make(map[abi.SectorNumber]proof.ExtendedSectorInfo)
			for i, s := range xsinfos {
				sectorNums[i] = s.SectorNumber
				sectorMap[s.SectorNumber] = s
			}

			postChallenges, err := ffi.GeneratePoStFallbackSectorChallenges(ppt, abi.ActorID(mid), abi.PoStRandomness(rand), sectorNums)
			if err != nil {
				return xerrors.Errorf("generating fallback challenges: %w", err)
			}

			maxPartitionSize, err := builtin.PoStProofWindowPoStPartitionSectors(ppt)
			if err != nil {
				return xerrors.Errorf("get sectors count of partition failed:%+v", err)
			}

			sectors := make([]storiface.PostSectorChallenge, 0)
			for i := range postChallenges.Sectors {
				snum := postChallenges.Sectors[i]
				sinfo := sectorMap[snum]

				sectors = append(sectors, storiface.PostSectorChallenge{
					SealProof:    sinfo.SealProof,
					SectorNumber: snum,
					SealedCID:    sinfo.SealedCID,
					Challenge:    postChallenges.Challenges[snum],
					Update:       sinfo.SectorKey != nil,
				})
			}
			elapsed := time.Since(start)
			log.Infow("Generated fallback challenges", "error", err, "elapsed", elapsed, "sectors", len(sectors), "maxPartitionSize", maxPartitionSize, "postChallenges", len(postChallenges.Challenges))

			var outLk sync.Mutex

			wg := sync.WaitGroup{}
			wg.Add(len(sectors))
			throttle := make(chan struct{}, runtime.NumCPU())

			for i, s := range sectors {
				go func(i int, s storiface.PostSectorChallenge) {

					throttle <- struct{}{}
					defer func() {
						<-throttle
					}()
					defer wg.Done()

					start := time.Now()
					vanilla, err := deps.Stor.GenerateSingleVanillaProof(ctx, abi.ActorID(mid), s, ppt)
					elapsed := time.Since(start)

					if err != nil || vanilla == nil {
						outLk.Lock()
						log.Errorw("Generated vanilla proof for sector", "sector", s.SectorNumber, "of", fmt.Sprintf("%d/%d", i+1, len(sectors)), "error", err, "elapsed", elapsed)
						log.Errorw("reading PoSt challenge for sector", "sector", s.SectorNumber, "vlen", len(vanilla), "error", err)
						outLk.Unlock()
						return
					}

					// verify vproofs
					pvi := proof.WindowPoStVerifyInfo{
						Randomness:        abi.PoStRandomness(rand),
						Proofs:            []proof.PoStProof{},
						ChallengedSectors: []proof.SectorInfo{},
					}
					pvi.Proofs = append(pvi.Proofs, proof.PoStProof{
						PoStProof:  ppt,
						ProofBytes: vanilla,
					})
					for _, s := range sectors {
						pvi.ChallengedSectors = append(pvi.ChallengedSectors, proof.SectorInfo{
							SealProof:    s.SealProof,
							SectorNumber: s.SectorNumber,
							SealedCID:    s.SealedCID,
						})
					}

					verifyStart := time.Now()
					ok, err := cuproof.VerifyWindowPoStVanilla(pvi)
					verifyElapsed := time.Since(verifyStart)

					outLk.Lock()
					if elapsed.Seconds() > 2 {
						log.Warnw("Generated vanilla proof for sector (slow)", "sector", s.SectorNumber, "of", fmt.Sprintf("%d/%d", i+1, len(sectors)), "elapsed", elapsed)
					} else {
						log.Debugw("Generated vanilla proof for sector", "sector", s.SectorNumber, "of", fmt.Sprintf("%d/%d", i+1, len(sectors)), "elapsed", elapsed)
					}
					if err != nil || !ok {
						log.Errorw("Verified window post vanilla proofs", "error", err, "elapsed", verifyElapsed, "ok", ok)
					} else {
						log.Debugw("Verified window post vanilla proofs", "elapsed", verifyElapsed, "ok", ok)
					}
					outLk.Unlock()
				}(i, s)
			}
			wg.Wait()
		}

		return nil
	},
}
