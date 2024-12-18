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
	"strings"
	"time"

	eabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	fbig "github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"

	"github.com/filecoin-project/curio/build"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build/buildconstants"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/types/ethtypes"
	cliutil "github.com/filecoin-project/lotus/cli/util"
)

// Contract ABI (replace with your actual contract ABI)
// ABI of the deployed contract as a JSON string
const contractABI = `[
    {
        "inputs": [
            {
                "internalType": "string",
                "name": "uuid",
                "type": "string"
            },
            {
                "internalType": "uint256",
                "name": "rateAndTimestamp",
                "type": "uint256"
            },
            {
                "internalType": "bytes",
                "name": "signature",
                "type": "bytes"
            }
        ],
        "name": "pay",
        "outputs": [],
        "stateMutability": "payable",
        "type": "function"
    }
]`

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
		RateAndTimestamp string `json:"msg"` // "0x" + rate(24B) + timestamp(8B)
		Sig              string `json:"sig"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&exchangeRate); err != nil {
		return err
	}
	rateAtto := big.NewInt(0)
	rateAtto.SetString(exchangeRate.RateAndTimestamp[2:24*2+2], 16)

	costAtto := big.NewInt(map[string]int64{"1": 500, "2": 2000}[level])
	costAtto = costAtto.Mul(costAtto, rateAtto)
	costFil, err := types.ParseFIL(costAtto.String() + "attofil")
	if err != nil {
		return xerrors.Errorf("error parsing fil")
	}

	fmt.Println("The cost of level ", level, " membership is ", costFil.String(), " FIL")

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
		contractID = "0xE08bBc65aF1f1a40BD3cbaD290a23925F83b8BBB" //TODO: Update if contract is changed
	} else {
		// Give for free.
		return errors.New(build.BuildTypeString() + " network does not support buying premium memberships")
	}

	ctx := cctx.Context

	to, err := ethtypes.ParseEthAddress(contractID)
	if err != nil {
		return xerrors.Errorf("failed to parse contract address: %w", err)
	}

	toAddr, err := to.ToFilecoinAddress()
	if err != nil {
		return xerrors.Errorf("failed to convert Eth address to Filecoin address: %w", err)
	}

	rtString := strings.TrimPrefix(exchangeRate.RateAndTimestamp, "0x")

	// Convert the hex string to a byte slice
	rtb, err := hex.DecodeString(rtString)
	if err != nil {
		return xerrors.Errorf("failed to decode the rate and timestamp: %w", err)
	}

	// Convert byte slice to big.Int
	rt := new(big.Int).SetBytes(rtb)

	sigString := strings.TrimPrefix(exchangeRate.Sig, "0x")

	// Convert the hex string to a byte slice
	sigb, err := hex.DecodeString(sigString)
	if err != nil {
		return xerrors.Errorf("failed to decode the signature: %w", err)
	}

	// Parse the contract's ABI
	parsedABI, err := eabi.JSON(strings.NewReader(contractABI))
	if err != nil {
		return xerrors.Errorf("Failed to parse contract ABI: %w", err)
	}

	//Encode the method call to the contract (using the pay method with UUID)
	data, err := parsedABI.Pack("pay", premiumID, rt, sigb)
	if err != nil {
		return xerrors.Errorf("Failed to pack the `pay()` function call: %v", err)
	}

	param := abi.CborBytes(data)
	params, err := actors.SerializeParams(&param)
	if err != nil {
		return fmt.Errorf("failed to serialize params: %w", err)
	}

	msg := &types.Message{
		From:       walletAddress,
		To:         toAddr,
		Value:      abi.TokenAmount(costFil),
		Method:     builtin.MethodsEVM.InvokeContract,
		Params:     params,
		GasLimit:   buildconstants.BlockGasLimit,
		GasFeeCap:  fbig.Zero(),
		GasPremium: fbig.Zero(),
	}

	maxFee, err := types.ParseFIL("5 FIL")
	if err != nil {
		return xerrors.Errorf("failed to parse max fee: %w", err)
	}

	mspec := &api.MessageSendSpec{
		MaxFee: abi.TokenAmount(maxFee),
	}

	msg, err = full.GasEstimateMessageGas(ctx, msg, mspec, types.EmptyTSK)
	if err != nil {
		return xerrors.Errorf("failed to estimate gas: %w", err)
	}

	if !cctx.Bool("really-do-it") {
		fmt.Println("Not sending the message... Use '--really-do-it flag to send the message'")
		return nil
	}

	sm, err := full.MpoolPushMessage(ctx, msg, mspec)
	if err != nil {
		return xerrors.Errorf("failed to push message to mempool: %w", err)
	}

	fmt.Printf("Sent the payment in message %s\n", sm.Cid().String())

	res, err := full.StateWaitMsg(ctx, sm.Cid(), 5, 2000, true)
	if err != nil {
		return xerrors.Errorf("failed to wait for message: %w", err)
	}

	if res.Receipt.ExitCode != 0 {
		return xerrors.Errorf("message execution failed (exit code %d)", res.Receipt.ExitCode)
	}

	fmt.Printf("Membership purchased successfully with %s!\n", res.Message.String())

	// Provide a web link to update your Slack list.
	// Fakers have: PremiumID, WalletID, Level, Cost, TxnID, Timestamp
	// To verify, blockchain + wallet-signed blob w/txnID & timestamp.
	//   Market used GLIF for txn read & verifies wallet signature.
	qry := url.Values{}
	qry.Add("network", build.BuildTypeString())
	qry.Add("premiumID", premiumID)
	qry.Add("timestamp", strconv.FormatInt(time.Now().Unix(), 10))
	qry.Add("MessageCID", res.Message.String())
	qry.Add("walletID", walletID)
	qry.Add("exchangeRate", rt.String())
	qry.Add("blockHeight", res.Height.String())
	qry.Add("amount", costFil.String())
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
	return nil
}
