package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	logging "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/go-address"

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
			SignedMsgCID    string `db:"signed_message_cid"`
			PrecommitMsgCID string `db:"precommit_msg_cid"`
			CommitMsgCID    string `db:"commit_msg_cid"`

			ExecutedTskCID   string `db:"executed_tsk_cid"`
			ExecutedTskEpoch int64  `db:"executed_tsk_epoch"`
			ExecutedMsgCID   string `db:"executed_msg_cid"`

			ExecutedRcptExitCode int64 `db:"executed_rcpt_exitcode"`
			ExecutedRcptGasUsed  int64 `db:"executed_rcpt_gas_used"`
		}

		err = db.Select(cctx.Context, &msgs, `SELECT spipeline.precommit_msg_cid, spipeline.commit_msg_cid, executed_tsk_cid, executed_tsk_epoch, executed_msg_cid, executed_rcpt_exitcode, executed_rcpt_gas_used
					FROM sectors_sdr_pipeline spipeline
					JOIN message_waits ON spipeline.precommit_msg_cid = message_waits.signed_message_cid
					WHERE executed_tsk_cid IS NULL`)
		if err != nil {
			log.Errorw("failed to query message_waits", "error", err)
			return err
		}

		for _, msg := range msgs {
			fmt.Println(msg)
		}
		// x := []string{}
		// var tskey []cid.Cid
		// for _, s := range x {
		// 	bcid, err := cid.Parse(s)
		// 	if err != nil {
		// 		return err
		// 	}
		// 	tskey = append(tskey, bcid)
		// }

		// tsk := types.NewTipSetKey(tskey...)

		// tcid, err := tsk.Cid()
		// if err != nil {
		// 	return err
		// }
		// fmt.Println(tcid.String())
		return nil
	},
}
