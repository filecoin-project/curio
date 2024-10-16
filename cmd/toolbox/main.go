package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"

	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps"
)

var log = logging.Logger("toolbox")

func main() {
	local := []*cli.Command{
		precommitStuckCmd,
	}

	app := &cli.App{
		Name:                 "toolbox",
		Usage:                "Some tools to fix some problems",
		Version:              build.UserVersion(),
		EnableBashCompletion: true,
		Commands:             local,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "repo",
				EnvVars: []string{"LOTUS_PATH"},
				Hidden:  true,
				Value:   "~/.lotus", // TODO: Consider XDG_DATA_HOME
			},
			&cli.StringFlag{
				Name:  "log-level",
				Value: "info",
			},
			&cli.StringFlag{
				Name:     "actor",
				Required: os.Getenv("LOTUS_DOCS_GENERATION") != "1",
				Usage:    "miner actor to manage",
				EnvVars:  []string{"SP_ADDRESS"},
			},
		},
		Before: func(cctx *cli.Context) error {
			return logging.SetLogLevel("toolbox", cctx.String("toolbox"))
		},
	}

	// terminate early on ctrl+c
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-c
		cancel()
		fmt.Println("Received interrupt, shutting down... Press CTRL+C again to force shutdown")
		<-c
		fmt.Println("Forcing stop")
		os.Exit(1)
	}()

	if err := app.RunContext(ctx, os.Args); err != nil {
		log.Errorf("%+v", err)
		os.Exit(1)
		return
	}

}

func SPTActorGetter(cctx *cli.Context) (address.Address, error) {
	addr, err := address.NewFromString(cctx.String("actor"))
	if err != nil {
		return address.Undef, fmt.Errorf("parsing address: %w", err)
	}
	return addr, nil
}

var precommitStuckCmd = &cli.Command{
	Name:  "precommit-stuck",
	Usage: "Perform db operations to fix issues with precommit messages getting stuck",
	Action: func(cctx *cli.Context) error {
		db, err := deps.MakeDB(cctx)
		if err != nil {
			log.Error(err)
			return err
		}

		// retrieve messages from curio.message_waits which does not have a executed_tsk_cid
		var msgs []struct {
			SignedMsgCID    *string `db:"signed_message_cid"`
			PrecommitMsgCID *string `db:"precommit_msg_cid"`
			CommitMsgCID    *string `db:"commit_msg_cid"`

			ExecutedTskCID   *string `db:"executed_tsk_cid"`
			ExecutedTskEpoch *int64  `db:"executed_tsk_epoch"`
			ExecutedMsgCID   *string `db:"executed_msg_cid"`

			ExecutedRcptExitCode *int64 `db:"executed_rcpt_exitcode"`
			ExecutedRcptGasUsed  *int64 `db:"executed_rcpt_gas_used"`
		}

		err = db.Select(cctx.Context, &msgs, `SELECT spipeline.precommit_msg_cid, spipeline.commit_msg_cid, executed_tsk_cid, executed_tsk_epoch, executed_msg_cid, executed_rcpt_exitcode, executed_rcpt_gas_used
					FROM sectors_sdr_pipeline spipeline
					JOIN message_waits ON spipeline.precommit_msg_cid = message_waits.signed_message_cid
					WHERE executed_tsk_cid IS NULL`)
		if err != nil {
			log.Errorw("failed to query message_waits", "error", err)
			return err
		}

		// check if cid is associated with a precommit msg onchain via filfox api
		for _, msg := range msgs {
			ffmsg, err := filfoxMessage(*msg.PrecommitMsgCID)
			if err != nil {
				return err
			}
			if ffmsg.MethodNumber == 28 {
				var tskey []cid.Cid
				for _, s := range ffmsg.Blocks {
					bcid, err := cid.Parse(s)
					if err != nil {
						return err
					}
					tskey = append(tskey, bcid)
				}

				tsk := types.NewTipSetKey(tskey...)

				tcid, err := tsk.Cid()
				if err != nil {
					return err
				}
				fmt.Println(tcid.String())

				emsg, err := ffMsg2Message(ffmsg)
				if err != nil {
					return err
				}
				execMsg, err := json.Marshal(emsg)
				if err != nil {
					return err
				}

				// once all the variables are gathered call the following for each msg
				_, err = db.Exec(cctx.Context, `UPDATE message_waits SET
				waiter_machine_id = NULL,
				executed_tsk_cid = $1, executed_tsk_epoch = $2,
				executed_msg_cid = $3, executed_msg_data = $4,
				executed_rcpt_exitcode = $5, executed_rcpt_return = $6, executed_rcpt_gas_used = $7
				               WHERE signed_message_cid = $8`,
					tcid, ffmsg.Height, msg, execMsg,
					0, ffmsg.Receipt.Return, ffmsg.Receipt.GasUsed,
					*msg.PrecommitMsgCID)
			}

		}

		return nil
	},
}

func req(url string) ([]byte, error) {
	method := "GET"
	client := &http.Client{}
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
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
	url := fmt.Sprintf("https://filfox.info/api/v1/message/%s", cid)
	body, err := req(url)
	if err != nil {
		return FilfoxMsg{}, err
	}
	var resp FilfoxMsg
	err = json.Unmarshal(body, &resp)
	if err != nil {
		return FilfoxMsg{}, err
	}
	return resp, nil
}

func ffMsg2Message(ffmsg FilfoxMsg) (types.Message, error) {
	to, err := address.NewFromString(ffmsg.To)
	if err != nil {
		return types.Message{}, err
	}
	from, err := address.NewFromString(ffmsg.From)
	if err != nil {
		return types.Message{}, err
	}
	value, err := strconv.Atoi(ffmsg.Value)
	if err != nil {
		return types.Message{}, err
	}
	gasfee, err := strconv.Atoi(ffmsg.GasFeeCap)
	if err != nil {
		return types.Message{}, err
	}
	gasprem, err := strconv.Atoi(ffmsg.GasPremium)
	if err != nil {
		return types.Message{}, err
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
