package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/build"
	fbig "github.com/filecoin-project/go-state-types/big"

	"github.com/filecoin-project/go-address"

	cliutil "github.com/filecoin-project/lotus/cli/util"
)

var premiumCmd = &cli.Command{
	Name:  "premium",
	Usage: "Manage Filecoin Premium Memberships",
	Subcommands: []*cli.Command{
		{
			Name:      "pay",
			Usage:     "Pay for a premium membership",
			Action:    pay,
			ArgsUsage: "<PremiumID> <walletID> <Level [1|2]>",
			Flags: []cli.Flag{
				&cli.BoolFlag{
					Name:  "really-do-it",
					Usage: "Actually pay for the membership",
				},
			},
		},
	},
}

func pay(cctx *cli.Context) error {
	if cctx.Args().Len() != 3 {
		return cli.ShowCommandHelp(cctx, cctx.Command.Name)
	}

	premiumID := cctx.Args().Get(0)
	walletID := cctx.Args().Get(1)
	level := cctx.Args().Get(2)

	if level != "1" && level != "2" {
		return cli.ShowCommandHelp(cctx, cctx.Command.Name)
	}

	resp, err := http.DefaultClient.Get("https://market.curiostorage.org/api/exchangerate")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var exchangeRate struct {
		Msg string `json:"msg"` // "0x" + rate(24B) + timestamp(8B)
		Sig string `json:"sig"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&exchangeRate); err != nil {
		return err
	}
	rateAtto := big.NewInt(0)
	rateAtto.SetString(exchangeRate.Msg[2:24*2+2], 16)

	costAtto := big.NewInt(map[string]int64{"1": 500, "2": 2000}[level])
	costAtto = costAtto.Mul(costAtto, rateAtto)
	costFil := new(big.Float).Quo(new(big.Float).SetInt(costAtto), new(big.Float).SetInt64(1e18))
	fmt.Println("The cost of level ", level, " membership is ", costFil.Text('f', 3), " FIL")

	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return xerrors.Errorf("connecting to full node: %w", err)
	}
	defer closer()

	walletAddress, err := address.NewFromString(walletID)
	if err != nil {
		return xerrors.Errorf("parsing wallet address: %w", err)
	}
	walletAmnt, err := full.WalletBalance(cctx.Context, walletAddress)
	if err != nil {
		return xerrors.Errorf("getting wallet balance: %w", err)
	}
	if walletAmnt.LessThan(fbig.NewFromGo(costAtto)) {
		return xerrors.Errorf("wallet balance is less than the cost of the membership")
	}

	var contractID string
	if build.BuildType == build.BuildMainnet {
		contractID = "f01234" //TODO!!!!!!!!!
	} else if build.BuildType == build.BuildCalibnet {
		contractID = "f01234" //TODO!!!!!!!!!
	} else {
		// Give for free.
		return errors.New(build.BuildTypeString() + " network does not support buying premium memberships")
	}

	//TODO!!!!!! format a eth_call . Care about --really-do-it flag
	_ = premiumID
	_ = contractID

	txnID := "0x" //TODO!!!!!!!!

	// Provide a web link to update your Slack list.
	// Fakers have: PremiumID, WalletID, Level, Cost, TxnID, Timestamp
	// To verify, blockchain + wallet-signed blob w/txnID & timestamp.
	//   Market used GLIF for txn read & verifies wallet signature.
	var qry url.Values
	qry.Add("network", build.BuildTypeString())
	qry.Add("premiumID", premiumID)
	qry.Add("timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	qry.Add("txnID", txnID)
	qry.Add("walletID", walletID)
	message := qry.Encode()
	sig, err := full.WalletSign(cctx.Context, walletAddress, []byte(message))
	if err != nil {
		return xerrors.Errorf("Purchase made. Could not build link: signing txn: %w", err)
	}
	txnSig, err := sig.MarshalBinary()
	if err != nil {
		return xerrors.Errorf("Purchase made. Could not build link: marshaling txn: %w", err)
	}
	qry.Add("txnSign", hex.EncodeToString(txnSig))
	fmt.Println(`Complete/Verify your registration at: 
		https://market.curiostorage.org/pay/verify/?` + qry.Encode())
	return errors.New("not implemented")
}
