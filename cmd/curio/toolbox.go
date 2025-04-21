package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/ipfs/go-cid"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/cmd/curio/internal/translations"
	"github.com/filecoin-project/curio/deps"
	"github.com/filecoin-project/curio/lib/reqcontext"

	"github.com/filecoin-project/lotus/chain/types"
)

const filFoxMsgAPI = "https://filfox.info/api/v1/messages/"

// Curio toolbox exists to keep `sptool` separated from Curio. Otherwise, `sptool` will require more than
// chain node to function
var toolboxCmd = &cli.Command{
	Name:  "toolbox",
	Usage: translations.T("Tool Box for Curio"),
	Subcommands: []*cli.Command{
		fixMsgCmd,
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
			log.Errorw("failed to query message_waits", "error", err)
			return err
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

	defer res.Body.Close()
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
