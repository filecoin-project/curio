package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	mbig "math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/ethereum/go-ethereum/common"
	"github.com/fatih/color"
	"github.com/ipfs/go-cid"
	"github.com/manifoldco/promptui"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"github.com/yugabyte/gocql"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/cmd/curio/internal/translations"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/reqcontext"
	"github.com/filecoin-project/curio/pdp/contract"

	"github.com/filecoin-project/lotus/chain/types"
)

const filFoxMsgAPI = "https://filfox.info/api/v1/message/"

// Curio toolbox exists to keep `sptool` separated from Curio. Otherwise, `sptool` will require more than
// chain node to function
var toolboxCmd = &cli.Command{
	Name:  "toolbox",
	Usage: translations.T("Tool Box for Curio"),
	Subcommands: []*cli.Command{
		fixMsgCmd,
		registerPDPServiceProviderCmd,
		downgradeCmd,
		fixBoostMigrationCmd,
	},
}

var fixMsgCmd = &cli.Command{
	Name:  "fix-msg",
	Usage: translations.T("Updated DB with message data missing from chain node"),
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "all",
			Usage: translations.T("Update data for messages in wait queue"),
		},
	},
	Action: func(cctx *cli.Context) error {
		all := cctx.Bool("all")

		if all && cctx.Args().Len() > 0 {
			return xerrors.Errorf("cannot specify both --all and message cid")
		}

		if !all && cctx.Args().Len() == 0 {
			return xerrors.Errorf("must specify message cid")
		}

		if !all && cctx.Args().Len() > 1 {
			return xerrors.Errorf("cannot specify multiple message cid")
		}

		var msgCid cid.Cid
		var err error

		if !all {
			msgCidStr := cctx.Args().First()
			msgCid, err = cid.Parse(msgCidStr)
			if err != nil {
				return xerrors.Errorf("failed to parse message cid: %w", err)
			}
		}

		ctx := reqcontext.ReqContext(cctx)
		dep, err := deps.GetDepsCLI(ctx, cctx)
		if err != nil {
			return err
		}

		// retrieve messages from curio.message_waits which does not have a executed_tsk_cid
		var msgs []struct {
			SignedMsgCID string `db:"signed_message_cid"`

			ExecutedTskEpoch *int64  `db:"executed_tsk_epoch"`
			ExecutedMsgCID   *string `db:"executed_msg_cid"`

			ExecutedRcptExitCode *int64 `db:"executed_rcpt_exitcode"`
			ExecutedRcptGasUsed  *int64 `db:"executed_rcpt_gas_used"`
		}

		if !all {
			err = dep.DB.Select(cctx.Context, &msgs, `SELECT
														signed_message_cid,
														executed_tsk_epoch,
														executed_msg_cid,
														executed_rcpt_exitcode,
														executed_rcpt_gas_used
													FROM
														message_waits
													WHERE
													    signed_message_cid = $1 AND
														executed_tsk_cid IS NULL`, msgCid.String())
		} else {
			err = dep.DB.Select(cctx.Context, &msgs, `SELECT
														signed_message_cid,
														executed_tsk_epoch,
														executed_msg_cid,
														executed_rcpt_exitcode,
														executed_rcpt_gas_used
													FROM
														message_waits
													WHERE
														executed_tsk_cid IS NULL`)
		}

		if err != nil {
			return xerrors.Errorf("failed to query message_waits: %w", err)
		}

		// Find the details from FilFox and update the DB
		for _, msg := range msgs {
			ffmsg, err := filfoxMessage(msg.SignedMsgCID)
			if err != nil {
				fmt.Println(err) // Skip errors from FilFox as some msgs may have not landed yet when using --all flag
				continue
			}

			var tskey []cid.Cid

			for _, s := range ffmsg.Blocks {
				bcid, err := cid.Parse(s)
				if err != nil {
					return xerrors.Errorf("failed to parse block cid: %w", err)
				}
				tskey = append(tskey, bcid)
			}

			tsk := types.NewTipSetKey(tskey...)

			tcid, err := tsk.Cid()
			if err != nil {
				return xerrors.Errorf("failed to get tipset cid: %w", err)
			}

			emsg, err := ffMsg2Message(ffmsg)
			if err != nil {
				return err
			}
			execMsg, err := json.Marshal(emsg)
			if err != nil {
				return err
			}

			// once all the variables are gathered call the following for each msg
			_, err = dep.DB.Exec(cctx.Context, `UPDATE message_waits SET
				waiter_machine_id = NULL,
				executed_tsk_cid = $1, executed_tsk_epoch = $2,
				executed_msg_cid = $3, executed_msg_data = $4,
				executed_rcpt_exitcode = $5, executed_rcpt_return = $6, executed_rcpt_gas_used = $7
				               WHERE signed_message_cid = $8 AND executed_tsk_cid IS NULL`,
				tcid, ffmsg.Height, ffmsg.Cid, execMsg,
				0, ffmsg.Receipt.Return, ffmsg.Receipt.GasUsed,
				msg.SignedMsgCID)
			if err != nil {
				return xerrors.Errorf("failed to update message_waits: %w", err)
			}
			fmt.Printf("Updated message_waits for %s\n", msg.SignedMsgCID)
		}

		return nil
	},
}

type FilfoxMsg struct {
	Cid           string   `json:"cid,omitempty"`
	Height        int      `json:"height,omitempty"`
	Timestamp     int      `json:"timestamp,omitempty"`
	Confirmations int      `json:"confirmations,omitempty"`
	Blocks        []string `json:"blocks,omitempty"`
	Version       int      `json:"version,omitempty"`
	From          string   `json:"from,omitempty"`
	FromID        string   `json:"fromId,omitempty"`
	FromActor     string   `json:"fromActor,omitempty"`
	To            string   `json:"to,omitempty"`
	ToID          string   `json:"toId,omitempty"`
	ToActor       string   `json:"toActor,omitempty"`
	Nonce         int      `json:"nonce,omitempty"`
	Value         string   `json:"value,omitempty"`
	GasLimit      int      `json:"gasLimit,omitempty"`
	GasFeeCap     string   `json:"gasFeeCap,omitempty"`
	GasPremium    string   `json:"gasPremium,omitempty"`
	Method        string   `json:"method,omitempty"`
	MethodNumber  int      `json:"methodNumber,omitempty"`
	EvmMethod     string   `json:"evmMethod,omitempty"`
	Params        string   `json:"params,omitempty"`
	Receipt       struct {
		ExitCode int    `json:"exitCode,omitempty"`
		Return   string `json:"return,omitempty"`
		GasUsed  int    `json:"gasUsed,omitempty"`
	} `json:"receipt,omitempty"`
	Size    int    `json:"size,omitempty"`
	Error   string `json:"error,omitempty"`
	BaseFee string `json:"baseFee,omitempty"`
	Fee     struct {
		BaseFeeBurn        string `json:"baseFeeBurn,omitempty"`
		OverEstimationBurn string `json:"overEstimationBurn,omitempty"`
		MinerPenalty       string `json:"minerPenalty,omitempty"`
		MinerTip           string `json:"minerTip,omitempty"`
		Refund             string `json:"refund,omitempty"`
	} `json:"fee,omitempty"`
	Transfers []struct {
		From   string `json:"from,omitempty"`
		FromID string `json:"fromId,omitempty"`
		To     string `json:"to,omitempty"`
		ToID   string `json:"toId,omitempty"`
		Value  string `json:"value,omitempty"`
		Type   string `json:"type,omitempty"`
	} `json:"transfers,omitempty"`
	EthTransactionHash string `json:"ethTransactionHash,omitempty"`
	EventLogCount      int    `json:"eventLogCount,omitempty"`
	SubcallCount       int    `json:"subcallCount,omitempty"`
	TokenTransfers     []any  `json:"tokenTransfers,omitempty"`
}

func filfoxMessage(cid string) (FilfoxMsg, error) {
	url := filFoxMsgAPI + cid

	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return FilfoxMsg{}, xerrors.Errorf("creating request failed: %w", err)
	}

	res, err := client.Do(req)
	if err != nil {
		return FilfoxMsg{}, xerrors.Errorf("request failed: %w", err)
	}

	defer func() {
		_ = res.Body.Close()
	}()
	if res.StatusCode != 200 {
		return FilfoxMsg{}, xerrors.Errorf("request failed with status code %d", res.StatusCode)
	}

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return FilfoxMsg{}, xerrors.Errorf("reading response body failed: %w", err)
	}

	var resp FilfoxMsg
	err = json.Unmarshal(data, &resp)
	if err != nil {
		return FilfoxMsg{}, xerrors.Errorf("unmarshaling response failed: %w", err)
	}
	return resp, nil
}

func ffMsg2Message(ffmsg FilfoxMsg) (types.Message, error) {
	to, err := address.NewFromString(ffmsg.To)
	if err != nil {
		return types.Message{}, xerrors.Errorf("parsing to address failed: %w", err)
	}
	from, err := address.NewFromString(ffmsg.From)
	if err != nil {
		return types.Message{}, xerrors.Errorf("parsing from address failed: %w", err)
	}
	value, err := strconv.Atoi(ffmsg.Value)
	if err != nil {
		return types.Message{}, xerrors.Errorf("parsing value failed: %w", err)
	}
	gasfee, err := strconv.Atoi(ffmsg.GasFeeCap)
	if err != nil {
		return types.Message{}, xerrors.Errorf("parsing gas fee cap failed: %w", err)
	}
	gasprem, err := strconv.Atoi(ffmsg.GasPremium)
	if err != nil {
		return types.Message{}, xerrors.Errorf("parsing gas premium failed: %w", err)
	}

	return types.Message{
		Version:    uint64(ffmsg.Version),
		To:         to,
		From:       from,
		Nonce:      uint64(ffmsg.Nonce),
		Value:      abi.NewTokenAmount(int64(value)),
		GasLimit:   int64(ffmsg.GasLimit),
		GasFeeCap:  abi.NewTokenAmount(int64(gasfee)),
		GasPremium: abi.NewTokenAmount(int64(gasprem)),
		Method:     abi.MethodNum(ffmsg.MethodNumber),
		Params:     []byte(ffmsg.Params),
	}, nil
}

var registerPDPServiceProviderCmd = &cli.Command{
	Name:  "register-pdp-service-provider",
	Usage: translations.T("Register a PDP service provider with Filecoin Service Registry Contract"),
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "name",
			Usage:    translations.T("Service provider name"),
			Required: true,
		},
		&cli.StringFlag{
			Name:  "description",
			Usage: translations.T("Service provider description"),
		},
		&cli.StringFlag{
			Name:     "service-url",
			Usage:    translations.T("URL of the service provider"),
			Required: true,
		},
		&cli.StringFlag{
			Name:  "min-size",
			Usage: translations.T("Minimum piece size"),
			Value: "1 MiB",
		},
		&cli.StringFlag{
			Name:  "max-size",
			Usage: translations.T("Maximum piece size"),
			Value: "64 GiB",
		},
		&cli.BoolFlag{
			Name:  "ipni-piece",
			Usage: translations.T("Supports IPNI piece CID indexing"),
		},
		&cli.BoolFlag{
			Name:  "ipni-ipfs",
			Usage: translations.T("Supports IPNI IPFS CID indexing"),
		},
		&cli.Int64Flag{
			Name:  "price",
			Usage: translations.T("Storage price per TiB per month in USDFC, Default is 1 USDFC."),
			Value: 1000000,
		},
		&cli.Int64Flag{
			Name:  "proving-period",
			Usage: translations.T("Shortest frequency interval in epochs at which the SP is willing to prove access to the stored dataset"),
			Value: 60,
		},
		&cli.StringFlag{
			Name:  "location",
			Usage: translations.T("Location of the service provider"),
		},
		&cli.StringFlag{
			Name:  "token-address",
			Usage: translations.T("Token contract for payment (IERC20(address(0)) for FIL)"),
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := reqcontext.ReqContext(cctx)
		dep, err := deps.GetDepsCLI(ctx, cctx)
		if err != nil {
			return err
		}

		serviceURL, err := url.Parse(cctx.String("service-url"))
		if err != nil {
			return xerrors.Errorf("failed to parse service url: %w", err)
		}

		minSize, err := units.RAMInBytes(cctx.String("min-size"))
		if err != nil {
			return xerrors.Errorf("failed to parse min size: %w", err)
		}

		maxSize, err := units.RAMInBytes(cctx.String("max-size"))
		if err != nil {
			return xerrors.Errorf("failed to parse max size: %w", err)
		}

		if minSize > maxSize {
			return xerrors.Errorf("min size must be less than max size")
		}

		ipiece := cctx.Bool("ipni-piece")
		ipfs := cctx.Bool("ipni-ipfs")

		price := cctx.Int64("price")

		pp := cctx.Int64("proving-period")
		if pp < 0 {
			return xerrors.Errorf("proving period must be greater than 0")
		}

		location := cctx.String("location")
		if location == "" {
			location = "Unknown"
		}

		if len(location) > 128 {
			return xerrors.Errorf("location must be less than 128 characters")
		}

		tokenAddress := cctx.String("token-address")

		if tokenAddress == "" {
			tokenAddress = "0x0000000000000000000000000000000000000000"
		}

		if tokenAddress[0:1] == "0x" {
			tokenAddress = tokenAddress[2:]
		}

		if !common.IsHexAddress(tokenAddress) {
			return xerrors.Errorf("token address is not valid")
		}

		offering := contract.ServiceProviderRegistryStoragePDPOffering{
			ServiceURL:                 serviceURL.String(),
			MinPieceSizeInBytes:        mbig.NewInt(minSize),
			MaxPieceSizeInBytes:        mbig.NewInt(maxSize),
			IpniPiece:                  ipiece,
			IpniIpfs:                   ipfs,
			StoragePricePerTibPerMonth: mbig.NewInt(price),
			MinProvingPeriodInEpochs:   mbig.NewInt(pp),
			Location:                   location,
			PaymentTokenAddress:        common.HexToAddress(tokenAddress),
		}

		ethClient, err := dep.EthClient.Val()
		if err != nil {
			return xerrors.Errorf("failed to get eth client: %w", err)
		}

		name := cctx.String("name")
		description := cctx.String("description")

		id, err := contract.FSRegister(ctx, dep.DB, dep.Chain, ethClient, name, description, offering, nil)
		if err != nil {
			return xerrors.Errorf("failed to register storage provider with service contract: %w", err)
		}

		fmt.Printf("Registered storage provider with ID: %d\n", id)

		return nil
	},
}

var downgradeCmd = &cli.Command{
	Name:        "downgrade",
	Usage:       translations.T("Downgrade a cluster's daatabase to a previous software version."),
	Description: translations.T("If, however, the upgrade has a serious bug and you need to downgrade, first shutdown all nodes in your cluster and then run this command. Finally, only start downgraded nodes."),
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:     "last_good_date",
			Usage:    translations.T("YYYYMMDD when your cluster had the preferred schema. Ex: 20251128"),
			Required: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		db, err := deps.MakeDB(cctx)
		if err != nil {
			return err
		}

		var runningMachines []string
		if err := db.Select(cctx.Context, &runningMachines, `SELECT host_and_port FROM harmony_machines 
		WHERE last_contact > CURRENT_TIMESTAMP - INTERVAL '1 MINUTE' `); err != nil {
			return err
		}

		if len(runningMachines) > 0 {
			return xerrors.Errorf("All machines must be shutdown before downgrading. Machines seen running in the past 60 seconds: %s", strings.Join(runningMachines, ", "))
		}

		// Prompt user to confirm they have a database backup
		fmt.Println()
		fmt.Printf("%s Before proceeding, ensure you have a database backup.\n", color.YellowString("WARNING:"))
		fmt.Printf("  See: %s\n", color.CyanString("https://docs.curiostorage.org/administration/yugabyte-backup"))
		fmt.Println()

		i, _, err := (&promptui.Select{
			Label: "Do you have a database backup?",
			Items: []string{
				"No, abort downgrade",
				"Yes, I have a backup",
			},
		}).Run()
		if err != nil {
			return xerrors.Errorf("selection failed: %w", err)
		}
		if i == 0 {
			fmt.Println("Downgrade cancelled. Please create a database backup before proceeding.")
			return nil
		}

		return db.DowngradeTo(cctx.Context, cctx.Int("last_good_date"))
	},
}

var fixBoostMigrationCmd = &cli.Command{
	Name:  "fix-boost-migration",
	Usage: translations.T("Fix Boost migration"),
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "check",
			Usage: translations.T("check how many entries need to be fixed"),
		},
		&cli.StringFlag{
			Name:  "db-file",
			Usage: translations.T("location of boost.db file"),
		},
		&cli.StringSliceFlag{
			Name:     "boostd-data-hosts",
			Usage:    translations.T("yugabyte hosts to connect to over cassandra interface eg '127.0.0.1'"),
			Required: true,
		},
		&cli.StringFlag{
			Name:  "boostd-data-username",
			Usage: translations.T("yugabyte username to connect to over cassandra interface eg 'cassandra'"),
		},
		&cli.StringFlag{
			Name:  "boostd-data-password",
			Usage: translations.T("yugabyte password to connect to over cassandra interface eg 'cassandra'"),
		},
	},
	Action: func(cctx *cli.Context) error {
		db, err := deps.MakeDB(cctx)
		if err != nil {
			return err
		}

		ctx := cctx.Context

		if cctx.IsSet("check") {
			var count int
			err = db.QueryRow(ctx, `SELECT COUNT(*) FROM market_piece_deal WHERE raw_size = 0`).Scan(&count)
			if err != nil {
				return err
			}
			fmt.Println("Entries to fix:", count)
			return nil
		}

		if cctx.String("db-file") == "" {
			return xerrors.Errorf("db-file flag is required if check flag is not set")
		}

		dbPath := cctx.String("db-file")
		dbPath, err = homedir.Expand(dbPath)
		if err != nil {
			return xerrors.Errorf("expanding home dir failed: %w", err)
		}

		sdb, err := sql.Open("sqlite3", "file:"+dbPath+"?cache=shared")
		if err != nil {
			return xerrors.Errorf("opening sqlite db failed: %w", err)
		}

		sdb.SetMaxOpenConns(1)

		defer func() {
			_ = sdb.Close()
		}()

		var session *gocql.Session

		hosts := cctx.StringSlice("boostd-data-hosts")
		if len(hosts) > 0 {
			cluster := gocql.NewCluster(cctx.StringSlice("boostd-data-hosts")...)
			cluster.Timeout = 60 * time.Second
			if cctx.IsSet("boostd-data-username") && cctx.IsSet("boostd-data-password") {
				cluster.Authenticator = gocql.PasswordAuthenticator{
					Username: cctx.String("boostd-data-username"),
					Password: cctx.String("boostd-data-password"),
				}
			}
			cluster.Keyspace = "idx"
			session, err = cluster.CreateSession()
			if err != nil {
				return xerrors.Errorf("creating cassandra session failed: %w", err)
			}
			defer session.Close()
		}

		var deals []struct {
			ID string `db:"id"`
		}

		err = db.Select(ctx, &deals, `SELECT id FROM market_piece_deal WHERE raw_size = 0 LIMIT 1`)
		if err != nil {
			return xerrors.Errorf("selecting deals failed: %w", err)
		}

		for _, deal := range deals {
			var size int64
			err = sdb.QueryRow(`SELECT TransferSize FROM Deals WHERE ID = ?`, deal.ID).Scan(&size)
			if err != nil {
				return xerrors.Errorf("failed to get transfer size from Boost DB: %w", err)
			}

			if size == 0 && session != nil {
				err = session.Query(`SELECT CarLength FROM PieceDeal WHERE DealUuid = ? AND CarLength > 0 LIMIT 1`, deal.ID).WithContext(ctx).Scan(&size)
				if err != nil {
					return xerrors.Errorf("failed to get size from Cassandra: %w", err)
				}
			}

			if size == 0 {
				fmt.Printf("Deal %s has no size in Cassandra Or SQLite, skipping\n", deal.ID)
				continue
			}

			comm, err := db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
				n, err := tx.Exec(`UPDATE market_piece_deal SET raw_size = $1 WHERE id = $2`, size, deal.ID)
				if err != nil {
					return false, xerrors.Errorf("updating market piece deal failed: %w", err)
				}

				if n != 1 {
					return false, xerrors.Errorf("expected 1 row updated, got %d", n)
				}

				n, err = tx.Exec(`UPDATE market_mk12_deals SET raw_size = $1 WHERE deal_id = $2`, size, deal.ID)
				if err != nil {
					return false, xerrors.Errorf("updating market mk12 deals failed: %w", err)
				}

				if n != 1 {
					return false, xerrors.Errorf("expected 1 row updated, got %d", n)
				}

				_, err = tx.Exec(`UPDATE market_mk12_deal_pipeline SET raw_size = $1 WHERE id = $2`, size, deal.ID)
				if err != nil {
					return false, xerrors.Errorf("updating market mk12 deal pipeline failed: %w", err)
				}

				_, err = tx.Exec(`UPDATE market_mk12_deal_pipeline_migration SET raw_size = $1 WHERE deal_id = $2`, size, deal.ID)
				if err != nil {
					return false, xerrors.Errorf("updating market mk12 deal pipeline migration failed: %w", err)
				}

				return true, nil
			})

			if err != nil {
				return xerrors.Errorf("committing transaction failed: %w", err)
			}

			if !comm {
				return xerrors.Errorf("transaction failed to commit")
			}

			fmt.Println("Fixed deal:", deal.ID)
		}

		return nil
	},
}
