package main

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/filecoin-project/curio/lib/proofsvc/common"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/urfave/cli/v2"
)

var debugSNSvc = &cli.Command{
	Name: "debug-snsvc",
	Subcommands: []*cli.Command{
		{
			Name:  "deposit",
			Usage: "Deposit FIL into the Router contract (client)",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "from", Usage: "Sender address", Required: true},
				&cli.StringFlag{Name: "amount", Usage: "Amount in FIL", Required: true},
			},
			Action: depositAction,
		},
		{
			Name:  "redeem-client",
			Usage: "Redeem a client voucher (service role)",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "from", Usage: "Service sender address", Required: true},
				&cli.StringFlag{Name: "client", Usage: "Client actor", Required: true},
				&cli.StringFlag{Name: "amount", Usage: "Cumulative amount (FIL)", Required: true},
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
				&cli.StringFlag{Name: "provider", Usage: "Provider actor", Required: true},
				&cli.StringFlag{Name: "amount", Usage: "Cumulative amount (FIL)", Required: true},
				&cli.Uint64Flag{Name: "nonce", Usage: "Voucher nonce", Required: true},
				&cli.StringFlag{Name: "sig", Usage: "Voucher signature (hex)", Required: true},
			},
			Action: redeemProviderAction,
		},
		{
			Name:  "service-withdraw",
			Usage: "Withdraw funds from the service pool (service role)",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "amount", Usage: "Amount to withdraw (FIL)", Required: true},
			},
			Action: serviceWithdrawAction,
		},
		{
			Name:  "get-client-state",
			Usage: "Query the state of a client",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "client", Usage: "Client actor address", Required: true},
			},
			Action: getClientStateAction,
		},
		{
			Name:  "get-provider-state",
			Usage: "Query the state of a provider",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "provider", Usage: "Provider actor address", Required: true},
			},
			Action: getProviderStateAction,
		},
		{
			Name:   "get-service-state",
			Usage:  "Query the service state",
			Flags:  []cli.Flag{},
			Action: getServiceStateAction,
		},
		{
			Name:   "create-client-voucher",
			Usage:  "Create a client voucher",
			Flags:  []cli.Flag{
				&cli.StringFlag{Name: "client", Usage: "Client actor address", Required: true},
				&cli.StringFlag{Name: "amount", Usage: "Amount to redeem (FIL)", Required: true},
			},
			Action: createClientVoucherAction,
		},
		{
			Name:   "create-provider-voucher",
			Usage:  "Create a provider voucher",
			Flags:  []cli.Flag{
				&cli.StringFlag{Name: "provider", Usage: "Provider actor address", Required: true},
				&cli.StringFlag{Name: "amount", Usage: "Amount to redeem (FIL)", Required: true},
				&cli.Uint64Flag{Name: "nonce", Usage: "Voucher nonce", Required: true},
			},
			Action: createProviderVoucherAction,
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
	filAmount, err := types.ParseFIL(amountStr)
	if err != nil {
		return fmt.Errorf("invalid amount: %w", err)
	}

	routerAddr := common.Router() // returns the router contract address (Filecoin address)
	if err := common.ClientDeposit(ctx, full, fromAddr, routerAddr, abi.TokenAmount(filAmount)); err != nil {
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

	clientStr := cctx.String("client")
	clientAddr, err := address.NewFromString(clientStr)
	if err != nil {
		return fmt.Errorf("invalid client address: %w", err)
	}

	clientIdAddr, err := full.StateLookupID(ctx, clientAddr, types.EmptyTSK)
	if err != nil {
		return fmt.Errorf("get client id failed: %w", err)
	}

	clientID, err := address.IDFromAddress(clientIdAddr)
	if err != nil {
		return fmt.Errorf("get client id failed: %w", err)
	}

	amountStr := cctx.String("amount")
	filAmount, err := types.ParseFIL(amountStr)
	if err != nil {
		return fmt.Errorf("invalid amount: %w", err)
	}

	nonce := cctx.Uint64("nonce")

	sigHex := cctx.String("sig")
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	routerAddr := common.Router()
	if err := common.ServiceRedeemClientVoucher(ctx, full, fromAddr, routerAddr, clientID, abi.TokenAmount(filAmount), nonce, sig); err != nil {
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

	providerStr := cctx.String("provider")
	providerAddr, err := address.NewFromString(providerStr)
	if err != nil {
		return fmt.Errorf("invalid provider address: %w", err)
	}

	providerIdAddr, err := full.StateLookupID(ctx, providerAddr, types.EmptyTSK)
	if err != nil {
		return fmt.Errorf("get provider id failed: %w", err)
	}

	providerID, err := address.IDFromAddress(providerIdAddr)
	if err != nil {
		return fmt.Errorf("get provider id failed: %w", err)
	}

	amountStr := cctx.String("amount")
	filAmount, err := types.ParseFIL(amountStr)
	if err != nil {
		return fmt.Errorf("invalid cumulative amount: %w", err)
	}

	nonce := cctx.Uint64("nonce")

	sigHex := cctx.String("sig")
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	routerAddr := common.Router()
	if err := common.ServiceRedeemProviderVoucher(ctx, full, fromAddr, routerAddr, providerID, abi.TokenAmount(filAmount), nonce, sig); err != nil {
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

	amountStr := cctx.String("amount")
	filAmount, err := types.ParseFIL(amountStr)
	if err != nil {
		return fmt.Errorf("invalid withdraw amount: %w", err)
	}
	
	if err := common.ServiceWithdraw(ctx, full, abi.TokenAmount(filAmount)); err != nil {
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

	clientAddrStr := cctx.String("client")
	clientAddr, err := address.NewFromString(clientAddrStr)
	if err != nil {
		return fmt.Errorf("invalid client address: %w", err)
	}

	clientIdAddr, err := full.StateLookupID(ctx, clientAddr, types.EmptyTSK)
	if err != nil {
		return fmt.Errorf("get client id failed: %w", err)
	}

	clientID, err := address.IDFromAddress(clientIdAddr)
	if err != nil {
		return fmt.Errorf("get client id failed: %w", err)
	}

	routerAddr := common.Router()
	balance, voucherRedeemed, lastNonce, err := common.GetClientState(ctx, full, routerAddr, clientID)
	if err != nil {
		return fmt.Errorf("get client state failed: %w", err)
	}
	fmt.Printf("Client %s: Balance: %s, VoucherRedeemed: %s, LastNonce: %d\n",
		clientAddrStr, types.FIL(balance).String(), types.FIL(voucherRedeemed).String(), lastNonce)
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

	providerAddrStr := cctx.String("provider")
	providerAddr, err := address.NewFromString(providerAddrStr)
	if err != nil {
		return fmt.Errorf("invalid provider address: %w", err)
	}

	providerIdAddr, err := full.StateLookupID(ctx, providerAddr, types.EmptyTSK)
	if err != nil {
		return fmt.Errorf("get provider id failed: %w", err)
	}

	providerID, err := address.IDFromAddress(providerIdAddr)
	if err != nil {
		return fmt.Errorf("get provider id failed: %w", err)
	}

	routerAddr := common.Router()
	voucherRedeemed, lastNonce, err := common.GetProviderState(ctx, full, routerAddr, providerID)
	if err != nil {
		return fmt.Errorf("get provider state failed: %w", err)
	}
	fmt.Printf("Provider %d: VoucherRedeemed: %s, LastNonce: %d\n",
		providerID, types.FIL(voucherRedeemed).String(), lastNonce)
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

	routerAddr := common.Router()
	serviceActor, pool, err := common.GetServiceState(ctx, full, routerAddr)
	if err != nil {
		return fmt.Errorf("get service state failed: %w", err)
	}
	fmt.Printf("Service State: ServiceActor: %d, Pool: %s\n", serviceActor, types.FIL(pool).String())
	return nil
}

func createClientVoucherAction(cctx *cli.Context) error {
	// Retrieve command-line flags.
	clientStr := cctx.String("client")
	amountStr := cctx.String("amount")

	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	// Parse the client Filecoin address.
	clientAddr, err := address.NewFromString(clientStr)
	if err != nil {
		return fmt.Errorf("invalid client address: %w", err)
	}

	// Parse the FIL amount (expected in attoFIL).
	cumulativeAmount, err := types.ParseFIL(amountStr)
	if err != nil {
		return fmt.Errorf("invalid amount: %w", err)
	}

	clientIDAddr, err := full.StateLookupID(cctx.Context, clientAddr, types.EmptyTSK)
	if err != nil {
		return fmt.Errorf("get client id failed: %w", err)
	}

	// Convert the client address to its actor ID.
	clientID, err := address.IDFromAddress(clientIDAddr)
	if err != nil {
		return fmt.Errorf("failed to get client actor ID: %w", err)
	}

	var nonce uint64
	{
		routerAddr := common.Router()

		balance, voucherRedeemed, lastNonce, err := common.GetClientState(cctx.Context, full, routerAddr, clientID)
		if err != nil {
			return fmt.Errorf("get client state failed: %w", err)
		}
		fmt.Printf("Client %s: Balance: %s, VoucherRedeemed: %s, LastNonce: %d\n",
			clientAddr.String(), types.FIL(balance).String(), types.FIL(voucherRedeemed).String(), lastNonce)

		nonce = lastNonce + 1
		cumulativeAmount = types.FIL(types.BigAdd(types.BigInt(voucherRedeemed), types.BigInt(cumulativeAmount)))
	}

	fmt.Printf("Creating client voucher for client %s with cumulative amount %s and nonce %d\n",
		clientAddr.String(), types.FIL(cumulativeAmount).String(), nonce)

	// Create the voucher message using the common implementation.
	voucher, err := common.CreateClientVoucher(cctx.Context, full, clientID, cumulativeAmount.Int, nonce)
	if err != nil {
		return fmt.Errorf("create client voucher failed: %w", err)
	}

	// Output the voucher as a hex-encoded string.
	fmt.Println("Client Voucher:", hex.EncodeToString(voucher))

	sig, err := full.WalletSign(cctx.Context, clientAddr, voucher)
	if err != nil {
		return fmt.Errorf("sign voucher failed: %w", err)
	}

	fmt.Println("Client Voucher Signature:", hex.EncodeToString(sig.Data))

	ok, err := full.WalletVerify(cctx.Context, clientAddr, voucher, sig)
	if err != nil {
		return fmt.Errorf("verify voucher failed: %w", err)
	}
	fmt.Println("Verification result:", ok)
	return nil
}

func createProviderVoucherAction(cctx *cli.Context) error {
	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	// Retrieve command-line flags.
	providerStr := cctx.String("provider")
	amountStr := cctx.String("amount")

	// Parse the provider Filecoin address.
	providerAddr, err := address.NewFromString(providerStr)
	if err != nil {
		return fmt.Errorf("invalid provider address: %w", err)
	}

	providerIDAddr, err := full.StateLookupID(cctx.Context, providerAddr, types.EmptyTSK)
	if err != nil {
		return fmt.Errorf("get provider id failed: %w", err)
	}

	// Parse the FIL amount (expected in attoFIL).
	cumulativeAmount, err := types.ParseFIL(amountStr)
	if err != nil {
		return fmt.Errorf("invalid amount: %w", err)
	}

	nonce := cctx.Uint64("nonce")

	// Convert the provider address to its actor ID.
	providerID, err := address.IDFromAddress(providerIDAddr)
	if err != nil {
		return fmt.Errorf("failed to get provider actor ID: %w", err)
	}

	// Create the voucher message using the common implementation.
	voucher, err := common.CreateProviderVoucher(cctx.Context, full, providerID, cumulativeAmount.Int, nonce)
	if err != nil {
		return fmt.Errorf("create provider voucher failed: %w", err)
	}

	// Output the voucher as a hex-encoded string.
	fmt.Println("Provider Voucher:", hex.EncodeToString(voucher))

	svcAddr, err := full.StateAccountKey(cctx.Context, common.Service, types.EmptyTSK)
	if err != nil {
		return fmt.Errorf("get service address failed: %w", err)
	}

	sig, err := full.WalletSign(cctx.Context, svcAddr, voucher)
	if err != nil {
		return fmt.Errorf("sign voucher failed: %w", err)
	}

	fmt.Println("Provider Voucher Signature:", hex.EncodeToString(sig.Data))

	ok, err := full.WalletVerify(cctx.Context, svcAddr, voucher, sig)
	if err != nil {
		return fmt.Errorf("verify voucher failed: %w", err)
	}
	fmt.Println("Verification result:", ok)
	return nil
}
