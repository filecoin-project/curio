package main

import (
	"encoding/csv"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/samber/lo"
	"github.com/snadrus/must"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/lib/paths"
	"github.com/filecoin-project/curio/lib/reqcontext"
	"github.com/filecoin-project/curio/lib/storiface"
)

var unsealCmd = &cli.Command{
	Name:  "unseal",
	Usage: "Manage unsealed data",
	Subcommands: []*cli.Command{
		unsealInfoCmd,
		listUnsealPipelineCmd,
		setTargetUnsealStateCmd,
	},
}

var unsealInfoCmd = &cli.Command{
	Name:      "info",
	Usage:     "Get information about unsealed data",
	ArgsUsage: "[minerAddress] [sectorNumber]",

	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 2 {
			return cli.ShowCommandHelp(cctx, "info")
		}
		minerAddress := cctx.Args().Get(0)
		sectorNumber := cctx.Args().Get(1)

		maddr, err := address.NewFromString(minerAddress)
		if err != nil {
			return xerrors.Errorf("failed to parse miner address: %w", err)
		}

		minerId, err := address.IDFromAddress(maddr)
		if err != nil {
			return xerrors.Errorf("failed to get miner id: %w", err)
		}

		sectorNumberInt, err := strconv.Atoi(sectorNumber)
		if err != nil {
			return xerrors.Errorf("failed to parse sector number: %w", err)
		}

		ctx := reqcontext.ReqContext(cctx)
		dep, err := deps.GetDepsCLI(ctx, cctx)
		if err != nil {
			return err
		}

		type fmeta struct {
			FileType  int64  `db:"sector_filetype"`
			StorageID string `db:"storage_id"`
			URLs      string `db:"urls"`
			CanSeal   bool   `db:"can_seal"`
			CanStore  bool   `db:"can_store"`
		}

		var fileMeta []fmeta

		err = dep.DB.Select(ctx, &fileMeta, `
			SELECT sector_filetype, sl.storage_id, sp.urls, sp.can_seal, sp.can_store FROM sector_location sl
			LEFT JOIN storage_path sp ON sl.storage_id = sp.storage_id
			WHERE miner_id = $1 AND sector_num = $2;
		`, minerId, sectorNumberInt)
		if err != nil {
			return xerrors.Errorf("failed to query sector location: %w", err)
		}

		matchType := func(t storiface.SectorFileType) func(item fmeta) bool {
			return func(item fmeta) bool {
				return storiface.SectorFileType(item.FileType) == t
			}
		}

		sealStoreStr := func(canSeal, canStore bool) string {
			switch {
			case canSeal && canStore:
				return color.CyanString("seal/store")
			case canSeal:
				return color.YellowString("seal")
			case canStore:
				return color.GreenString("store")
			default:
				return color.BlueString("none")
			}
		}

		simpleUrls := func(urls string) (string, error) {
			// urls are , separated, we only want the host:port parts
			// paths.URLSeparator

			var out []string
			for _, urlStr := range paths.UrlsFromString(urls) {
				u, err := url.Parse(urlStr)
				if err != nil {
					return "", err
				}

				out = append(out, u.Host)
			}

			return strings.Join(out, ","), nil
		}

		printMetaFor := func(fileType storiface.SectorFileType) {
			paths := lo.Filter(fileMeta, filterPred(matchType(fileType)))
			for _, path := range paths {
				idSuffix := ".." + path.StorageID[len(path.StorageID)-8:]

				fmt.Printf("  - %s (%s) %s\n", idSuffix, sealStoreStr(path.CanSeal, path.CanStore), must.One(simpleUrls(path.URLs)))
			}
		}

		fmt.Println("** On Disk:")

		if _, ok := lo.Find(fileMeta, matchType(storiface.FTUnsealed)); ok {
			fmt.Printf("Unsealed: %s\n", color.GreenString("✔"))
			printMetaFor(storiface.FTUnsealed)
		} else {
			fmt.Printf("Unsealed: %s\n", color.RedString("✘"))
		}

		_, ok := lo.Find(fileMeta, matchType(storiface.FTSealed))
		_, okSnap := lo.Find(fileMeta, matchType(storiface.FTUpdate))
		ok = ok || okSnap
		if okSnap {
			fmt.Printf("Sealed:   %s %s\n", color.GreenString("✔"), color.YellowString("snap"))
			printMetaFor(storiface.FTUpdate)
		} else if ok {
			fmt.Printf("Sealed:   %s\n", color.GreenString("✔"))
			printMetaFor(storiface.FTSealed)
		} else {
			fmt.Printf("Sealed:   %s\n", color.RedString("✘"))
		}

		var meta []struct {
			TicketValue       []byte `db:"ticket_value"`
			TargetUnsealState *bool  `db:"target_unseal_state"`
		}

		err = dep.DB.Select(ctx, &meta, `
			SELECT ticket_value, target_unseal_state FROM sectors_meta WHERE sp_id = $1 AND sector_num = $2
		`, minerId, sectorNumberInt)
		if err != nil {
			return xerrors.Errorf("failed to query sector meta: %w", err)
		}

		fmt.Println()

		if len(meta) > 0 {
			if meta[0].TargetUnsealState == nil {
				fmt.Printf("Target Unsealed State: %s\n", color.YellowString("keep as is"))
			} else if *meta[0].TargetUnsealState {
				fmt.Printf("Target Unsealed State: %s\n", color.GreenString("ensure unsealed"))
			} else {
				fmt.Printf("Target Unsealed State: %s\n", color.BlueString("ensure no unsealed"))
			}

			if len(meta[0].TicketValue) > 0 {
				fmt.Printf("Ticket: %s\n", color.GreenString("✔"))
			} else {
				fmt.Printf("Ticket: %s (unseal not possible)\n", color.RedString("✘"))
			}
		}

		var pipeline []struct {
			CreateTime         time.Time `db:"create_time"`
			TaskIDUnsealSDR    *int64    `db:"task_id_unseal_sdr"`
			AfterUnsealSDR     bool      `db:"after_unseal_sdr"`
			TaskIDDecodeSector *int64    `db:"task_id_decode_sector"`
			AfterDecodeSector  bool      `db:"after_decode_sector"`
		}

		err = dep.DB.Select(ctx, &pipeline, `
			SELECT create_time, task_id_unseal_sdr, after_unseal_sdr, task_id_decode_sector, after_decode_sector FROM sectors_unseal_pipeline WHERE sp_id = $1 AND sector_number = $2
		`, minerId, sectorNumberInt)
		if err != nil {
			return xerrors.Errorf("failed to query sector pipeline: %w", err)
		}

		fmt.Println()

		if len(pipeline) > 0 {
			fmt.Printf("Unseal Pipeline:\n")
			fmt.Printf("  - Created: %s\n", pipeline[0].CreateTime)

			if pipeline[0].TaskIDUnsealSDR != nil {
				fmt.Printf("  - Unseal SDR: %s running (task %d)\n", color.YellowString("⧖"), *pipeline[0].TaskIDUnsealSDR)
			} else {
				if pipeline[0].AfterUnsealSDR {
					fmt.Printf("  - Unseal SDR: %s done\n", color.GreenString("✔"))
				} else {
					fmt.Printf("  - Unseal SDR: %s not done\n", color.RedString("✘"))
				}
			}

			if pipeline[0].TaskIDDecodeSector != nil {
				fmt.Printf("  - Decode Sector: %s running (task %d)\n", color.YellowString("⧖"), *pipeline[0].TaskIDDecodeSector)
			} else {
				if pipeline[0].AfterDecodeSector {
					fmt.Printf("  - Decode Sector: %s done\n", color.GreenString("✔"))
				} else {
					fmt.Printf("  - Decode Sector: %s not done\n", color.RedString("✘"))
				}
			}
		} else {
			fmt.Printf("Unseal Pipeline: %s no entry\n", color.RedString("✘"))
		}

		return nil
	},
}

func filterPred[T any](pred func(T) bool) func(T, int) bool {
	return func(item T, _ int) bool {
		return pred(item)
	}
}

var listUnsealPipelineCmd = &cli.Command{
	Name:  "list-sectors",
	Usage: "List data from the sectors_unseal_pipeline and sectors_meta tables",
	Flags: []cli.Flag{
		&cli.Int64Flag{
			Name:    "sp-id",
			Aliases: []string{"s"},
			Usage:   "Filter by storage provider ID",
		},
		&cli.StringFlag{
			Name:    "output",
			Aliases: []string{"o"},
			Usage:   "Output file path (default: stdout)",
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := reqcontext.ReqContext(cctx)
		dep, err := deps.GetDepsCLI(ctx, cctx)
		if err != nil {
			return err
		}

		rows, err := dep.DB.Query(ctx, `
				SELECT 
					sm.sp_id, 
					sm.sector_num, 
					sm.reg_seal_proof, 
					sm.target_unseal_state,
					sm.is_cc,
					sup.create_time as create_time,
					sup.task_id_unseal_sdr, 
					sup.after_unseal_sdr, 
					sup.task_id_decode_sector, 
					sup.after_decode_sector
				FROM 
					sectors_meta sm
				LEFT JOIN 
					sectors_unseal_pipeline sup 
				ON 
					sm.sp_id = sup.sp_id AND sm.sector_num = sup.sector_number
				WHERE 
					($1 = 0 OR sm.sp_id = $1)
				ORDER BY 
					sm.sp_id, sm.sector_num DESC
			`, cctx.Int64("sp-id"))
		if err != nil {
			return xerrors.Errorf("failed to query sectors data: %w", err)
		}
		defer rows.Close()

		writer := csv.NewWriter(os.Stdout)
		if output := cctx.String("output"); output != "" {
			file, err := os.Create(output)
			if err != nil {
				return xerrors.Errorf("failed to create output file: %w", err)
			}
			defer file.Close()
			writer = csv.NewWriter(file)
		}
		defer writer.Flush()

		// Write header
		if err := writer.Write([]string{
			"SP ID", "Sector Number", "Reg Seal Proof", "Target Unseal State", "Is CC",
			"Create Time", "Task ID Unseal SDR", "After Unseal SDR",
			"Task ID Decode Sector", "After Decode Sector",
		}); err != nil {
			return xerrors.Errorf("failed to write CSV header: %w", err)
		}

		// Write data
		for rows.Next() {
			var spID, sectorNumber, regSealProof int64
			var targetUnsealState, isCC *bool
			var createTime *time.Time
			var taskIDUnsealSDR, taskIDDecodeSector *int64
			var afterUnsealSDR, afterDecodeSector *bool

			err := rows.Scan(
				&spID, &sectorNumber, &regSealProof, &targetUnsealState, &isCC,
				&createTime, &taskIDUnsealSDR, &afterUnsealSDR,
				&taskIDDecodeSector, &afterDecodeSector,
			)
			if err != nil {
				return xerrors.Errorf("failed to scan row: %w", err)
			}

			var cts string
			if createTime != nil {
				cts = createTime.Format(time.RFC3339)
			}

			row := []string{
				"f0" + strconv.FormatInt(spID, 10),
				strconv.FormatInt(sectorNumber, 10),
				strconv.FormatInt(regSealProof, 10),
				formatNullableBool(targetUnsealState),
				formatNullableBool(isCC),
				cts,
				formatNullableInt64(taskIDUnsealSDR),
				formatNullableBool(afterUnsealSDR),
				formatNullableInt64(taskIDDecodeSector),
				formatNullableBool(afterDecodeSector),
			}

			if err := writer.Write(row); err != nil {
				return xerrors.Errorf("failed to write CSV row: %w", err)
			}
		}

		if err := rows.Err(); err != nil {
			return xerrors.Errorf("error iterating rows: %w", err)
		}

		fmt.Println("Data exported successfully.")
		return nil
	},
}

var setTargetUnsealStateCmd = &cli.Command{
	Name:      "set-target-state",
	Usage:     "Set the target unseal state for a sector",
	ArgsUsage: "<sp-id> <sector-number> <target-state>",
	Description: `Set the target unseal state for a specific sector.
   <sp-id>: The storage provider ID
   <sector-number>: The sector number
   <target-state>: The target state (true, false, or none)`,
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 3 {
			return cli.ShowSubcommandHelp(cctx)
		}

		sp, err := address.NewFromString(cctx.Args().Get(0))
		if err != nil {
			return xerrors.Errorf("invalid storage provider address: %w", err)
		}

		spID, err := address.IDFromAddress(sp)
		if err != nil {
			return xerrors.Errorf("failed to get storage provider id: %w", err)
		}

		sectorNum, err := strconv.ParseInt(cctx.Args().Get(1), 10, 64)
		if err != nil {
			return xerrors.Errorf("invalid sector-number: %w", err)
		}

		targetStateStr := strings.ToLower(cctx.Args().Get(2))
		var targetState *bool
		switch targetStateStr {
		case "true":
			trueVal := true
			targetState = &trueVal
		case "false":
			falseVal := false
			targetState = &falseVal
		case "none":
			targetState = nil
		default:
			return xerrors.Errorf("invalid target-state: must be true, false, or none")
		}

		ctx := reqcontext.ReqContext(cctx)
		dep, err := deps.GetDepsCLI(ctx, cctx)
		if err != nil {
			return err
		}

		_, err = dep.DB.Exec(ctx, `
			UPDATE sectors_meta
			SET target_unseal_state = $1
			WHERE sp_id = $2 AND sector_num = $3
		`, targetState, spID, sectorNum)
		if err != nil {
			return xerrors.Errorf("failed to update target unseal state: %w", err)
		}

		fmt.Printf("Successfully set target unseal state to %v for SP %d, sector %d\n", targetStateStr, spID, sectorNum)
		return nil
	},
}

func formatNullableInt64(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}

func formatNullableBool(v *bool) string {
	if v == nil {
		return ""
	}
	return strconv.FormatBool(*v)
}