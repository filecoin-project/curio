package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/builtin/v13/miner"
	"github.com/filecoin-project/go-state-types/builtin/v9/verifreg"

	"github.com/filecoin-project/curio/cmd/curio/internal/translations"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/reqcontext"
	"github.com/filecoin-project/curio/market/storageingest"

	lapi "github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/actors/builtin/market"
	"github.com/filecoin-project/lotus/chain/actors/policy"
	"github.com/filecoin-project/lotus/chain/types"
)

var marketCmd = &cli.Command{
	Name: "market",
	Subcommands: []*cli.Command{
		marketSealCmd,
		marketAddOfflineURLCmd,
		marketMoveToEscrowCmd,
		ddoCmd,
	},
}

var marketSealCmd = &cli.Command{
	Name:  "seal",
	Usage: translations.T("start sealing a deal sector early"),
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "actor",
			Usage:    translations.T("Specify actor address to start sealing sectors for"),
			Required: true,
		},
		&cli.BoolFlag{
			Name:  "synthetic",
			Usage: translations.T("Use synthetic PoRep"),
			Value: false,
		},
	},

	ArgsUsage: translations.T("<sector>"),
	Action: func(cctx *cli.Context) error {
		act, err := address.NewFromString(cctx.String("actor"))
		if err != nil {
			return xerrors.Errorf("parsing --actor: %w", err)
		}

		if cctx.Args().Len() > 1 {
			return xerrors.Errorf("specify only one sector")
		}
		sec := cctx.Args().First()

		sector, err := strconv.ParseUint(sec, 10, 64)
		if err != nil {
			return xerrors.Errorf("failed to parse the sector number: %w", err)
		}

		ctx := reqcontext.ReqContext(cctx)
		dep, err := deps.GetDepsCLI(ctx, cctx)
		if err != nil {
			return err
		}

		return storageingest.SealNow(ctx, dep.Chain, dep.DB, act, abi.SectorNumber(sector), cctx.Bool("synthetic"))
	},
}

var marketAddOfflineURLCmd = &cli.Command{
	Name:  "add-url",
	Usage: translations.T("Add URL to fetch data for offline deals"),
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "file",
			Usage: translations.T("CSV file location to use for multiple deal input. Each line in the file should be in the format 'uuid,raw size,url,header1,header2...'"),
		},
		&cli.StringSliceFlag{
			Name:    "header",
			Aliases: []string{"H"},
			Usage:   translations.T("Custom `HEADER` to include in the HTTP request"),
		},
		&cli.StringFlag{
			Name:     "url",
			Aliases:  []string{"u"},
			Usage:    translations.T("`URL` to send the request to"),
			Required: true,
		},
	},
	ArgsUsage: "<deal UUID> <raw size/car size>",
	Action: func(cctx *cli.Context) error {
		if !cctx.IsSet("file") && cctx.Args().Len() != 2 {
			return xerrors.Errorf("incorrect number of arguments")
		}

		ctx := reqcontext.ReqContext(cctx)
		dep, err := deps.GetDepsCLI(ctx, cctx)
		if err != nil {
			return err
		}

		if cctx.IsSet("file") {
			// Read file line by line
			fileStr := cctx.String("file")
			loc, err := homedir.Expand(fileStr)
			if err != nil {
				return err
			}
			file, err := os.Open(loc)
			if err != nil {
				return err
			}
			defer file.Close()
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := scanner.Text()
				// Extract pieceCid, pieceSize and MinerAddr from line
				parts := strings.SplitN(line, ",", 4)
				if parts[0] == "" || parts[1] == "" || parts[2] == "" {
					return fmt.Errorf("empty column value in the input file at %s", line)
				}

				uuid := parts[0]
				size, err := strconv.ParseInt(parts[1], 10, 64)
				if err != nil {
					return fmt.Errorf("failed to parse size %w", err)
				}

				url := parts[2]

				if parts[3] != "" {
					header := http.Header{}
					for _, s := range strings.Split(parts[3], ",") {
						key, value, found := strings.Cut(s, ":")
						if !found {
							return fmt.Errorf("invalid header format, expected key:value")
						}
						header.Set(strings.TrimSpace(key), strings.TrimSpace(value))
					}

					hdr, err := json.Marshal(header)
					if err != nil {
						return xerrors.Errorf("marshalling headers: %w", err)
					}
					_, err = dep.DB.Exec(ctx, `INSERT INTO market_offline_urls (
								uuid,
								url,
								headers,
								raw_size
							) VALUES ($1, $2, $3, $4);`,
						uuid, url, hdr, size)
					if err != nil {
						return xerrors.Errorf("adding details to DB: %w", err)
					}
				} else {
					_, err = dep.DB.Exec(ctx, `INSERT INTO market_offline_urls (
								uuid,
								url,
								raw_size
							) VALUES ($1, $2, $3, $4);`,
						uuid, url, size)
					if err != nil {
						return xerrors.Errorf("adding details to DB: %w", err)
					}
				}

				if err := scanner.Err(); err != nil {
					return err
				}
			}
		}

		url := cctx.String("url")

		uuid := cctx.Args().First()

		sizeStr := cctx.Args().Get(1)
		size, err := strconv.ParseInt(sizeStr, 10, 64)
		if err != nil {
			return xerrors.Errorf("parsing size: %w", err)
		}

		if cctx.IsSet("header") {
			// Split the header into key-value
			header := http.Header{}
			headerValue := cctx.StringSlice("header")
			for _, s := range headerValue {
				key, value, found := strings.Cut(s, ":")
				if !found {
					return fmt.Errorf("invalid header format, expected key:value")
				}
				header.Set(strings.TrimSpace(key), strings.TrimSpace(value))
			}

			hdr, err := json.Marshal(header)
			if err != nil {
				return xerrors.Errorf("marshalling headers: %w", err)
			}

			_, err = dep.DB.Exec(ctx, `INSERT INTO market_offline_urls (
								uuid,
								url,
								headers,
								raw_size
							) VALUES ($1, $2, $3, $4);`,
				uuid, url, hdr, size)
			if err != nil {
				return xerrors.Errorf("adding details to DB: %w", err)
			}
		} else {
			_, err = dep.DB.Exec(ctx, `INSERT INTO market_offline_urls (
								uuid,
								url,
                                headers,
								raw_size
							) VALUES ($1, $2, $3, $4);`,
				uuid, url, []byte("{}"), size)
			if err != nil {
				return xerrors.Errorf("adding details to DB: %w", err)
			}
		}

		return nil
	},
}

var marketMoveToEscrowCmd = &cli.Command{
	Name:  "move-to-escrow",
	Usage: translations.T("Moves funds from the deal collateral wallet into escrow with the storage market actor"),
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "actor",
			Usage:    translations.T("Specify actor address to start sealing sectors for"),
			Required: true,
		},
		&cli.StringFlag{
			Name:     "max-fee",
			Usage:    translations.T("maximum fee in FIL user is willing to pay for this message"),
			Required: false,
			Value:    "0.5",
		},
		&cli.StringFlag{
			Name:     "wallet",
			Usage:    translations.T("Specify wallet address to send the funds from"),
			Required: true,
		},
	},
	ArgsUsage: "<amount>",
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 1 {
			return xerrors.Errorf("incorrect number of agruments")
		}
		amount, err := types.ParseFIL(cctx.Args().First())
		if err != nil {
			return xerrors.Errorf("failed to parse the input amount: %w", err)
		}

		amt := abi.TokenAmount(amount)

		if !cctx.IsSet("actor") {
			return cli.ShowCommandHelp(cctx, "move-to-escrow")
		}
		if !cctx.IsSet("wallet") {
			return cli.ShowCommandHelp(cctx, "move-to-escrow")
		}

		wallet, err := address.NewFromString(cctx.String("wallet"))
		if err != nil {
			return xerrors.Errorf("parsing wallet address: %w", err)
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

		obal, err := dep.Chain.StateMarketBalance(ctx, act, types.EmptyTSK)
		if err != nil {
			return err
		}

		params, err := actors.SerializeParams(&act)
		if err != nil {
			return xerrors.Errorf("failed to serialize the parameters: %w", err)
		}

		maxfee, err := types.ParseFIL(cctx.String("max-fee") + " FIL")
		if err != nil {
			return xerrors.Errorf("failed to parse the maximum fee: %w", err)
		}

		msp := &lapi.MessageSendSpec{
			MaxFee: abi.TokenAmount(maxfee),
		}

		w, err := dep.Chain.StateGetActor(ctx, wallet, types.EmptyTSK)
		if err != nil {
			return err
		}
		if w.Balance.LessThan(amt) {
			return xerrors.Errorf("Wallet balance %s is lower than specified amount %s", w.Balance.String(), amt.String())
		}

		msg := &types.Message{
			To:     market.Address,
			From:   wallet,
			Value:  amt,
			Method: market.Methods.AddBalance,
			Params: params,
		}

		smsg, err := dep.Chain.MpoolPushMessage(ctx, msg, msp)
		if err != nil {
			return xerrors.Errorf("moving %s to escrow wallet %s from %s: %w", amount.String(), act, wallet.String(), err)
		}

		fmt.Printf("Funds moved to escrow in message %s\n", smsg.Cid().String())
		fmt.Println("Waiting for the message to be included in a block")
		res, err := dep.Chain.StateWaitMsg(ctx, smsg.Cid(), 2, 2000, true)
		if err != nil {
			return err
		}
		if !res.Receipt.ExitCode.IsSuccess() {
			return xerrors.Errorf("message execution failed with exit code: %d", res.Receipt.ExitCode)
		}
		fmt.Println("Message executed successfully")
		nbal, err := dep.Chain.StateMarketBalance(ctx, act, types.EmptyTSK)
		if err != nil {
			return err
		}
		fmt.Printf("Previous available balance: %s\n New available Balance: %s\n", big.Sub(obal.Escrow, obal.Locked).String(), big.Sub(nbal.Escrow, nbal.Locked).String())
		return nil
	},
}

var ddoCmd = &cli.Command{
	Name:      "ddo",
	Usage:     translations.T("Create a new offline verified DDO deal for Curio"),
	ArgsUsage: "<client-address> <allocation-id>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "actor",
			Usage:    translations.T("Specify actor address for the deal"),
			Required: true,
		},
		&cli.BoolFlag{
			Name:  "remove-unsealed-copy",
			Usage: translations.T("Remove unsealed copies of sector containing this deal"),
			Value: false,
		},
		&cli.BoolFlag{
			Name:  "skip-ipni-announce",
			Usage: translations.T("indicates that deal index should not be announced to the IPNI"),
			Value: false,
		},
		&cli.IntFlag{
			Name:  "start-epoch",
			Usage: translations.T("start epoch by when the deal should be proved by provider on-chain (default: 2 days from now)"),
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() < 2 {
			return xerrors.Errorf("must specify piececid and file path")
		}

		if !cctx.IsSet("actor") {
			return cli.ShowCommandHelp(cctx, "move-to-escrow")
		}

		act, err := address.NewFromString(cctx.String("actor"))
		if err != nil {
			return xerrors.Errorf("parsing --actor: %w", err)
		}

		actID, err := address.IDFromAddress(act)
		if err != nil {
			return err
		}

		ctx := reqcontext.ReqContext(cctx)
		dep, err := deps.GetDepsCLI(ctx, cctx)
		if err != nil {
			return err
		}

		clientAddr, err := address.NewFromString(cctx.Args().Get(0))
		if err != nil {
			return xerrors.Errorf("failed to parse client address: %w", err)
		}

		client, err := dep.Chain.StateLookupID(ctx, clientAddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		allocationIDStr := cctx.Args().Get(1)
		allocationId, err := strconv.ParseInt(allocationIDStr, 10, 64)
		if err != nil {
			return xerrors.Errorf("could not parse allocation id: %w", err)
		}

		head, err := dep.Chain.ChainHead(ctx)
		if err != nil {
			return xerrors.Errorf("getting chain head: %w", err)
		}

		startEpoch := abi.ChainEpoch(cctx.Int("start-epoch"))

		// Set Default if not specified by the user
		if startEpoch == 0 {
			startEpoch = head.Height() + (builtin.EpochsInDay * 2)
		}

		alloc, err := dep.Chain.StateGetAllocation(ctx, client, verifreg.AllocationId(allocationId), head.Key())
		if err != nil {
			return xerrors.Errorf("getting allocation details from chain: %w", err)
		}

		if alloc == nil {
			return xerrors.Errorf("no allocation found with ID %d", allocationId)
		}

		if alloc.Provider != abi.ActorID(actID) {
			return xerrors.Errorf("allocation provider %d does not match actor %d", alloc.Provider, actID)
		}

		// If the TermMin is longer than initial sector duration, the deal will be dropped from the sector
		if alloc.TermMin > miner.MaxSectorExpirationExtension-policy.SealRandomnessLookback {
			return xerrors.Errorf("allocation term min %d is longer than the sector lifetime %d", alloc.TermMin, miner.MaxSectorExpirationExtension-policy.SealRandomnessLookback)
		}

		if alloc.Expiration < startEpoch {
			return xerrors.Errorf("allocation will expire on %d before start epoch %d", alloc.Expiration, startEpoch)
		}

		// Since StartEpoch is more than Head+StartEpochSealingBuffer, we can set end epoch as start+TermMin
		endEpoch := startEpoch + alloc.TermMin

		id := uuid.New().String()

		fastRetrieval := !cctx.Bool("remove-unsealed-copy")
		announce := !cctx.Bool("skip-ipni-announce")

		// Add the deal to DB
		comm, err := dep.DB.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
			_, err = tx.Exec(`INSERT INTO market_direct_deals (
											uuid, sp_id, client, offline, verified, 
											start_epoch, end_epoch, allocation_id, piece_cid, piece_size, 
											fast_retrieval, announce_to_ipni
										) VALUES (
											$1, $2, $3, $4, $5, 
										    $6, $7, $8, $9, $10, 
										    $11, $12
										)
									`, id, actID, client.String(), true, true, startEpoch, endEpoch,
				allocationId, alloc.Data.String(), alloc.Size, fastRetrieval, announce)
			if err != nil {
				return false, xerrors.Errorf("inserting market_direct_deals: %w", err)
			}

			// Insert the offline deal into the deal pipeline
			_, err = tx.Exec(`INSERT INTO market_mk12_deal_pipeline (uuid, sp_id, piece_cid, piece_size, offline, should_index, announce, is_ddo)
								VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (uuid) DO NOTHING`,
				id, actID, alloc.Data.String(), alloc.Size, true, fastRetrieval, announce, true)
			if err != nil {
				return false, xerrors.Errorf("inserting direct deal into deal pipeline: %w", err)
			}

			return true, nil
		}, harmonydb.OptionRetry())

		if err != nil {
			return xerrors.Errorf("inserting market_direct_deals: %w", err)
		}

		if !comm {
			return xerrors.Errorf("failed to commit market_direct_deals: %s", id)
		}

		fmt.Println("Direct deals inserted successfully:", id)
		return nil
	},
}
