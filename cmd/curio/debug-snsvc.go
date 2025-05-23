package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/proofsvc/common"

	"github.com/filecoin-project/lotus/chain/types"
	cliutil "github.com/filecoin-project/lotus/cli/util"
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
			Name:  "client-initiate-withdrawal",
			Usage: "Initiate a withdrawal request from the client's deposit",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:     "from",
					Usage:    "Client sender address",
					Required: true,
				},
				&cli.StringFlag{
					Name:     "amount",
					Usage:    "Withdrawal amount (in FIL)",
					Required: true,
				},
			},
			Action: clientInitiateWithdrawalAction,
		},
		{
			Name:  "client-complete-withdrawal",
			Usage: "Complete a pending client withdrawal after the withdrawal window elapses",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:     "from",
					Usage:    "Client sender address",
					Required: true,
				},
			},
			Action: clientCompleteWithdrawalAction,
		},
		{
			Name:  "client-cancel-withdrawal",
			Usage: "Cancel a pending client withdrawal request",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:     "from",
					Usage:    "Client sender address",
					Required: true,
				},
			},
			Action: clientCancelWithdrawalAction,
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
			Name:  "service-initiate-withdrawal",
			Usage: "Initiate a withdrawal request from the service pool",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:     "amount",
					Usage:    "Withdrawal amount (in FIL)",
					Required: true,
				},
				&cli.StringFlag{Name: "from", Usage: "Service sender address", Required: true},
			},
			Action: serviceInitiateWithdrawalAction,
		},
		{
			Name:  "service-complete-withdrawal",
			Usage: "Complete a pending service withdrawal after the withdrawal window elapses",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "from", Usage: "Service sender address", Required: true},
			},
			Action: serviceCompleteWithdrawalAction,
		},
		{
			Name:  "service-cancel-withdrawal",
			Usage: "Cancel a pending service withdrawal request",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "from", Usage: "Service sender address", Required: true},
			},
			Action: serviceCancelWithdrawalAction,
		},
		{
			Name:  "service-deposit",
			Usage: "Deposit funds into the service pool (service role)",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "from", Usage: "Service sender address", Required: true},
				&cli.StringFlag{Name: "amount", Usage: "Amount to deposit (FIL)", Required: true},
			},
			Action: serviceDepositAction,
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
			Name:  "create-client-voucher",
			Usage: "Create a client voucher",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "client", Usage: "Client actor address", Required: true},
				&cli.StringFlag{Name: "amount", Usage: "Amount to redeem (FIL)", Required: true},
			},
			Action: createClientVoucherAction,
		},
		{
			Name:  "create-provider-voucher",
			Usage: "Create a provider voucher",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "provider", Usage: "Provider actor address", Required: true},
				&cli.StringFlag{Name: "amount", Usage: "Amount to redeem (FIL)", Required: true},
				&cli.Uint64Flag{Name: "nonce", Usage: "Voucher nonce", Required: true},
				&cli.StringFlag{Name: "service", Usage: "Service sender address", Required: true},
			},
			Action: createProviderVoucherAction,
		},
		{
			Name:  "propose-service-actor",
			Usage: "Propose a new service actor",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "from", Usage: "Service sender address", Required: true},
				&cli.StringFlag{Name: "new-service-actor", Usage: "New service actor address", Required: true},
			},
			Action: proposeServiceActorAction,
		},
		{
			Name:  "accept-service-actor",
			Usage: "Accept a proposed service actor",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "from", Usage: "Service sender address", Required: true},
			},
			Action: acceptServiceActorAction,
		},
		{
			Name:  "validate-client-voucher",
			Usage: "Validate a client voucher signature",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "client", Usage: "Client actor address", Required: true},
				&cli.StringFlag{Name: "amount", Usage: "Cumulative amount (FIL)", Required: true},
				&cli.Uint64Flag{Name: "nonce", Usage: "Voucher nonce", Required: true},
				&cli.StringFlag{Name: "sig", Usage: "Voucher signature (hex)", Required: true},
			},
			Action: validateClientVoucherAction,
		},
		{
			Name:  "validate-provider-voucher",
			Usage: "Validate a provider voucher signature",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "provider", Usage: "Provider actor address", Required: true},
				&cli.StringFlag{Name: "amount", Usage: "Cumulative amount (FIL)", Required: true},
				&cli.Uint64Flag{Name: "nonce", Usage: "Voucher nonce", Required: true},
				&cli.StringFlag{Name: "sig", Usage: "Voucher signature (hex)", Required: true},
			},
			Action: validateProviderVoucherAction,
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

	svc := common.NewService(full)

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

	depositCid, err := svc.ClientDeposit(ctx, fromAddr, abi.TokenAmount(filAmount))
	if err != nil {
		return fmt.Errorf("deposit failed: %w", err)
	}

	fmt.Println("Deposit succeeded", depositCid)
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

	svc := common.NewService(full)

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

	if _, err := svc.ServiceRedeemClientVoucher(ctx, fromAddr, clientID, abi.TokenAmount(filAmount), nonce, sig); err != nil {
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

	svc := common.NewService(full)

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

	if err := svc.ServiceRedeemProviderVoucher(ctx, fromAddr, providerID, abi.TokenAmount(filAmount), nonce, sig); err != nil {
		return fmt.Errorf("redeem provider voucher failed: %w", err)
	}
	fmt.Println("Redeem provider voucher succeeded")
	return nil
}

// clientInitiateWithdrawalAction submits a transaction to call `initiateClientWithdrawal(amount)`.
// It deducts the withdrawal amount from the client’s deposit (subject to the withdrawal window).
func clientInitiateWithdrawalAction(cctx *cli.Context) error {
	ctx := context.Background()
	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	svc := common.NewService(full)

	fromStr := cctx.String("from")
	fromAddr, err := address.NewFromString(fromStr)
	if err != nil {
		return fmt.Errorf("invalid client from address: %w", err)
	}

	amountStr := cctx.String("amount")
	filAmount, err := types.ParseFIL(amountStr)
	if err != nil {
		return fmt.Errorf("invalid withdrawal amount: %w", err)
	}

	initiateCid, err := svc.ClientInitiateWithdrawal(ctx, fromAddr, abi.TokenAmount(filAmount))
	if err != nil {
		return fmt.Errorf("client initiate withdrawal failed: %w", err)
	}

	fmt.Println("Client withdrawal initiated successfully", initiateCid)
	return nil
}

// clientCompleteWithdrawalAction completes a previously initiated client withdrawal.
func clientCompleteWithdrawalAction(cctx *cli.Context) error {
	ctx := context.Background()
	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	svc := common.NewService(full)

	fromStr := cctx.String("from")
	fromAddr, err := address.NewFromString(fromStr)
	if err != nil {
		return fmt.Errorf("invalid client from address: %w", err)
	}

	completeCid, err := svc.ClientCompleteWithdrawal(ctx, fromAddr)
	if err != nil {
		return fmt.Errorf("client complete withdrawal failed: %w", err)
	}

	fmt.Println("Client withdrawal completed successfully", completeCid)
	return nil
}

// clientCancelWithdrawalAction cancels a pending client withdrawal.
func clientCancelWithdrawalAction(cctx *cli.Context) error {
	ctx := context.Background()
	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	svc := common.NewService(full)

	fromStr := cctx.String("from")
	fromAddr, err := address.NewFromString(fromStr)
	if err != nil {
		return fmt.Errorf("invalid client from address: %w", err)
	}

	cancelCid, err := svc.ClientCancelWithdrawal(ctx, fromAddr)
	if err != nil {
		return fmt.Errorf("client cancel withdrawal failed: %w", err)
	}

	fmt.Println("Client withdrawal canceled successfully", cancelCid)
	return nil
}

// serviceInitiateWithdrawalAction submits a transaction to call `initiateServiceWithdrawal(amount)`.
// Note that the service actor is hardcoded (via the common.Service variable).
func serviceInitiateWithdrawalAction(cctx *cli.Context) error {
	ctx := context.Background()
	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	svc := common.NewService(full)

	amountStr := cctx.String("amount")
	filAmount, err := types.ParseFIL(amountStr)
	if err != nil {
		return fmt.Errorf("invalid withdrawal amount: %w", err)
	}

	fromStr := cctx.String("from")
	fromAddr, err := address.NewFromString(fromStr)
	if err != nil {
		return fmt.Errorf("invalid service from address: %w", err)
	}

	if err := svc.ServiceInitiateWithdrawal(ctx, fromAddr, abi.TokenAmount(filAmount)); err != nil {
		return fmt.Errorf("service initiate withdrawal failed: %w", err)
	}

	fmt.Println("Service withdrawal initiated successfully")
	return nil
}

// serviceCompleteWithdrawalAction completes a previously initiated service withdrawal.
func serviceCompleteWithdrawalAction(cctx *cli.Context) error {
	ctx := context.Background()
	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	svc := common.NewService(full)

	fromStr := cctx.String("from")
	fromAddr, err := address.NewFromString(fromStr)
	if err != nil {
		return fmt.Errorf("invalid service from address: %w", err)
	}

	if err := svc.ServiceCompleteWithdrawal(ctx, fromAddr); err != nil {
		return fmt.Errorf("service complete withdrawal failed: %w", err)
	}

	fmt.Println("Service withdrawal completed successfully")
	return nil
}

// serviceCancelWithdrawalAction cancels a pending service withdrawal.
func serviceCancelWithdrawalAction(cctx *cli.Context) error {
	ctx := context.Background()
	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	svc := common.NewService(full)

	fromStr := cctx.String("from")
	fromAddr, err := address.NewFromString(fromStr)
	if err != nil {
		return fmt.Errorf("invalid service from address: %w", err)
	}

	if err := svc.ServiceCancelWithdrawal(ctx, fromAddr); err != nil {
		return fmt.Errorf("service cancel withdrawal failed: %w", err)
	}

	fmt.Println("Service withdrawal canceled successfully")
	return nil
}

// serviceDepositAction allows the service to deposit funds into the service pool.
func serviceDepositAction(cctx *cli.Context) error {
	ctx := context.Background()
	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	svc := common.NewService(full)

	amountStr := cctx.String("amount")
	filAmount, err := types.ParseFIL(amountStr)
	if err != nil {
		return fmt.Errorf("invalid deposit amount: %w", err)
	}

	fromStr := cctx.String("from")
	fromAddr, err := address.NewFromString(fromStr)
	if err != nil {
		return fmt.Errorf("invalid service from address: %w", err)
	}

	if err := svc.ServiceDeposit(ctx, fromAddr, abi.TokenAmount(filAmount)); err != nil {
		return fmt.Errorf("service deposit failed: %w", err)
	}
	fmt.Println("Service deposit succeeded")
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

	svc := common.NewService(full)

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

	clientState, err := svc.GetClientState(ctx, clientID)
	if err != nil {
		return fmt.Errorf("get client state failed: %w", err)
	}
	fmt.Printf("Client: %s\n", clientAddrStr)
	fmt.Printf("Balance: %s\n", types.FIL(clientState.Balance).String())
	fmt.Printf("VoucherRedeemed: %s\n", types.FIL(clientState.VoucherRedeemed).String())
	fmt.Printf("LastNonce: %d\n", clientState.LastNonce)
	fmt.Printf("WithdrawAmount: %s\n", types.FIL(clientState.WithdrawAmount).String())
	ts := clientState.WithdrawTimestamp.Int.Uint64()
	wt := time.Unix(int64(ts), 0)
	diff := time.Until(wt)
	var diffStr string
	if diff >= 0 {
		diffStr = fmt.Sprintf("in %s", diff)
	} else {
		diffStr = fmt.Sprintf("%s ago", -diff)
	}
	fmt.Printf("WithdrawTime: %s (%s)\n", wt.Format(time.RFC3339), diffStr)
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

	svc := common.NewService(full)

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

	voucherRedeemed, lastNonce, err := svc.GetProviderState(ctx, providerID)
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

	svc := common.NewService(full)

	serviceActor, pool, pendingWithdrawalAmount, pendingWithdrawalTimestamp, proposedServiceActor, actorChangeTimestamp, err := svc.GetServiceState(ctx)
	if err != nil {
		return fmt.Errorf("get service state failed: %w", err)
	}
	fmt.Printf("Service State:\n")
	fmt.Printf("  ServiceActor: %d\n", serviceActor)
	fmt.Printf("  Pool: %s\n", types.FIL(pool).String())
	fmt.Printf("  PendingWithdrawalAmount: %s\n", types.FIL(pendingWithdrawalAmount).String())
	fmt.Printf("  Pending Withdrawal Timestamp: %s\n", time.Unix(types.BigInt(pendingWithdrawalTimestamp).Int64(), 0).Format(time.RFC1123))
	fmt.Printf("  Proposed Service Actor: %d\n", proposedServiceActor)
	fmt.Printf("  Actor Change Timestamp: %s\n", time.Unix(types.BigInt(actorChangeTimestamp).Int64(), 0).Format(time.RFC1123))
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

	svc := common.NewService(full)

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
		clientState, err := svc.GetClientState(cctx.Context, clientID)
		if err != nil {
			return fmt.Errorf("get client state failed: %w", err)
		}
		fmt.Printf("Client %s: Balance: %s, VoucherRedeemed: %s, LastNonce: %d, WithdrawAmount: %s, WithdrawTimestamp: %d\n",
			clientAddr.String(),
			types.FIL(clientState.Balance).String(),
			types.FIL(clientState.VoucherRedeemed).String(),
			clientState.LastNonce,
			types.FIL(clientState.WithdrawAmount).String(),
			types.BigInt(clientState.WithdrawTimestamp).Int64())
		nonce = clientState.LastNonce + 1
		cumulativeAmount = types.FIL(types.BigAdd(types.BigInt(clientState.VoucherRedeemed), types.BigInt(cumulativeAmount)))
	}

	fmt.Printf("Creating client voucher for client %s with cumulative amount %s and nonce %d\n",
		clientAddr.String(), types.FIL(cumulativeAmount).String(), nonce)

	// Create the voucher message using the common implementation.
	voucher, err := svc.CreateClientVoucher(cctx.Context, clientID, cumulativeAmount.Int, nonce)
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

	svc := common.NewService(full)

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
	voucher, err := svc.CreateProviderVoucher(cctx.Context, providerID, cumulativeAmount.Int, nonce)
	if err != nil {
		return fmt.Errorf("create provider voucher failed: %w", err)
	}

	// Output the voucher as a hex-encoded string.
	fmt.Println("Provider Voucher:", hex.EncodeToString(voucher))

	service, err := address.NewFromString(cctx.String("service"))
	if err != nil {
		return fmt.Errorf("invalid service address: %w", err)
	}

	svcAddr, err := full.StateAccountKey(cctx.Context, service, types.EmptyTSK)
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

func proposeServiceActorAction(cctx *cli.Context) error {
	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	svc := common.NewService(full)

	fromStr := cctx.String("from")
	fromAddr, err := address.NewFromString(fromStr)
	if err != nil {
		return fmt.Errorf("invalid service from address: %w", err)
	}

	newServiceActorStr := cctx.String("new-service-actor")
	newServiceActorAddr, err := address.NewFromString(newServiceActorStr)
	if err != nil {
		return fmt.Errorf("invalid new service actor address: %w", err)
	}

	if err := svc.ProposeServiceActor(cctx.Context, fromAddr, newServiceActorAddr); err != nil {
		return fmt.Errorf("propose service actor failed: %w", err)
	}

	fmt.Println("Service actor proposed successfully")
	return nil
}

func acceptServiceActorAction(cctx *cli.Context) error {
	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	svc := common.NewService(full)

	fromStr := cctx.String("from")
	fromAddr, err := address.NewFromString(fromStr)
	if err != nil {
		return fmt.Errorf("invalid service from address: %w", err)
	}

	if err := svc.AcceptServiceActor(cctx.Context, fromAddr); err != nil {
		return fmt.Errorf("accept service actor failed: %w", err)
	}

	fmt.Println("Service actor accepted successfully")
	return nil
}

func validateClientVoucherAction(cctx *cli.Context) error {
	// Retrieve command-line flags.
	clientStr := cctx.String("client")
	amountStr := cctx.String("amount")
	nonce := cctx.Uint64("nonce")
	sigHex := cctx.String("sig")

	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	svc := common.NewService(full)

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

	fmt.Printf("Validating client voucher for client %s with cumulative amount %s and nonce %d\n",
		clientAddr.String(), types.FIL(cumulativeAmount).String(), nonce)

	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	// Use the ValidateClientVoucher method from the Service struct
	isValid, err := svc.ValidateClientVoucher(cctx.Context, clientID, cumulativeAmount.Int, nonce, sig)
	if err != nil {
		return fmt.Errorf("validate client voucher failed: %w", err)
	}

	fmt.Println("Validation result:", isValid)
	return nil
}

func validateProviderVoucherAction(cctx *cli.Context) error {
	// Retrieve command-line flags.
	providerStr := cctx.String("provider")
	amountStr := cctx.String("amount")
	nonce := cctx.Uint64("nonce")
	sigHex := cctx.String("sig")

	full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
	if err != nil {
		return err
	}
	defer closer()

	svc := common.NewService(full)

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

	// Convert the provider address to its actor ID.
	providerID, err := address.IDFromAddress(providerIDAddr)
	if err != nil {
		return fmt.Errorf("failed to get provider actor ID: %w", err)
	}

	fmt.Printf("Validating provider voucher for provider %d with cumulative amount %s and nonce %d\n",
		providerID, types.FIL(cumulativeAmount).String(), nonce)

	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return fmt.Errorf("invalid signature: %w", err)
	}

	// Use the ValidateProviderVoucher method from the Service struct
	isValid, err := svc.ValidateProviderVoucher(cctx.Context, providerID, cumulativeAmount.Int, nonce, sig)
	if err != nil {
		return fmt.Errorf("validate provider voucher failed: %w", err)
	}

	fmt.Println("Validation result:", isValid)
	return nil
}
