package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ipfs/go-cid"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"

	curioproof "github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/proofsvc"
	"github.com/filecoin-project/curio/lib/proofsvc/common"

	"github.com/filecoin-project/lotus/chain/types"
	cliutil "github.com/filecoin-project/lotus/cli/util"
)

type commit2In struct {
	SectorNum  int64  `json:"SectorNum"`
	Phase1Out  []byte `json:"Phase1Out"`
	SectorSize uint64 `json:"SectorSize"`
}

var proofsvcClientCmd = &cli.Command{
	Name:  "proofsvc-client",
	Usage: "Interact with the remote proof service",
	Subcommands: []*cli.Command{
		proofsvcCreateVoucherCmd,
		proofsvcSubmitCmd,
		proofsvcStatusCmd,
	},
}

var proofsvcCreateVoucherCmd = &cli.Command{
	Name:  "create-voucher",
	Usage: "Create a client voucher",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "client", Required: true},
		&cli.StringFlag{Name: "amount", Required: true},
	},
	Action: func(cctx *cli.Context) error {
		clientStr := cctx.String("client")
		amountStr := cctx.String("amount")

		full, closer, err := cliutil.GetFullNodeAPIV1(cctx)
		if err != nil {
			return err
		}
		defer closer()

		svc := common.NewService(full)

		clientAddr, err := address.NewFromString(clientStr)
		if err != nil {
			return err
		}

		cumulativeAmount, err := types.ParseFIL(amountStr)
		if err != nil {
			return err
		}

		idAddr, err := full.StateLookupID(cctx.Context, clientAddr, types.EmptyTSK)
		if err != nil {
			return err
		}
		clientID, err := address.IDFromAddress(idAddr)
		if err != nil {
			return err
		}

		state, err := svc.GetClientState(cctx.Context, clientID)
		if err != nil {
			return err
		}
		nonce := state.LastNonce + 1
		cumulativeAmount = types.FIL(types.BigAdd(types.BigInt(state.VoucherRedeemed), types.BigInt(cumulativeAmount)))

		voucher, err := svc.CreateClientVoucher(cctx.Context, clientID, cumulativeAmount.Int, nonce)
		if err != nil {
			return err
		}

		sig, err := full.WalletSign(cctx.Context, clientAddr, voucher)
		if err != nil {
			return err
		}

		output := struct {
			Voucher   string `json:"voucher"`
			Signature string `json:"signature"`
			Nonce     uint64 `json:"nonce"`
		}{
			Voucher:   hex.EncodeToString(voucher),
			Signature: hex.EncodeToString(sig.Data),
			Nonce:     nonce,
		}

		enc, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return err
		}
		fmt.Print(string(enc))
		return nil
	},
}

var proofsvcSubmitCmd = &cli.Command{
	Name:  "submit",
	Usage: "Submit a proof request",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "c1", Usage: "path to lotus-bench c1 json", Required: true},
		&cli.StringFlag{Name: "miner", Usage: "miner address", Required: true},
		&cli.Uint64Flag{Name: "client-id", Required: true},
		&cli.Uint64Flag{Name: "nonce", Required: true},
		&cli.StringFlag{Name: "amount", Required: true},
		&cli.StringFlag{Name: "sig", Required: true},
	},
	Action: func(cctx *cli.Context) error {
		inb, err := os.ReadFile(cctx.String("c1"))
		if err != nil {
			return err
		}
		var c2 commit2In
		if err := json.Unmarshal(inb, &c2); err != nil {
			return err
		}

		minerAddr, err := address.NewFromString(cctx.String("miner"))
		if err != nil {
			return err
		}
		idAddr, err := address.IDFromAddress(minerAddr)
		if err != nil {
			return err
		}

		c1raw, err := curioproof.DecodeCommit1OutRaw(bytes.NewReader(c2.Phase1Out))
		if err != nil {
			return xerrors.Errorf("decode c1: %w", err)
		}

		pd := common.ProofData{
			SectorID: &abi.SectorID{Miner: abi.ActorID(idAddr), Number: abi.SectorNumber(c2.SectorNum)},
			PoRep:    &c1raw,
		}

		pbytes, err := json.Marshal(pd)
		if err != nil {
			return err
		}

		ctx := context.Background()
		cid, err := proofsvc.UploadProofData(ctx, pbytes)
		if err != nil {
			return err
		}

		amt, err := types.ParseFIL(cctx.String("amount"))
		if err != nil {
			return err
		}
		sigBytes, err := hex.DecodeString(cctx.String("sig"))
		if err != nil {
			return err
		}

		pr := common.ProofRequest{
			Data:                    cid,
			PriceEpoch:              0,
			PaymentClientID:         int64(cctx.Uint64("client-id")),
			PaymentNonce:            int64(cctx.Uint64("nonce")),
			PaymentCumulativeAmount: abi.NewTokenAmount(amt.Int64()),
			PaymentSignature:        sigBytes,
		}

		_, err = proofsvc.RequestProof(pr)
		if err != nil {
			return err
		}

		fmt.Println("request-id:", cid.String())
		return nil
	},
}

var proofsvcStatusCmd = &cli.Command{
	Name:  "status",
	Usage: "Check proof status",
	Flags: []cli.Flag{
		&cli.StringFlag{Name: "id", Required: true},
	},
	Action: func(cctx *cli.Context) error {
		rcid, err := cid.Parse(cctx.String("id"))
		if err != nil {
			return err
		}
		resp, err := proofsvc.GetProofStatus(rcid)
		if err != nil {
			return err
		}
		b, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return err
		}
		fmt.Print(string(b))
		return nil
	},
}
