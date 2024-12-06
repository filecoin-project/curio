package libp2p

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/metrics"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	basichost "github.com/libp2p/go-libp2p/p2p/host/basic"
	"github.com/libp2p/go-libp2p/p2p/host/peerstore/pstoremem"
	"github.com/multiformats/go-multiaddr"
	"github.com/samber/lo"
	"github.com/snadrus/must"
	"go.uber.org/zap"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	cborutil "github.com/filecoin-project/go-cbor-util"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/builtin/v13/miner"
	"github.com/filecoin-project/go-state-types/builtin/v9/market"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/mk12"
	"github.com/filecoin-project/curio/market/mk12/legacytypes"
	"github.com/filecoin-project/curio/tasks/message"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors"
	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("curio-libp2p")

// typically 6M gas per message. 0.02 FIL should suffice even at close to 5nFIL basefee, but provides a reasonable upper bound
var maintenanceMsgMaxFee = must.One(types.ParseFIL("0.02"))

func NewLibp2pHost(ctx context.Context, db *harmonydb.DB, cfg *config.CurioConfig, machine string) (host.Host, multiaddr.Multiaddr, error) {
	lcfg, err := getCfg(ctx, db, cfg.HTTP, machine)
	if err != nil {
		return nil, nil, err
	}

	pstore, err := pstoremem.NewPeerstore()
	if err != nil {
		return nil, nil, fmt.Errorf("creating peer store: %w", err)
	}

	pubK := lcfg.priv.GetPublic()
	id, err := peer.IDFromPublicKey(pubK)
	if err != nil {
		return nil, nil, fmt.Errorf("getting peer ID: %w", err)
	}

	err = pstore.AddPrivKey(id, lcfg.priv)
	if err != nil {
		return nil, nil, fmt.Errorf("adding private key to peerstore: %w", err)
	}
	err = pstore.AddPubKey(id, pubK)
	if err != nil {
		return nil, nil, fmt.Errorf("adding public key to peerstore: %w", err)
	}

	addrFactory, err := MakeAddrsFactory([]multiaddr.Multiaddr{lcfg.AnnounceAddr})
	if err != nil {
		return nil, nil, fmt.Errorf("creating address factory: %w", err)
	}

	opts := []libp2p.Option{
		libp2p.DefaultTransports,
		libp2p.NoListenAddrs,
		libp2p.ListenAddrs(lcfg.ListenAddr...),
		libp2p.AddrsFactory(addrFactory),
		libp2p.Peerstore(pstore),
		libp2p.UserAgent("curio-" + build.UserVersion()),
		libp2p.Ping(true),
		libp2p.EnableNATService(),
		libp2p.BandwidthReporter(metrics.NewBandwidthCounter()),
		libp2p.Identity(lcfg.priv),
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		return nil, nil, xerrors.Errorf("creating libp2p host: %w", err)
	}

	listenAddress, err := getMatchingLocalListenAddress(h, machine)
	if err != nil {
		return nil, nil, xerrors.Errorf("failed to get matching local listen address: %w", err)
	}

	log.Infof("Libp2p started listening on %s/p2p/%s", listenAddress, h.ID())
	log.Infof("Libp2p announcing %s/p2p/%s", lcfg.AnnounceAddr, h.ID())

	// Update the database local_listen
	_, err = db.Exec(ctx, `UPDATE libp2p SET local_listen = $1 WHERE running_on = $2`, listenAddress.String(), machine)
	if err != nil {
		return nil, nil, xerrors.Errorf("failed to update local_listen in DB: %w", err)
	}

	// Start a goroutine to update updated_at colum of libp2p table and release lock at node shutdown
	go func() {
		ticker := time.NewTicker(time.Second * 30)
		defer func(h host.Host) {
			err := h.Close()
			if err != nil {
				log.Error("could not stop libp2p node: %w", err)
			}
		}(h)
		for {
			select {
			case <-ctx.Done():
				log.Info("Releasing libp2p claims")
				_, err := db.Exec(ctx, `UPDATE libp2p SET running_on = NULL`)
				if err != nil {
					log.Error("Cleaning up libp2p claims ", err)
				}
				return
			case <-ticker.C:
				n, err := db.Exec(ctx, `UPDATE libp2p SET updated_at=CURRENT_TIMESTAMP WHERE running_on = $1`, machine)
				if err != nil {
					log.Error("Cannot keepalive ", err)
				}
				if n != 1 {
					log.Error("could not update the DB, possibly lost the libp2p lock to some other node")
					return
				}
			}
		}
	}()

	return h, lcfg.AnnounceAddr, err
}

func getMatchingLocalListenAddress(h host.Host, machine string) (multiaddr.Multiaddr, error) {
	// 'machine' is in the format "host:port"
	hostStr, _, err := net.SplitHostPort(machine)
	if err != nil {
		return nil, xerrors.Errorf("invalid machine address: %v", err)
	}

	// Parse the host to get the IP
	ip := net.ParseIP(hostStr)
	if ip == nil {
		return nil, xerrors.Errorf("invalid IP address in machine: %s", hostStr)
	}

	// Determine IP version
	var ipVersion int
	if ip.To4() != nil {
		ipVersion = 4
	} else if ip.To16() != nil {
		ipVersion = 6
	} else {
		return nil, xerrors.Errorf("unknown IP version for IP: %s", ip.String())
	}

	la := h.Network().ListenAddresses()
	if len(la) != 1 {
		return nil, xerrors.Errorf("expected exactly one listen address, but got %d", len(la))
	}

	port, err := la[0].ValueForProtocol(multiaddr.P_TCP)
	if err != nil {
		return nil, xerrors.Errorf("failed to get port from listen address: %w", err)
	}

	localListen, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip%d/%s/tcp/%s/ws", ipVersion, ip.String(), port))
	if err != nil {
		return nil, xerrors.Errorf("creating local listen address: %w", err)
	}

	return localListen, nil
}

type libp2pCfg struct {
	priv         crypto.PrivKey
	ListenAddr   []multiaddr.Multiaddr
	AnnounceAddr multiaddr.Multiaddr
}

func getCfg(ctx context.Context, db *harmonydb.DB, httpConf config.HTTPConfig, machine string) (*libp2pCfg, error) {
	if !httpConf.Enable {
		return nil, xerrors.New("libp2p requires the HTTP server to be enabled")
	}
	if httpConf.DomainName == "" {
		return nil, xerrors.New("libp2p requires the domain name to be set")
	}

	var ret libp2pCfg

	ret.ListenAddr = append(ret.ListenAddr, must.One(multiaddr.NewMultiaddr("/ip4/0.0.0.0/tcp/0/ws")))

	publicAddr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/dns/%s/tcp/%d/wss", httpConf.DomainName, 443))
	if err != nil {
		return nil, xerrors.Errorf("creating public address: %w", err)
	}

	ret.AnnounceAddr = publicAddr

	// Generate possible initial key values (really only used on first cluster startup, but cheap enough to just propose to the function)
	initialPriv, initialPub, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, xerrors.Errorf("generating private key: %w", err)
	}

	initialPeerID, err := peer.IDFromPublicKey(initialPub)
	if err != nil {
		return nil, xerrors.Errorf("getting peer ID: %w", err)
	}

	initialPrivBytes, err := crypto.MarshalPrivateKey(initialPriv)
	if err != nil {
		return nil, xerrors.Errorf("marshaling private key: %w", err)
	}

	var privKey []byte
	err = db.QueryRow(ctx, `SELECT update_libp2p_node ($1, $2, $3)`, machine, initialPrivBytes, initialPeerID).Scan(&privKey)
	if err != nil {
		return nil, xerrors.Errorf("getting private key from DB: %w", err)
	}

	p, err := crypto.UnmarshalPrivateKey(privKey)
	if err != nil {
		return nil, xerrors.Errorf("unmarshaling private key: %w", err)
	}

	ret.priv = p

	return &ret, nil
}

func MakeAddrsFactory(announceAddrs []multiaddr.Multiaddr) (basichost.AddrsFactory, error) {
	return func(_ []multiaddr.Multiaddr) []multiaddr.Multiaddr {
		return announceAddrs
	}, nil
}

var netlog = logging.Logger("mk12-net")
var propLog = logging.Logger("mk12-prop")

const DealProtocolv120ID = "/fil/storage/mk/1.2.0"
const DealProtocolv121ID = "/fil/storage/mk/1.2.1"
const DealStatusV12ProtocolID = "/fil/storage/status/1.2.0"

// The time limit to read a message from the client when the client opens a stream
const providerReadDeadline = 10 * time.Second

// The time limit to write a response to the client
const providerWriteDeadline = 10 * time.Second

func SafeHandle(h network.StreamHandler) network.StreamHandler {
	return func(stream network.Stream) {
		defer func() {
			if r := recover(); r != nil {
				netlog.Error("panic occurred\n", string(debug.Stack()))
			}
		}()

		h(stream)
	}
}

// DealProvider listens for incoming deal proposals over libp2p
type DealProvider struct {
	ctx            context.Context
	host           host.Host
	prov           *mk12.MK12
	api            mk12libp2pAPI
	db             *harmonydb.DB
	disabledMiners []address.Address
}

type mk12libp2pAPI interface {
	StateAccountKey(context.Context, address.Address, types.TipSetKey) (address.Address, error)
	StateMinerInfo(context.Context, address.Address, types.TipSetKey) (minerInfo api.MinerInfo, err error)
}

func NewDealProvider(ctx context.Context, db *harmonydb.DB, cfg *config.CurioConfig, prov *mk12.MK12, api mk12libp2pAPI, sender *message.Sender, miners []address.Address, machine string) error {
	h, announceAddr, err := NewLibp2pHost(ctx, db, cfg, machine)
	if err != nil {
		return xerrors.Errorf("failed to start libp2p nodes: %w", err)
	}

	var disabledMiners []address.Address

	for _, m := range cfg.Market.StorageMarketConfig.MK12.DisabledMiners {
		maddr, err := address.NewFromString(m)
		if err != nil {
			return err
		}
		disabledMiners = append(disabledMiners, maddr)
	}

	p := &DealProvider{
		ctx:            ctx,
		host:           h,
		prov:           prov,
		api:            api,
		db:             db,
		disabledMiners: disabledMiners,
	}

	go p.Start(ctx, h)

	nonDisabledMiners := lo.Filter(miners, func(addr address.Address, _ int) bool {
		return !lo.Contains(disabledMiners, addr)
	})

	go p.checkMinerInfos(ctx, sender, announceAddr, nonDisabledMiners)

	return nil
}

func (p *DealProvider) checkMinerInfos(ctx context.Context, sender *message.Sender, announceAddr multiaddr.Multiaddr, miners []address.Address) {
	for _, m := range miners {
		mi, err := p.api.StateMinerInfo(ctx, m, types.EmptyTSK)
		if err != nil {
			log.Errorw("failed to get miner info", "miner", m, "error", err)
			continue
		}

		if mi.PeerId == nil || mi.PeerId.String() != p.host.ID().String() {
			// update the peerid

			params, aerr := actors.SerializeParams(&miner.ChangePeerIDParams{NewID: abi.PeerID(p.host.ID())})
			if aerr != nil {
				log.Errorw("failed to serialize params", "miner", m, "error", aerr)
				continue
			}

			msg := &types.Message{
				To:     m,
				From:   mi.Worker,
				Value:  big.Zero(),
				Method: builtin.MethodsMiner.ChangePeerID,
				Params: params,
			}

			smsg, err := sender.Send(ctx, msg, &api.MessageSendSpec{MaxFee: abi.TokenAmount(maintenanceMsgMaxFee)}, "libp2p-peerid")
			if err != nil {
				log.Errorw("failed to send message", "miner", m, "error", err)
				continue
			}

			log.Warnw("sent message to update miner peerid", "miner", m, "cid", smsg.String())
		}

		var chainma multiaddr.Multiaddr
		if mi.Multiaddrs != nil && len(mi.Multiaddrs) == 1 { // == 1 because if it's not then we really have no reason to check further
			chainma, err = multiaddr.NewMultiaddrBytes(mi.Multiaddrs[0])
			if err != nil {
				log.Errorw("failed to parse miner multiaddr", "miner", m, "error", err)
				// continue anyways, might be messed up on-chain data
			}
		}

		if chainma != nil && chainma.Equal(announceAddr) {
			continue
		}

		// update the multiaddr
		params, aerr := actors.SerializeParams(&miner.ChangeMultiaddrsParams{NewMultiaddrs: []abi.Multiaddrs{announceAddr.Bytes()}})
		if aerr != nil {
			log.Errorw("failed to serialize params", "miner", m, "error", aerr)
			continue
		}

		msg := &types.Message{
			To:     m,
			From:   mi.Worker,
			Value:  big.Zero(),
			Method: builtin.MethodsMiner.ChangeMultiaddrs,
			Params: params,
		}

		smsg, err := sender.Send(ctx, msg, &api.MessageSendSpec{MaxFee: abi.TokenAmount(maintenanceMsgMaxFee)}, "libp2p-multiaddr")
		if err != nil {
			log.Errorw("failed to send message", "miner", m, "error", err)
			continue
		}

		log.Warnw("sent message to update miner multiaddr", "miner", m, "cid", smsg.String())
	}
}

func (p *DealProvider) Start(ctx context.Context, host host.Host) {
	// Note that the handling for deal protocol v1.2.0 and v1.2.1 is the same.
	// Deal protocol v1.2.1 has a couple of new fields: SkipIPNIAnnounce and
	// RemoveUnsealedCopy.
	// If a client that supports deal protocol v1.2.0 sends a request to a
	// boostd server that supports deal protocol v1.2.1, the DealParams struct
	// will be missing these new fields.
	// When the DealParams struct is unmarshalled the missing fields will be
	// set to false, which maintains the previous behaviour:
	// - SkipIPNIAnnounce=false:    announce deal to IPNI
	// - RemoveUnsealedCopy=false:  keep unsealed copy of deal data
	host.SetStreamHandler(DealProtocolv121ID, SafeHandle(p.handleNewDealStream))
	host.SetStreamHandler(DealProtocolv120ID, SafeHandle(p.handleNewDealStream))
	host.SetStreamHandler(DealStatusV12ProtocolID, SafeHandle(p.handleNewDealStatusStream))

	// Handle Query Ask
	host.SetStreamHandler(legacytypes.AskProtocolID, SafeHandle(p.handleNewAskStream))

	// Wait for context cancellation

	<-ctx.Done()
	host.RemoveStreamHandler(DealProtocolv121ID)
	host.RemoveStreamHandler(DealProtocolv120ID)
	host.RemoveStreamHandler(DealStatusV12ProtocolID)
	host.RemoveStreamHandler(legacytypes.AskProtocolID)
}

// Called when the client opens a libp2p stream with a new deal proposal
func (p *DealProvider) handleNewDealStream(s network.Stream) {
	start := time.Now()
	reqLogUuid := uuid.New()
	reqLog := netlog.With("reqlog-uuid", reqLogUuid.String(), "client-peer", s.Conn().RemotePeer())
	reqLog.Debugw("new deal proposal request")

	defer func() {
		err := s.Close()
		if err != nil {
			reqLog.Infow("closing stream", "err", err)
		}
		reqLog.Debugw("handled deal proposal request", "duration", time.Since(start).String())
	}()

	// Set a deadline on reading from the stream so it doesn't hang
	_ = s.SetReadDeadline(time.Now().Add(providerReadDeadline))

	// Read the deal proposal from the stream
	var proposal mk12.DealParams
	err := proposal.UnmarshalCBOR(s)
	_ = s.SetReadDeadline(time.Time{}) // Clear read deadline so conn doesn't get closed
	if err != nil {
		reqLog.Warnw("reading storage deal proposal from stream", "err", err)
		return
	}

	reqLog = reqLog.With("id", proposal.DealUUID)
	reqLog.Infow("received deal proposal")
	startExec := time.Now()

	var res mk12.ProviderDealRejectionInfo

	if lo.Contains(p.disabledMiners, proposal.ClientDealProposal.Proposal.Provider) {
		reqLog.Infow("Deal rejected as libp2p is disabled for provider", "deal", proposal.DealUUID, "provider", proposal.ClientDealProposal.Proposal.Provider)
		res.Accepted = false
		res.Reason = "Libp2p is disabled for the provider"
	} else {
		// Start executing the deal.
		// Note: This method just waits for the deal to be accepted, it doesn't
		// wait for deal execution to complete.
		eres, err := p.prov.ExecuteDeal(context.Background(), &proposal, s.Conn().RemotePeer())
		reqLog.Debugw("processed deal proposal accept")
		if err != nil {
			reqLog.Warnw("deal proposal failed", "err", err, "reason", res.Reason)
		}

		res = *eres
	}

	// Log the response
	propLog.Infow("send deal proposal response",
		"id", proposal.DealUUID,
		"accepted", res.Accepted,
		"msg", res.Reason,
		"peer id", s.Conn().RemotePeer(),
		"client address", proposal.ClientDealProposal.Proposal.Client,
		"provider address", proposal.ClientDealProposal.Proposal.Provider,
		"piece cid", proposal.ClientDealProposal.Proposal.PieceCID.String(),
		"piece size", proposal.ClientDealProposal.Proposal.PieceSize,
		"verified", proposal.ClientDealProposal.Proposal.VerifiedDeal,
		"label", proposal.ClientDealProposal.Proposal.Label,
		"start epoch", proposal.ClientDealProposal.Proposal.StartEpoch,
		"end epoch", proposal.ClientDealProposal.Proposal.EndEpoch,
		"price per epoch", proposal.ClientDealProposal.Proposal.StoragePricePerEpoch,
		"duration", time.Since(startExec).String(),
	)

	// Set a deadline on writing to the stream so it doesn't hang
	_ = s.SetWriteDeadline(time.Now().Add(providerWriteDeadline))
	defer s.SetWriteDeadline(time.Time{}) // nolint

	// Write the response to the client
	err = cborutil.WriteCborRPC(s, &mk12.DealResponse{Accepted: res.Accepted, Message: res.Reason})
	if err != nil {
		reqLog.Warnw("writing deal response", "err", err)
	}
}

func (p *DealProvider) handleNewDealStatusStream(s network.Stream) {
	start := time.Now()
	reqLogUuid := uuid.New()
	reqLog := netlog.With("reqlog-uuid", reqLogUuid.String(), "client-peer", s.Conn().RemotePeer())
	reqLog.Debugw("new deal status request")

	defer func() {
		err := s.Close()
		if err != nil {
			reqLog.Infow("closing stream", "err", err)
		}
		reqLog.Debugw("handled deal status request", "duration", time.Since(start).String())
	}()

	// Read the deal status request from the stream
	_ = s.SetReadDeadline(time.Now().Add(providerReadDeadline))
	var req mk12.DealStatusRequest
	err := req.UnmarshalCBOR(s)
	_ = s.SetReadDeadline(time.Time{}) // Clear read deadline so conn doesn't get closed
	if err != nil {
		reqLog.Warnw("reading deal status request from stream", "err", err)
		return
	}
	reqLog = reqLog.With("id", req.DealUUID)
	reqLog.Debugw("received deal status request")

	resp := p.getDealStatus(req, reqLog)
	reqLog.Debugw("processed deal status request")

	// Set a deadline on writing to the stream so it doesn't hang
	_ = s.SetWriteDeadline(time.Now().Add(providerWriteDeadline))
	defer s.SetWriteDeadline(time.Time{}) // nolint

	if err := cborutil.WriteCborRPC(s, &resp); err != nil {
		reqLog.Errorw("failed to write deal status response", "err", err)
	}
}

func (p *DealProvider) getDealStatus(req mk12.DealStatusRequest, reqLog *zap.SugaredLogger) mk12.DealStatusResponse {
	errResp := func(err string) mk12.DealStatusResponse {
		return mk12.DealStatusResponse{DealUUID: req.DealUUID, Error: err}
	}

	var pdeals []struct {
		AfterPSD bool `db:"after_psd"`
		Sealed   bool `db:"sealed"`
		Indexed  bool `db:"indexed"`
	}

	err := p.db.Select(p.ctx, &pdeals, `SELECT 
									after_psd,
									sealed,
									indexed
								FROM 
									market_mk12_deal_pipeline
								WHERE 
									uuid = $1;`, req.DealUUID)

	if err != nil {
		return errResp(fmt.Sprintf("failed to query the db for deal status: %s", err))
	}

	if len(pdeals) > 1 {
		return errResp("found multiple entries for the same UUID, inform the storage provider")
	}

	// If deal is still in pipeline
	if len(pdeals) == 1 {
		pdeal := pdeals[0]
		// If PSD is done
		if pdeal.AfterPSD {
			st, err := p.getSealedDealStatus(p.ctx, req.DealUUID.String(), true)
			if err != nil {
				reqLog.Errorw("failed to get sealed deal status", "err", err)
				return errResp("failed to get sealed deal status")
			}
			ret := mk12.DealStatusResponse{
				DealUUID: req.DealUUID,
				DealStatus: &mk12.DealStatus{
					Error:             st.Error,
					Status:            "Sealing",
					SealingStatus:     "Sealed",
					Proposal:          st.Proposal,
					SignedProposalCid: st.SignedProposalCID,
					PublishCid:        &st.PublishCID,
					ChainDealID:       st.ChainDealID,
				},
				IsOffline:      st.Offline,
				TransferSize:   1,
				NBytesReceived: 1,
			}
			if pdeal.Sealed {
				ret.DealStatus.Status = "Sealed"
			}
			if pdeal.Indexed {
				ret.DealStatus.Status = "Sealed and Indexed"
			}
		}
		// Anything before PSD is processing
		st, err := p.getSealedDealStatus(p.ctx, req.DealUUID.String(), false)
		if err != nil {
			reqLog.Errorw("failed to get sealed deal status", "err", err)
			return errResp("failed to get sealed deal status")
		}
		return mk12.DealStatusResponse{
			DealUUID: req.DealUUID,
			DealStatus: &mk12.DealStatus{
				Error:             st.Error,
				Status:            "Processing",
				SealingStatus:     "Not assigned to sector",
				Proposal:          st.Proposal,
				SignedProposalCid: st.SignedProposalCID,
				PublishCid:        &st.PublishCID,
				ChainDealID:       st.ChainDealID,
			},
			IsOffline:      st.Offline,
			TransferSize:   1,
			NBytesReceived: 1,
		}
	}

	// If deal is not in deal pipeline
	st, err := p.getSealedDealStatus(p.ctx, req.DealUUID.String(), true)
	if err != nil {
		reqLog.Errorw("failed to get sealed deal status", "err", err)
		return errResp("failed to get sealed deal status")
	}

	return mk12.DealStatusResponse{
		DealUUID: req.DealUUID,
		DealStatus: &mk12.DealStatus{
			Error:             st.Error,
			Status:            "Sealed",
			SealingStatus:     "Sealed and Indexed",
			Proposal:          st.Proposal,
			SignedProposalCid: st.SignedProposalCID,
			PublishCid:        &st.PublishCID,
			ChainDealID:       st.ChainDealID,
		},
		IsOffline:      st.Offline,
		TransferSize:   1,
		NBytesReceived: 1,
	}
}

type dealInfo struct {
	Offline           bool
	Error             string
	Proposal          market.DealProposal
	SignedProposalCID cid.Cid
	ChainDealID       abi.DealID
	PublishCID        cid.Cid
}

func (p *DealProvider) getSealedDealStatus(ctx context.Context, id string, onChain bool) (dealInfo, error) {
	var dealInfos []struct {
		Offline           bool            `db:"offline"`
		Error             string          `db:"error"`
		Proposal          json.RawMessage `db:"proposal"`
		SignedProposalCID string          `db:"signed_proposal_cid"`
		Label             []byte          `db:"label"`
	}
	err := p.db.Select(ctx, &dealInfos, `SELECT
    										offline,
											error,
											proposal,
											signed_proposal_cid,
											label
										FROM 
											market_mk12_deals
										WHERE 
											uuid = $1;`, id)

	if err != nil {
		return dealInfo{}, xerrors.Errorf("failed to get deal details from DB: %w", err)
	}

	if len(dealInfos) != 1 {
		return dealInfo{}, xerrors.Errorf("expected 1 row but got %d", len(dealInfos))
	}

	di := dealInfos[0]

	var prop market.DealProposal
	err = json.Unmarshal(di.Proposal, &prop)
	if err != nil {
		return dealInfo{}, xerrors.Errorf("failed to unmarshal deal proposal: %w", err)
	}

	// Unmarshal Label from cbor and replace in proposal. This fixes the problem where non-string
	// labels are saved as "" in json in DB
	var l market.DealLabel
	lr := bytes.NewReader(di.Label)
	err = l.UnmarshalCBOR(lr)
	if err != nil {
		return dealInfo{}, xerrors.Errorf("unmarshal label: %w", err)
	}
	prop.Label = l

	spc, err := cid.Parse(di.SignedProposalCID)
	if err != nil {
		return dealInfo{}, xerrors.Errorf("failed to parse signed proposal CID: %w", err)
	}

	ret := dealInfo{
		Offline:           di.Offline,
		Error:             di.Error,
		Proposal:          prop,
		SignedProposalCID: spc,
		ChainDealID:       abi.DealID(0),
		PublishCID:        cid.Undef,
	}

	if !onChain {
		return ret, nil
	}

	var cInfos []struct {
		ChainDealID int64  `db:"chain_deal_id"`
		PublishCID  string `db:"publish_cid"`
	}
	err = p.db.Select(ctx, &dealInfos, `SELECT 
											chain_deal_id,
											publish_cid
										FROM 
											market_mk12_deals
										WHERE 
											uuid = $1;`, id)

	if err != nil {
		return dealInfo{}, xerrors.Errorf("failed to get deal details from DB: %w", err)
	}

	if len(cInfos) != 1 {
		return dealInfo{}, xerrors.Errorf("expected 1 row but got %d", len(dealInfos))
	}

	ci := cInfos[0]

	pc, err := cid.Parse(ci.PublishCID)
	if err != nil {
		return dealInfo{}, xerrors.Errorf("failed to parse publish CID: %w", err)
	}

	ret.PublishCID = pc
	ret.ChainDealID = abi.DealID(ci.ChainDealID)

	return ret, nil
}

func (p *DealProvider) handleNewAskStream(s network.Stream) {
	start := time.Now()
	reqLog := netlog.With("client-peer", s.Conn().RemotePeer())
	reqLog.Debugw("new queryAsk request")

	defer func() {
		err := s.Close()
		if err != nil {
			reqLog.Infow("closing stream", "err", err)
		}
		reqLog.Debugw("handled queryAsk request", "duration", time.Since(start).String())
	}()

	// Read the deal status request from the stream
	_ = s.SetReadDeadline(time.Now().Add(providerReadDeadline))
	var req legacytypes.AskRequest
	err := req.UnmarshalCBOR(s)
	_ = s.SetReadDeadline(time.Time{}) // Clear read deadline so conn doesn't get closed
	if err != nil {
		reqLog.Warnw("reading queryAsk request from stream", "err", err)
		return
	}

	var resp legacytypes.AskResponse

	resp.Ask, err = p.prov.GetAsk(p.ctx, req.Miner)
	if err != nil {
		reqLog.Warnw("failed to get ask from storage provider", "err", err)
	}

	// Set a deadline on writing to the stream so it doesn't hang
	_ = s.SetWriteDeadline(time.Now().Add(providerWriteDeadline))
	defer s.SetWriteDeadline(time.Time{}) // nolint

	if err := cborutil.WriteCborRPC(s, &resp); err != nil {
		reqLog.Errorw("failed to write queryAsk response", "err", err)
	}
}
