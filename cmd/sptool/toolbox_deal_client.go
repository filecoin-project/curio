package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/dustin/go-humanize"
	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	inet "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/mitchellh/go-homedir"
	"github.com/multiformats/go-multiaddr"
	"github.com/urfave/cli/v2"
	"golang.org/x/term"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	cborutil "github.com/filecoin-project/go-cbor-util"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin/v9/market"

	"github.com/filecoin-project/curio/lib/keystore"
	mk12_libp2p "github.com/filecoin-project/curio/market/libp2p"
	"github.com/filecoin-project/curio/market/mk12"

	"github.com/filecoin-project/lotus/api"
	chain_types "github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/chain/wallet"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/lib/tablewriter"
)

type Node struct {
	Host   host.Host
	Wallet *wallet.LocalWallet
}

func Setup(cfgdir string) (*Node, error) {
	cfgdir, err := homedir.Expand(cfgdir)
	if err != nil {
		return nil, xerrors.Errorf("getting homedir: %w", err)
	}

	_, err = os.Stat(cfgdir)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("repo dir doesn't exist. run `sptool mk12-client init` first")
	}

	peerkey, err := loadOrInitPeerKey(keyPath(cfgdir))
	if err != nil {
		return nil, err
	}

	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.Identity(peerkey),
	)

	if err != nil {
		return nil, err
	}

	wallet, err := setupWallet(walletPath(cfgdir))
	if err != nil {
		return nil, err
	}

	return &Node{
		Host:   h,
		Wallet: wallet,
	}, nil
}

func loadOrInitPeerKey(kf string) (crypto.PrivKey, error) {
	data, err := os.ReadFile(kf)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}

		k, _, err := crypto.GenerateEd25519Key(rand.Reader)
		if err != nil {
			return nil, err
		}

		data, err := crypto.MarshalPrivateKey(k)
		if err != nil {
			return nil, err
		}

		if err := os.WriteFile(kf, data, 0600); err != nil {
			return nil, err
		}

		return k, nil
	}
	return crypto.UnmarshalPrivateKey(data)
}

func setupWallet(dir string) (*wallet.LocalWallet, error) {
	kstore, err := keystore.OpenOrInitKeystore(dir)
	if err != nil {
		return nil, err
	}

	wallet, err := wallet.NewWallet(kstore)
	if err != nil {
		return nil, err
	}

	addrs, err := wallet.WalletList(context.TODO())
	if err != nil {
		return nil, err
	}

	if len(addrs) == 0 {
		_, err := wallet.WalletNew(context.TODO(), chain_types.KTBLS)
		if err != nil {
			return nil, err
		}
	}

	return wallet, nil
}

func keyPath(baseDir string) string {
	return filepath.Join(baseDir, "libp2p.key")
}

func walletPath(baseDir string) string {
	return filepath.Join(baseDir, "wallet")
}

func (n *Node) GetProvidedOrDefaultWallet(ctx context.Context, provided string) (address.Address, error) {
	var walletAddr address.Address
	if provided == "" {
		var err error
		walletAddr, err = n.Wallet.GetDefault()
		if err != nil {
			return address.Address{}, err
		}
	} else {
		w, err := address.NewFromString(provided)
		if err != nil {
			return address.Address{}, err
		}

		addrs, err := n.Wallet.WalletList(ctx)
		if err != nil {
			return address.Address{}, err
		}

		found := false
		for _, a := range addrs {
			if bytes.Equal(a.Bytes(), w.Bytes()) {
				walletAddr = w
				found = true
			}
		}

		if !found {
			return address.Address{}, xerrors.Errorf("couldn't find wallet %s locally", provided)
		}
	}

	return walletAddr, nil
}

func PrintJson(obj interface{}) error {
	resJson, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return xerrors.Errorf("marshalling json: %w", err)
	}

	fmt.Println(string(resJson))
	return nil
}

var mk12_client_repo = &cli.StringFlag{
	Name:    "mk12-client-repo",
	Usage:   "repo directory for mk12 client",
	Value:   "~/.curio-client",
	EnvVars: []string{"CURIO_MK12_CLIENT_REPO"},
}

var mk12Clientcmd = &cli.Command{
	Name:  "mk12-client",
	Usage: "mk12 client for Curio",
	Flags: []cli.Flag{
		mk12_client_repo,
	},
	Subcommands: []*cli.Command{
		initCmd,
		dealCmd,
		offlineDealCmd,
		marketAddCmd,
		marketWithdrawCmd,
		commpCmd,
		generateRandCar,
		walletCmd,
	},
}

var dealFlags = []cli.Flag{
	&cli.StringFlag{
		Name:     "provider",
		Usage:    "storage provider on-chain address",
		Required: true,
	},
	&cli.StringFlag{
		Name:     "commp",
		Usage:    "commp of the CAR file",
		Required: true,
	},
	&cli.Uint64Flag{
		Name:     "piece-size",
		Usage:    "size of the CAR file as a padded piece",
		Required: true,
	},
	&cli.StringFlag{
		Name:     "payload-cid",
		Usage:    "root CID of the CAR file",
		Required: true,
	},
	&cli.IntFlag{
		Name:  "start-epoch-head-offset",
		Usage: "start epoch by when the deal should be proved by provider on-chain after current chain head",
	},
	&cli.IntFlag{
		Name:  "start-epoch",
		Usage: "start epoch by when the deal should be proved by provider on-chain",
	},
	&cli.IntFlag{
		Name:  "duration",
		Usage: "duration of the deal in epochs",
		Value: 518400, // default is 2880 * 180 == 180 days
	},
	&cli.IntFlag{
		Name:  "provider-collateral",
		Usage: "deal collateral that storage miner must put in escrow; if empty, the min collateral for the given piece size will be used",
	},
	&cli.Int64Flag{
		Name:  "storage-price",
		Usage: "storage price in attoFIL per epoch per GiB",
		Value: 1,
	},
	&cli.BoolFlag{
		Name:  "verified",
		Usage: "whether the deal funds should come from verified client data-cap",
		Value: true,
	},
	&cli.BoolFlag{
		Name:  "remove-unsealed-copy",
		Usage: "indicates that an unsealed copy of the sector in not required for fast retrieval",
		Value: false,
	},
	&cli.StringFlag{
		Name:  "wallet",
		Usage: "wallet address to be used to initiate the deal",
	},
	&cli.BoolFlag{
		Name:  "skip-ipni-announce",
		Usage: "indicates that deal index should not be announced to the IPNI(Network Indexer)",
		Value: false,
	},
}

var dealCmd = &cli.Command{
	Name:  "deal",
	Usage: "Make an online deal with Curio",
	Flags: append([]cli.Flag{
		&cli.StringFlag{
			Name:     "http-url",
			Usage:    "http url to CAR file",
			Required: true,
		},
		&cli.StringSliceFlag{
			Name:  "http-headers",
			Usage: "http headers to be passed with the request (e.g key=value)",
		},
		&cli.Uint64Flag{
			Name:     "car-size",
			Usage:    "size of the CAR file: required for online deals",
			Required: true,
		},
	}, dealFlags...),
	Action: func(cctx *cli.Context) error {
		return dealCmdAction(cctx, true)
	},
}

var offlineDealCmd = &cli.Command{
	Name:  "offline-deal",
	Usage: "Make an offline deal with Curio",
	Flags: dealFlags,
	Action: func(cctx *cli.Context) error {
		return dealCmdAction(cctx, false)
	},
}

func dealCmdAction(cctx *cli.Context, isOnline bool) error {
	ctx := cctx.Context

	n, err := Setup(cctx.String(mk12_client_repo.Name))
	if err != nil {
		return err
	}

	api, closer, err := lcli.GetGatewayAPI(cctx)
	if err != nil {
		return xerrors.Errorf("cant setup gateway connection: %w", err)
	}
	defer closer()

	walletAddr, err := n.GetProvidedOrDefaultWallet(ctx, cctx.String("wallet"))
	if err != nil {
		return err
	}

	log.Debugw("selected wallet", "wallet", walletAddr)

	maddr, err := address.NewFromString(cctx.String("provider"))
	if err != nil {
		return err
	}

	minfo, err := api.StateMinerInfo(ctx, maddr, chain_types.EmptyTSK)
	if err != nil {
		return err
	}
	if minfo.PeerId == nil {
		return xerrors.Errorf("storage provider %s has no peer ID set on-chain", maddr)
	}

	var maddrs []multiaddr.Multiaddr
	for _, mma := range minfo.Multiaddrs {
		ma, err := multiaddr.NewMultiaddrBytes(mma)
		if err != nil {
			return xerrors.Errorf("storage provider %s had invalid multiaddrs in their info: %w", maddr, err)
		}
		maddrs = append(maddrs, ma)
	}
	if len(maddrs) == 0 {
		return xerrors.Errorf("storage provider %s has no multiaddrs set on-chain", maddr)
	}

	addrInfo := &peer.AddrInfo{
		ID:    *minfo.PeerId,
		Addrs: maddrs,
	}

	log.Debugw("found storage provider", "id", addrInfo.ID, "multiaddrs", addrInfo.Addrs, "addr", maddr)

	if err := n.Host.Connect(ctx, *addrInfo); err != nil {
		return xerrors.Errorf("failed to connect to peer %s: %w", addrInfo.ID, err)
	}

	x, err := n.Host.Peerstore().FirstSupportedProtocol(addrInfo.ID, mk12_libp2p.DealProtocolv121ID)
	if err != nil {
		return xerrors.Errorf("getting protocols for peer %s: %w", addrInfo.ID, err)
	}

	if len(x) == 0 {
		return xerrors.Errorf("curio client cannot make a deal with storage provider %s because it does not support protocol version 1.2.0", maddr)
	}

	dealUuid := uuid.New()

	commp := cctx.String("commp")
	pieceCid, err := cid.Parse(commp)
	if err != nil {
		return xerrors.Errorf("parsing commp '%s': %w", commp, err)
	}

	pieceSize := cctx.Uint64("piece-size")
	if pieceSize == 0 {
		return xerrors.Errorf("must provide piece-size parameter for CAR url")
	}

	payloadCidStr := cctx.String("payload-cid")
	rootCid, err := cid.Parse(payloadCidStr)
	if err != nil {
		return xerrors.Errorf("parsing payload cid %s: %w", payloadCidStr, err)
	}

	transfer := mk12.Transfer{}
	if isOnline {

		carFileSize := cctx.Uint64("car-size")
		if carFileSize == 0 {
			return xerrors.Errorf("size of car file cannot be 0")
		}

		transfer.Size = carFileSize
		// Store the path to the CAR file as a transfer parameter
		transferParams := &mk12.HttpRequest{URL: cctx.String("http-url")}

		if cctx.IsSet("http-headers") {
			transferParams.Headers = make(map[string]string)

			for _, header := range cctx.StringSlice("http-headers") {
				sp := strings.Split(header, "=")
				if len(sp) != 2 {
					return xerrors.Errorf("malformed http header: %s", header)
				}

				transferParams.Headers[sp[0]] = sp[1]
			}
		}

		paramsBytes, err := json.Marshal(transferParams)
		if err != nil {
			return xerrors.Errorf("marshalling request parameters: %w", err)
		}
		transfer.Type = "http"
		transfer.Params = paramsBytes
	}

	var providerCollateral abi.TokenAmount
	if cctx.IsSet("provider-collateral") {
		providerCollateral = abi.NewTokenAmount(cctx.Int64("provider-collateral"))
	} else {
		bounds, err := api.StateDealProviderCollateralBounds(ctx, abi.PaddedPieceSize(pieceSize), cctx.Bool("verified"), chain_types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("node error getting collateral bounds: %w", err)
		}

		providerCollateral = big.Div(big.Mul(bounds.Min, big.NewInt(6)), big.NewInt(5)) // add 20%
	}

	tipset, err := api.ChainHead(ctx)
	if err != nil {
		return xerrors.Errorf("cannot get chain head: %w", err)
	}

	head := tipset.Height()
	log.Debugw("current block height", "number", head)

	if cctx.IsSet("start-epoch") && cctx.IsSet("start-epoch-head-offset") {
		return errors.New("only one flag from `start-epoch-head-offset' or `start-epoch` can be specified")
	}

	var startEpoch abi.ChainEpoch

	if cctx.IsSet("start-epoch-head-offset") {
		startEpoch = head + abi.ChainEpoch(cctx.Int("start-epoch-head-offset"))
	} else if cctx.IsSet("start-epoch") {
		startEpoch = abi.ChainEpoch(cctx.Int("start-epoch"))
	} else {
		// default
		startEpoch = head + abi.ChainEpoch(5760) // head + 2 days
	}

	// Create a deal proposal to storage provider using deal protocol v1.2.0 format
	dealProposal, err := dealProposal(ctx, n, walletAddr, rootCid, abi.PaddedPieceSize(pieceSize), pieceCid, maddr, startEpoch, cctx.Int("duration"), cctx.Bool("verified"), providerCollateral, abi.NewTokenAmount(cctx.Int64("storage-price")))
	if err != nil {
		return xerrors.Errorf("failed to create a deal proposal: %w", err)
	}

	dealParams := mk12.DealParams{
		DealUUID:           dealUuid,
		ClientDealProposal: *dealProposal,
		DealDataRoot:       rootCid,
		IsOffline:          !isOnline,
		Transfer:           transfer,
		RemoveUnsealedCopy: cctx.Bool("remove-unsealed-copy"),
		SkipIPNIAnnounce:   cctx.Bool("skip-ipni-announce"),
	}

	log.Debugw("about to submit deal proposal", "uuid", dealUuid.String())

	s, err := n.Host.NewStream(ctx, addrInfo.ID, mk12_libp2p.DealProtocolv121ID)
	if err != nil {
		return xerrors.Errorf("failed to open stream to peer %s: %w", addrInfo.ID, err)
	}
	defer s.Close()

	var resp mk12.DealResponse
	if err := doRpc(ctx, s, &dealParams, &resp); err != nil {
		return xerrors.Errorf("send proposal rpc: %w", err)
	}

	if !resp.Accepted {
		return xerrors.Errorf("deal proposal rejected: %s", resp.Message)
	}

	if cctx.Bool("json") {
		out := map[string]interface{}{
			"dealUuid":           dealUuid.String(),
			"provider":           maddr.String(),
			"clientWallet":       walletAddr.String(),
			"payloadCid":         rootCid.String(),
			"commp":              dealProposal.Proposal.PieceCID.String(),
			"startEpoch":         dealProposal.Proposal.StartEpoch.String(),
			"endEpoch":           dealProposal.Proposal.EndEpoch.String(),
			"providerCollateral": dealProposal.Proposal.ProviderCollateral.String(),
		}
		if isOnline {
			out["url"] = cctx.String("http-url")
		}
		return PrintJson(out)
	}

	msg := "sent deal proposal"
	if !isOnline {
		msg += " for offline deal"
	}
	msg += "\n"
	msg += fmt.Sprintf("  deal uuid: %s\n", dealUuid)
	msg += fmt.Sprintf("  storage provider: %s\n", maddr)
	msg += fmt.Sprintf("  client wallet: %s\n", walletAddr)
	msg += fmt.Sprintf("  payload cid: %s\n", rootCid)
	if isOnline {
		msg += fmt.Sprintf("  url: %s\n", cctx.String("http-url"))
	}
	msg += fmt.Sprintf("  commp: %s\n", dealProposal.Proposal.PieceCID)
	msg += fmt.Sprintf("  start epoch: %d\n", dealProposal.Proposal.StartEpoch)
	msg += fmt.Sprintf("  end epoch: %d\n", dealProposal.Proposal.EndEpoch)
	msg += fmt.Sprintf("  provider collateral: %s\n", chain_types.FIL(dealProposal.Proposal.ProviderCollateral).Short())
	fmt.Println(msg)

	return nil
}

func dealProposal(ctx context.Context, n *Node, clientAddr address.Address, rootCid cid.Cid, pieceSize abi.PaddedPieceSize, pieceCid cid.Cid, minerAddr address.Address, startEpoch abi.ChainEpoch, duration int, verified bool, providerCollateral abi.TokenAmount, storagePrice abi.TokenAmount) (*market.ClientDealProposal, error) {
	endEpoch := startEpoch + abi.ChainEpoch(duration)
	// deal proposal expects total storage price for deal per epoch, therefore we
	// multiply pieceSize * storagePrice (which is set per epoch per GiB) and divide by 2^30
	storagePricePerEpochForDeal := big.Div(big.Mul(big.NewInt(int64(pieceSize)), storagePrice), big.NewInt(int64(1<<30)))
	l, err := market.NewLabelFromString(rootCid.String())
	if err != nil {
		return nil, err
	}
	proposal := market.DealProposal{
		PieceCID:             pieceCid,
		PieceSize:            pieceSize,
		VerifiedDeal:         verified,
		Client:               clientAddr,
		Provider:             minerAddr,
		Label:                l,
		StartEpoch:           startEpoch,
		EndEpoch:             endEpoch,
		StoragePricePerEpoch: storagePricePerEpochForDeal,
		ProviderCollateral:   providerCollateral,
	}

	buf, err := cborutil.Dump(&proposal)
	if err != nil {
		return nil, err
	}

	sig, err := n.Wallet.WalletSign(ctx, clientAddr, buf, api.MsgMeta{Type: api.MTDealProposal})
	if err != nil {
		return nil, xerrors.Errorf("wallet sign failed: %w", err)
	}

	return &market.ClientDealProposal{
		Proposal:        proposal,
		ClientSignature: *sig,
	}, nil
}

func doRpc(ctx context.Context, s inet.Stream, req interface{}, resp interface{}) error {
	errc := make(chan error)
	go func() {
		if err := cborutil.WriteCborRPC(s, req); err != nil {
			errc <- xerrors.Errorf("failed to send request: %w", err)
			return
		}

		if err := cborutil.ReadCborRPC(s, resp); err != nil {
			errc <- xerrors.Errorf("failed to read response: %w", err)
			return
		}

		errc <- nil
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

var initCmd = &cli.Command{
	Name:  "init",
	Usage: "Initialise curio mk12 client repo",
	Action: func(cctx *cli.Context) error {
		ctx := lcli.ReqContext(cctx)

		sdir, err := homedir.Expand(cctx.String(mk12_client_repo.Name))
		if err != nil {
			return err
		}

		os.Mkdir(sdir, 0755) //nolint:errcheck

		n, err := Setup(cctx.String(mk12_client_repo.Name))
		if err != nil {
			return err
		}

		api, closer, err := lcli.GetGatewayAPI(cctx)
		if err != nil {
			return xerrors.Errorf("can not setup gateway connection: %w", err)
		}
		defer closer()

		walletAddr, err := n.Wallet.GetDefault()
		if err != nil {
			return err
		}

		log.Infow("default wallet set", "wallet", walletAddr)

		walletBalance, err := api.WalletBalance(ctx, walletAddr)
		if err != nil {
			return err
		}

		log.Infow("wallet balance", "value", chain_types.FIL(walletBalance).Short())

		marketBalance, err := api.StateMarketBalance(ctx, walletAddr, chain_types.EmptyTSK)
		if err != nil {
			if strings.Contains(err.Error(), "actor not found") {
				log.Warn("market actor is not initialised, you must add funds to it in order to send online deals")

				return nil
			}
			return err
		}

		log.Infow("market balance", "escrow", chain_types.FIL(marketBalance.Escrow).Short(), "locked", chain_types.FIL(marketBalance.Locked).Short())

		return nil
	},
}

var walletCmd = &cli.Command{
	Name:  "wallet",
	Usage: "Manage mk12 client wallets",
	Subcommands: []*cli.Command{
		walletNew,
		walletList,
		walletBalance,
		walletExport,
		walletImport,
		walletGetDefault,
		walletSetDefault,
		walletDelete,
		walletSign,
	},
}

var walletNew = &cli.Command{
	Name:      "new",
	Usage:     "Generate a new key of the given type",
	ArgsUsage: "[bls|secp256k1|delegated (default secp256k1)]",
	Action: func(cctx *cli.Context) error {
		ctx := lcli.ReqContext(cctx)

		n, err := Setup(cctx.String(mk12_client_repo.Name))
		if err != nil {
			return err
		}

		t := cctx.Args().First()
		if t == "" {
			t = "secp256k1"
		}

		nk, err := n.Wallet.WalletNew(ctx, chain_types.KeyType(t))
		if err != nil {
			return err
		}

		if cctx.Bool("json") {
			out := map[string]interface{}{
				"address": nk.String(),
			}
			PrintJson(out) //nolint:errcheck
		} else {
			fmt.Println(nk.String())
		}

		return nil
	},
}

var walletList = &cli.Command{
	Name:  "list",
	Usage: "List wallet address",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "addr-only",
			Usage:   "Only print addresses",
			Aliases: []string{"a"},
		},
		&cli.BoolFlag{
			Name:    "id",
			Usage:   "Output ID addresses",
			Aliases: []string{"i"},
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := lcli.ReqContext(cctx)

		n, err := Setup(cctx.String(mk12_client_repo.Name))
		if err != nil {
			return err
		}

		api, closer, err := lcli.GetGatewayAPI(cctx)
		if err != nil {
			return xerrors.Errorf("cant setup gateway connection: %w", err)
		}
		defer closer()

		addrs, err := n.Wallet.WalletList(ctx)
		if err != nil {
			return err
		}

		// Assume an error means no default key is set
		def, _ := n.Wallet.GetDefault()

		// Map Keys. Corresponds to the standard tablewriter output
		addressKey := "Address"
		idKey := "ID"
		balanceKey := "Balance"
		marketKey := "market" // for json only
		marketAvailKey := "Market(Avail)"
		marketLockedKey := "Market(Locked)"
		nonceKey := "Nonce"
		defaultKey := "Default"
		errorKey := "Error"
		dataCapKey := "DataCap"

		// One-to-one mapping between tablewriter keys and JSON keys
		tableKeysToJsonKeys := map[string]string{
			addressKey: strings.ToLower(addressKey),
			idKey:      strings.ToLower(idKey),
			balanceKey: strings.ToLower(balanceKey),
			marketKey:  marketKey, // only in JSON
			nonceKey:   strings.ToLower(nonceKey),
			defaultKey: strings.ToLower(defaultKey),
			errorKey:   strings.ToLower(errorKey),
			dataCapKey: strings.ToLower(dataCapKey),
		}

		// List of Maps whose keys are defined above. One row = one list element = one wallet
		var wallets []map[string]interface{}

		for _, addr := range addrs {
			if cctx.Bool("addr-only") {
				fmt.Println(addr.String())
			} else {
				a, err := api.StateGetActor(ctx, addr, chain_types.EmptyTSK)
				if err != nil {
					if !strings.Contains(err.Error(), "actor not found") {
						wallet := map[string]interface{}{
							addressKey: addr,
							errorKey:   err,
						}
						wallets = append(wallets, wallet)
						continue
					}

					a = &chain_types.Actor{
						Balance: big.Zero(),
					}
				}

				wallet := map[string]interface{}{
					addressKey: addr,
					balanceKey: chain_types.FIL(a.Balance),
					nonceKey:   a.Nonce,
				}

				if cctx.Bool("json") {
					if addr == def {
						wallet[defaultKey] = true
					} else {
						wallet[defaultKey] = false
					}
				} else {
					if addr == def {
						wallet[defaultKey] = "X"
					}
				}

				if cctx.Bool("id") {
					id, err := api.StateLookupID(ctx, addr, chain_types.EmptyTSK)
					if err != nil {
						wallet[idKey] = "n/a"
					} else {
						wallet[idKey] = id
					}
				}

				mbal, err := api.StateMarketBalance(ctx, addr, chain_types.EmptyTSK)
				if err == nil {
					marketAvailValue := chain_types.FIL(chain_types.BigSub(mbal.Escrow, mbal.Locked))
					marketLockedValue := chain_types.FIL(mbal.Locked)
					// structure is different for these particular keys so we have to distinguish the cases here
					if cctx.Bool("json") {
						wallet[marketKey] = map[string]interface{}{
							"available": marketAvailValue,
							"locked":    marketLockedValue,
						}
					} else {
						wallet[marketAvailKey] = marketAvailValue
						wallet[marketLockedKey] = marketLockedValue
					}
				}
				dcap, err := api.StateVerifiedClientStatus(ctx, addr, chain_types.EmptyTSK)
				if err == nil {
					wallet[dataCapKey] = dcap
					if !cctx.Bool("json") && dcap == nil {
						wallet[dataCapKey] = "X"
					} else if dcap != nil {
						wallet[dataCapKey] = humanize.IBytes(dcap.Int.Uint64())
					}
				} else {
					wallet[dataCapKey] = "n/a"
					if cctx.Bool("json") {
						wallet[dataCapKey] = nil
					}
				}

				wallets = append(wallets, wallet)
			}
		}

		if !cctx.Bool("addr-only") {

			if cctx.Bool("json") {
				// get a new list of wallets with json keys instead of tablewriter keys
				var jsonWallets []map[string]interface{}
				for _, wallet := range wallets {
					jsonWallet := make(map[string]interface{})
					for k, v := range wallet {
						jsonWallet[tableKeysToJsonKeys[k]] = v
					}
					jsonWallets = append(jsonWallets, jsonWallet)
				}
				// then return this!
				return PrintJson(jsonWallets)
			} else {
				// Init the tablewriter's columns
				tw := tablewriter.New(
					tablewriter.Col(addressKey),
					tablewriter.Col(idKey),
					tablewriter.Col(balanceKey),
					tablewriter.Col(marketAvailKey),
					tablewriter.Col(marketLockedKey),
					tablewriter.Col(nonceKey),
					tablewriter.Col(defaultKey),
					tablewriter.NewLineCol(errorKey))
				// populate it with content
				for _, wallet := range wallets {
					tw.Write(wallet)
				}
				// return the corresponding string
				return tw.Flush(os.Stdout)
			}
		}

		return nil
	},
}

var walletBalance = &cli.Command{
	Name:      "balance",
	Usage:     "Get account balance",
	ArgsUsage: "[address]",
	Action: func(cctx *cli.Context) error {
		ctx := lcli.ReqContext(cctx)

		n, err := Setup(cctx.String(mk12_client_repo.Name))
		if err != nil {
			return err
		}

		api, closer, err := lcli.GetGatewayAPI(cctx)
		if err != nil {
			return xerrors.Errorf("cant setup gateway connection: %w", err)
		}
		defer closer()

		var addr address.Address
		if cctx.Args().First() != "" {
			addr, err = address.NewFromString(cctx.Args().First())
		} else {
			addr, err = n.Wallet.GetDefault()
		}
		if err != nil {
			return err
		}

		balance, err := api.WalletBalance(ctx, addr)
		if err != nil {
			return err
		}

		if balance.Equals(chain_types.NewInt(0)) {
			warningMessage := "may display 0 if chain sync in progress"
			if cctx.Bool("json") {
				out := map[string]interface{}{
					"balance": chain_types.FIL(balance),
					"warning": warningMessage,
				}
				return PrintJson(out)
			} else {
				fmt.Printf("%s (warning: %s)\n\n", chain_types.FIL(balance), warningMessage)
			}
		} else {
			if cctx.Bool("json") {
				out := map[string]interface{}{
					"balance": chain_types.FIL(balance),
				}
				return PrintJson(out)
			} else {
				fmt.Printf("%s\n", chain_types.FIL(balance))
			}
		}

		return nil
	},
}

var walletExport = &cli.Command{
	Name:      "export",
	Usage:     "export keys",
	ArgsUsage: "[address]",
	Action: func(cctx *cli.Context) error {
		ctx := lcli.ReqContext(cctx)

		n, err := Setup(cctx.String(mk12_client_repo.Name))
		if err != nil {
			return err
		}

		if !cctx.Args().Present() {
			err := xerrors.Errorf("must specify key to export")
			return err
		}

		addr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		ki, err := n.Wallet.WalletExport(ctx, addr)
		if err != nil {
			return err
		}

		b, err := json.Marshal(ki)
		if err != nil {
			return err
		}

		if cctx.Bool("json") {
			out := map[string]interface{}{
				"key": hex.EncodeToString(b),
			}
			return PrintJson(out)
		} else {
			fmt.Println(hex.EncodeToString(b))
		}
		return nil
	},
}

var walletImport = &cli.Command{
	Name:      "import",
	Usage:     "import keys",
	ArgsUsage: "[<path> (optional, will read from stdin if omitted)]",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "format",
			Usage: "specify input format for key",
			Value: "hex-lotus",
		},
		&cli.BoolFlag{
			Name:  "as-default",
			Usage: "import the given key as your new default key",
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := lcli.ReqContext(cctx)

		n, err := Setup(cctx.String(mk12_client_repo.Name))
		if err != nil {
			return err
		}

		var inpdata []byte
		if !cctx.Args().Present() || cctx.Args().First() == "-" {
			if term.IsTerminal(int(os.Stdin.Fd())) {
				fmt.Print("Enter private key(not display in the terminal): ")

				sigCh := make(chan os.Signal, 1)
				// Notify the channel when SIGINT is received
				signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

				go func() {
					<-sigCh
					fmt.Println("\nInterrupt signal received. Exiting...")
					os.Exit(1)
				}()

				inpdata, err = term.ReadPassword(int(os.Stdin.Fd()))
				if err != nil {
					return err
				}
				fmt.Println()
			} else {
				reader := bufio.NewReader(os.Stdin)
				indata, err := reader.ReadBytes('\n')
				if err != nil {
					return err
				}
				inpdata = indata
			}

		} else {
			fdata, err := os.ReadFile(cctx.Args().First())
			if err != nil {
				return err
			}
			inpdata = fdata
		}

		var ki chain_types.KeyInfo
		switch cctx.String("format") {
		case "hex-lotus":
			data, err := hex.DecodeString(strings.TrimSpace(string(inpdata)))
			if err != nil {
				return err
			}

			if err := json.Unmarshal(data, &ki); err != nil {
				return err
			}
		case "json-lotus":
			if err := json.Unmarshal(inpdata, &ki); err != nil {
				return err
			}
		case "gfc-json":
			var f struct {
				KeyInfo []struct {
					PrivateKey []byte
					SigType    int
				}
			}
			if err := json.Unmarshal(inpdata, &f); err != nil {
				return xerrors.Errorf("failed to parse go-filecoin key: %s", err)
			}

			gk := f.KeyInfo[0]
			ki.PrivateKey = gk.PrivateKey
			switch gk.SigType {
			case 1:
				ki.Type = chain_types.KTSecp256k1
			case 2:
				ki.Type = chain_types.KTBLS
			case 3:
				ki.Type = chain_types.KTDelegated
			default:
				return xerrors.Errorf("unrecognized key type: %d", gk.SigType)
			}
		default:
			return xerrors.Errorf("unrecognized format: %s", cctx.String("format"))
		}

		addr, err := n.Wallet.WalletImport(ctx, &ki)
		if err != nil {
			return err
		}

		if cctx.Bool("as-default") {
			if err := n.Wallet.SetDefault(addr); err != nil {
				return xerrors.Errorf("failed to set default key: %w", err)
			}
		}

		if cctx.Bool("json") {
			out := map[string]interface{}{
				"address": addr,
			}
			return PrintJson(out)
		} else {
			fmt.Printf("imported key %s successfully!\n", addr)
		}
		return nil
	},
}

var walletGetDefault = &cli.Command{
	Name:    "default",
	Usage:   "Get default wallet address",
	Aliases: []string{"get-default"},
	Action: func(cctx *cli.Context) error {
		n, err := Setup(cctx.String(mk12_client_repo.Name))
		if err != nil {
			return err
		}

		addr, err := n.Wallet.GetDefault()
		if err != nil {
			return err
		}

		if cctx.Bool("json") {
			out := map[string]interface{}{
				"address": addr.String(),
			}
			return PrintJson(out)
		} else {
			fmt.Printf("%s\n", addr.String())
		}
		return nil
	},
}

var walletSetDefault = &cli.Command{
	Name:      "set-default",
	Usage:     "Set default wallet address",
	ArgsUsage: "[address]",
	Action: func(cctx *cli.Context) error {
		n, err := Setup(cctx.String(mk12_client_repo.Name))
		if err != nil {
			return err
		}

		if !cctx.Args().Present() {
			return xerrors.Errorf("must pass address to set as default")
		}

		addr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		return n.Wallet.SetDefault(addr)
	},
}

var walletDelete = &cli.Command{
	Name:      "delete",
	Usage:     "Delete an account from the wallet",
	ArgsUsage: "<address> ",
	Action: func(cctx *cli.Context) error {
		ctx := lcli.ReqContext(cctx)

		n, err := Setup(cctx.String(mk12_client_repo.Name))
		if err != nil {
			return err
		}

		if !cctx.Args().Present() || cctx.NArg() != 1 {
			return xerrors.Errorf("must specify address to delete")
		}

		addr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		return n.Wallet.WalletDelete(ctx, addr)
	},
}

var walletSign = &cli.Command{
	Name:      "sign",
	Usage:     "Sign a message",
	ArgsUsage: "<signing address> <hexMessage>",
	Action: func(cctx *cli.Context) error {
		ctx := lcli.ReqContext(cctx)

		n, err := Setup(cctx.String(mk12_client_repo.Name))
		if err != nil {
			return err
		}

		if !cctx.Args().Present() || cctx.NArg() != 2 {
			return xerrors.Errorf("must specify signing address and message to sign")
		}

		addr, err := address.NewFromString(cctx.Args().First())
		if err != nil {
			return err
		}

		msg, err := hex.DecodeString(cctx.Args().Get(1))
		if err != nil {
			return err
		}

		sig, err := n.Wallet.WalletSign(ctx, addr, msg, api.MsgMeta{Type: api.MTUnknown})
		if err != nil {
			return err
		}

		sigBytes := append([]byte{byte(sig.Type)}, sig.Data...)

		if cctx.Bool("json") {
			out := map[string]interface{}{
				"signature": hex.EncodeToString(sigBytes),
			}
			err := PrintJson(out)
			if err != nil {
				return err
			}
		} else {
			fmt.Println(hex.EncodeToString(sigBytes))
		}

		return nil
	},
}
