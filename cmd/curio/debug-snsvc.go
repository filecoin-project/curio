package main

import (
	"context"
	"fmt"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/filecoin-project/curio/lib/proofsvc/common"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/urfave/cli/v2"
	"math/big"
)

var debugSNSvc = &cli.Command{
	Name: "debug-snsvc",
	Subcommands: []*cli.Command{
		{
			Name:  "deposit",
			Usage: "Deposit FIL into the Router contract (client)",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "from", Usage: "Sender address", Required: true},
				&cli.StringFlag{Name: "amount", Usage: "Amount in attoFIL", Required: true},
			},
			Action: depositAction,
		},
		{
			Name:  "redeem-client",
			Usage: "Redeem a client voucher (service role)",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "from", Usage: "Service sender address", Required: true},
				&cli.Uint64Flag{Name: "client", Usage: "Client actor ID", Required: true},
				&cli.StringFlag{Name: "amount", Usage: "Cumulative amount (attoFIL)", Required: true},
				&cli.Uint64Flag{Name: "nonce", Usage: "Voucher nonce", Required: true},
				&cli.StringFlag{Name: "sig", Usage: "Voucher signature (hex)", Required: true},
			},
			Action: redeemClientAction,
		},
		{
			Name:  "redeem-provider",
			Usage: "Redeem a provider voucher (provider role)",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "from", Usage: "Provider sender address", Required: true},
				&cli.Uint64Flag{Name: "provider", Usage: "Provider actor ID", Required: true},
				&cli.StringFlag{Name: "amount", Usage: "Cumulative amount (attoFIL)", Required: true},
				&cli.Uint64Flag{Name: "nonce", Usage: "Voucher nonce", Required: true},
				&cli.StringFlag{Name: "sig", Usage: "Voucher signature (hex)", Required: true},
			},
			Action: redeemProviderAction,
		},
		{
			Name:  "service-withdraw",
			Usage: "Withdraw funds from the service pool (service role)",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "from", Usage: "Service sender address", Required: true},
				&cli.StringFlag{Name: "amount", Usage: "Amount to withdraw (attoFIL)", Required: true},
			},
			Action: serviceWithdrawAction,
		},
		{
			Name:  "get-client-state",
			Usage: "Query the state of a client",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "from", Usage: "Caller address", Required: true},
				&cli.Uint64Flag{Name: "client", Usage: "Client actor ID", Required: true},
			},
			Action: getClientStateAction,
		},
		{
			Name:  "get-provider-state",
			Usage: "Query the state of a provider",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "from", Usage: "Caller address", Required: true},
				&cli.Uint64Flag{Name: "provider", Usage: "Provider actor ID", Required: true},
			},
			Action: getProviderStateAction,
		},
		{
			Name:  "get-service-state",
			Usage: "Query the service state",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "from", Usage: "Caller address", Required: true},
			},
			Action: getServiceStateAction,
		},
	},
}

// depositAction submits a deposit transaction from a client.
func depositAction(cctx *cli.Context) error {
	ctx := context.Background()
	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	fromStr := cctx.String("from")
	fromAddr, err := address.NewFromString(fromStr)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}

	amountStr := cctx.String("amount")
	amount, ok := new(big.Int).SetString(amountStr, 10)
	if !ok {
		return fmt.Errorf("invalid amount")
	}

	routerAddr := common.Router() // returns the router contract address (Filecoin address)
	if err := common.ClientDeposit(ctx, full, fromAddr, routerAddr, abi.TokenAmount(amount)); err != nil {
		return fmt.Errorf("deposit failed: %w", err)
	}

	fmt.Println("Deposit succeeded")
	return nil
}

// redeemClientAction allows the service to redeem a client voucher.
func redeemClientAction(cctx *cli.Context) error {
	ctx := context.Background()
	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	fromStr := cctx.String("from")
	fromAddr, err := address.NewFromString(fromStr)
	if err != nil {
		return fmt.Errorf("invalid service from address: %w", err)
	}

	clientID := cctx.Uint64("client")

	amountStr := cctx.String("amount")
	cumAmount, ok := new(big.Int).SetString(amountStr, 10)
	if !ok {
		return fmt.Errorf("invalid cumulative amount")
	}
	nonce := cctx.Uint64("nonce")

	sigHex := cctx.String("sig")
	sig := ethcommon.FromHex(sigHex)

	routerAddr := common.Router()
	if err := common.ServiceRedeemClientVoucher(ctx, full, fromAddr, routerAddr, clientID, abi.TokenAmount(cumAmount), nonce, sig); err != nil {
		return fmt.Errorf("redeem client voucher failed: %w", err)
	}
	fmt.Println("Redeem client voucher succeeded")
	return nil
}

// redeemProviderAction allows a provider to redeem a voucher.
func redeemProviderAction(cctx *cli.Context) error {
	ctx := context.Background()
	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	fromStr := cctx.String("from")
	fromAddr, err := address.NewFromString(fromStr)
	if err != nil {
		return fmt.Errorf("invalid provider from address: %w", err)
	}

	providerID := cctx.Uint64("provider")

	amountStr := cctx.String("amount")
	cumAmount, ok := new(big.Int).SetString(amountStr, 10)
	if !ok {
		return fmt.Errorf("invalid cumulative amount")
	}
	nonce := cctx.Uint64("nonce")

	sigHex := cctx.String("sig")
	sig := ethcommon.FromHex(sigHex)

	routerAddr := common.Router()
	if err := common.Cli(ctx, full, fromAddr, routerAddr, providerID, abi.TokenAmount(cumAmount), nonce, sig); err != nil {
		return fmt.Errorf("redeem provider voucher failed: %w", err)
	}
	fmt.Println("Redeem provider voucher succeeded")
	return nil
}

// serviceWithdrawAction allows the service to withdraw residual funds.
func serviceWithdrawAction(cctx *cli.Context) error {
	ctx := context.Background()
	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	fromStr := cctx.String("from")
	fromAddr, err := address.NewFromString(fromStr)
	if err != nil {
		return fmt.Errorf("invalid service from address: %w", err)
	}

	amountStr := cctx.String("amount")
	amount, ok := new(big.Int).SetString(amountStr, 10)
	if !ok {
		return fmt.Errorf("invalid withdraw amount")
	}

	routerAddr := common.Router()
	if err := common.ServiceWithdraw(ctx, full, fromAddr, routerAddr, abi.TokenAmount(amount)); err != nil {
		return fmt.Errorf("service withdraw failed: %w", err)
	}
	fmt.Println("Service withdrawal succeeded")
	return nil
}

// getClientStateAction queries and prints the state for a given client.
func getClientStateAction(cctx *cli.Context) error {
	ctx := context.Background()
	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	fromStr := cctx.String("from")
	fromAddr, err := address.NewFromString(fromStr)
	if err != nil {
		return fmt.Errorf("invalid caller address: %w", err)
	}

	clientID := cctx.Uint64("client")
	routerAddr := common.Router()
	balance, voucherRedeemed, lastNonce, err := common.GetClientState(ctx, full, fromAddr, routerAddr, clientID)
	if err != nil {
		return fmt.Errorf("get client state failed: %w", err)
	}
	fmt.Printf("Client %d: Balance: %s, VoucherRedeemed: %s, LastNonce: %d\n",
		clientID, balance.String(), voucherRedeemed.String(), lastNonce)
	return nil
}

// getProviderStateAction queries and prints the state for a provider.
func getProviderStateAction(cctx *cli.Context) error {
	ctx := context.Background()
	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	fromStr := cctx.String("from")
	fromAddr, err := address.NewFromString(fromStr)
	if err != nil {
		return fmt.Errorf("invalid caller address: %w", err)
	}

	providerID := cctx.Uint64("provider")
	routerAddr := common.Router()
	voucherRedeemed, lastNonce, err := common.GetProviderState(ctx, full, fromAddr, routerAddr, providerID)
	if err != nil {
		return fmt.Errorf("get provider state failed: %w", err)
	}
	fmt.Printf("Provider %d: VoucherRedeemed: %s, LastNonce: %d\n",
		providerID, voucherRedeemed.String(), lastNonce)
	return nil
}

// getServiceStateAction queries and prints the service state.
func getServiceStateAction(cctx *cli.Context) error {
	ctx := context.Background()
	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	fromStr := cctx.String("from")
	fromAddr, err := address.NewFromString(fromStr)
	if err != nil {
		return fmt.Errorf("invalid caller address: %w", err)
	}

	routerAddr := common.Router()
	serviceActor, pool, err := common.GetServiceState(ctx, full, fromAddr, routerAddr)
	if err != nil {
		return fmt.Errorf("get service state failed: %w", err)
	}
	fmt.Printf("Service State: ServiceActor: %d, Pool: %s\n", serviceActor, pool.String())
	return nil
}
