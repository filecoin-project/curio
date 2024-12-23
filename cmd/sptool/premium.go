package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	eabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/samber/lo"
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
const paywithFilABI = `[
    {
      "inputs": [
        {
          "internalType": "string",
          "name": "machineId",
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
      "name": "payWithFil",
      "outputs": [],
      "stateMutability": "payable",
      "type": "function"
    }
]`

const payWithStablecoins = `[
	{
      "inputs": [
        {
          "internalType": "string",
          "name": "machineId",
          "type": "string"
        },
        {
          "internalType": "address",
          "name": "user",
          "type": "address"
        },
        {
          "internalType": "address",
          "name": "token",
          "type": "address"
        },
        {
          "internalType": "uint256",
          "name": "amount",
          "type": "uint256"
        },
        {
          "internalType": "string",
          "name": "destinationChain",
          "type": "string"
        }
      ],
      "name": "payWithStableCoin",
      "outputs": [],
      "stateMutability": "payable",
      "type": "function"
    }
]`

var premiumCmd = &cli.Command{
	Name:  "premium",
	Usage: "Manage Filecoin Premium Memberships",
	Subcommands: []*cli.Command{
		pay,
	},
}

var pay = &cli.Command{
	Name:  "pay",
	Usage: "Pay for Curio Premium Membership",
	Subcommands: []*cli.Command{
		payWithFil,
		payWithStableCoins,
	},
}

var payWithFil = &cli.Command{
	Name:      "Fil",
	Usage:     "Pay For Curio Premium Membership using Filecoins",
	ArgsUsage: "<PremiumID> <walletID>",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "really-do-it",
			Usage: "Actually pay for the membership",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 2 {
			return cli.ShowCommandHelp(cctx, cctx.Command.Name)
		}

		premiumID := cctx.Args().Get(0)
		walletID := cctx.Args().Get(1)

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

		costAtto := big.NewInt(1000) // $1000 USD
		costAtto = costAtto.Mul(costAtto, rateAtto)
		costFil, err := types.ParseFIL(costAtto.String() + "attofil")
		if err != nil {
			return xerrors.Errorf("error parsing fil")
		}

		fmt.Println("The cost of membership is ", costFil.String(), " FIL")

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
		parsedABI, err := eabi.JSON(strings.NewReader(paywithFilABI))
		if err != nil {
			return xerrors.Errorf("Failed to parse contract ABI: %w", err)
		}

		//Encode the method call to the contract (using the pay method with UUID)
		data, err := parsedABI.Pack("payWithFil", premiumID, rt, sigb)
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

		return nil
	},
}

var supportedChains = []string{"arbitrum", "ethereum", "polygon"}
var supportedTokens = []string{"usdc", "usdt"}

// chainTokenMap is a map of token <> address per chain
var chainTokenMap = map[string]*struct {
	name   string
	tokens map[string]string
}{
	"arbitrum": {
		name: "arbitrum", // Actual chain name from Axelar https://axelarscan.io/resources/chains
		tokens: map[string]string{
			"usdc": "0xaf88d065e77c8cC2239327C5EDb3A432268e5831", //https://developers.circle.com/stablecoins/usdc-on-main-networks
			"usdt": "",
		},
	},
	"ethereum": {
		name: "Ethereum",
		tokens: map[string]string{
			"usdc": "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48",
			"usdt": "0xdac17f958d2ee523a2206206994597c13d831ec7",
		},
	},
	"polygon": {
		name: "Polygon",
		tokens: map[string]string{
			"usdc": "0x3c499c542cef5e3811e1192ce70d8cc03d5c3359",
			"usdt": "",
		},
	},
}

var payWithStableCoins = &cli.Command{
	Name:      "StableCoin",
	Usage:     "Pay For Curio Premium Membership using StableCoins",
	ArgsUsage: "<PremiumID> <fund address> <filecoin address for gas>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "chain",
			Usage:    "Name of the chain with StableCoin tokens. Supported chains - Arbitrum, Ethereum, Polygon",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "token",
			Usage:    "Name of the StableCoin token. Supported tokens - USDC, USDT",
			Required: true,
		},
		&cli.BoolFlag{
			Name:  "really-do-it",
			Usage: "Actually pay for the membership",
		},
	},
	Action: func(cctx *cli.Context) error {
		if cctx.Args().Len() != 3 {
			return cli.ShowCommandHelp(cctx, cctx.Command.Name)
		}

		chain := cctx.String("chain")
		fmt.Println(chain)
		fmt.Println(strings.ToLower(chain))
		fmt.Println(supportedChains)
		if !lo.Contains(supportedChains, strings.ToLower(chain)) {
			return xerrors.Errorf("unsupported chain")
		}

		token := cctx.String("token")
		if !lo.Contains(supportedTokens, strings.ToLower(token)) {
			return xerrors.Errorf("unsupported token")
		}

		tokenContractString := chainTokenMap[strings.ToLower(chain)].tokens[strings.ToLower(token)]
		if tokenContractString == "" {
			return xerrors.Errorf("token not supported on the sepcified chain")
		}

		premiumID := cctx.Args().Get(0)
		remoteWalletStr := cctx.Args().Get(1)

		from, err := address.NewFromString(cctx.Args().Get(2))
		if err != nil {
			return xerrors.Errorf("parsing filecoin wallet")
		}

		// Parse the contract's ABI
		parsedABI, err := eabi.JSON(strings.NewReader(payWithStablecoins))
		if err != nil {
			return xerrors.Errorf("Failed to parse contract ABI: %w", err)
		}

		// Parameters for the function
		remoteWallet := common.HexToAddress(remoteWalletStr)
		tokenContract := common.HexToAddress(tokenContractString)
		amount := big.NewInt(1000) // 1 token in wei
		destinationChain := chainTokenMap[strings.ToLower(chain)].name

		//Encode the method call to the contract (using the pay method with UUID)
		data, err := parsedABI.Pack("payWithStableCoin", premiumID, remoteWallet, tokenContract, amount, destinationChain)
		if err != nil {
			return xerrors.Errorf("Failed to pack the `pay()` function call: %v", err)
		}

		param := abi.CborBytes(data)
		params, err := actors.SerializeParams(&param)
		if err != nil {
			return fmt.Errorf("failed to serialize params: %w", err)
		}

		var contractID string
		if build.BuildType == build.BuildMainnet {
			contractID = "0xE08bBc65aF1f1a40BD3cbaD290a23925F83b8BBB" //TODO!!!!!!!!!
		} else if build.BuildType == build.BuildCalibnet {
			contractID = "0xE08bBc65aF1f1a40BD3cbaD290a23925F83b8BBB" //TODO: Update if contract is changed
		} else {
			// Give for free.
			return errors.New(build.BuildTypeString() + " network does not support buying premium memberships")
		}

		to, err := ethtypes.ParseEthAddress(contractID)
		if err != nil {
			return xerrors.Errorf("failed to parse contract address: %w", err)
		}

		toAddr, err := to.ToFilecoinAddress()
		if err != nil {
			return xerrors.Errorf("failed to convert Eth address to Filecoin address: %w", err)
		}

		maxFee, err := types.ParseFIL("5 FIL")
		if err != nil {
			return xerrors.Errorf("failed to parse max fee: %w", err)
		}

		msg := &types.Message{
			From:       from,
			To:         toAddr,
			Value:      abi.TokenAmount(maxFee),
			Method:     builtin.MethodsEVM.InvokeContract,
			Params:     params,
			GasLimit:   buildconstants.BlockGasLimit,
			GasFeeCap:  fbig.Zero(),
			GasPremium: fbig.Zero(),
		}

		mspec := &api.MessageSendSpec{
			MaxFee: abi.TokenAmount(maxFee),
		}

		full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
		if err != nil {
			return xerrors.Errorf("connecting to full node: %w", err)
		}
		defer closer()

		ctx := cctx.Context

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
		return nil
	},
}
