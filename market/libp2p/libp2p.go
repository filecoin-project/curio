package libp2p

import (
	"context"
	"encoding/json"
	"fmt"
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
	mamask "github.com/whyrusleeping/multiaddr-filter"
	"go.uber.org/zap"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	cborutil "github.com/filecoin-project/go-cbor-util"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin/v9/market"

	"github.com/filecoin-project/curio/build"
	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/mk12"
	"github.com/filecoin-project/curio/market/mk12/legacytypes"

	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("curio-libp2p")

func NewLibp2pHost(ctx context.Context, db *harmonydb.DB, cfg *config.CurioConfig, machine string) (host.Host, error) {
	lcfg, err := getCfg(ctx, db, cfg.Market.StorageMarketConfig.MK12.Libp2p, machine)
	if err != nil {
		return nil, err
	}

	pstore, err := pstoremem.NewPeerstore()
	if err != nil {
		return nil, fmt.Errorf("creating peer store: %w", err)
	}

	pubK := lcfg.priv.GetPublic()
	id, err := peer.IDFromPublicKey(pubK)
	if err != nil {
		return nil, fmt.Errorf("getting peer ID: %w", err)
	}

	err = pstore.AddPrivKey(id, lcfg.priv)
	if err != nil {
		return nil, fmt.Errorf("adding private key to peerstore: %w", err)
	}
	err = pstore.AddPubKey(id, pubK)
	if err != nil {
		return nil, fmt.Errorf("adding public key to peerstore: %w", err)
	}

	addrFactory, err := MakeAddrsFactory(lcfg.AnnounceAddr, lcfg.NoAnnounceAddr)
	if err != nil {
		return nil, fmt.Errorf("creating address factory: %w", err)
	}

	opts := []libp2p.Option{
		libp2p.DefaultTransports,
		libp2p.ListenAddrs(lcfg.ListenAddr...),
		libp2p.AddrsFactory(addrFactory),
		libp2p.Peerstore(pstore),
		libp2p.UserAgent("curio-" + build.UserVersion()),
		libp2p.Ping(true),
		libp2p.EnableNATService(),
		libp2p.BandwidthReporter(metrics.NewBandwidthCounter()),
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		return nil, xerrors.Errorf("creating libp2p host: %w", err)
	}

	// Start listening
	err = h.Network().Listen(lcfg.ListenAddr...)
	if err != nil {
		return nil, xerrors.Errorf("failed to listen on addresses: %w", err)
	}

	log.Infof("Libp2p started listening")

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

	return h, err

}

type libp2pCfg struct {
	priv           crypto.PrivKey
	ListenAddr     []multiaddr.Multiaddr
	AnnounceAddr   []multiaddr.Multiaddr
	NoAnnounceAddr []multiaddr.Multiaddr
}

func getCfg(ctx context.Context, db *harmonydb.DB, cfg config.Libp2pConfig, machine string) (*libp2pCfg, error) {
	var ret libp2pCfg

	for _, l := range cfg.ListenAddresses {
		listenAddr, err := multiaddr.NewMultiaddr(l)
		if err != nil {
			return nil, xerrors.Errorf("parsing listen address: %w", err)
		}
		ret.ListenAddr = append(ret.ListenAddr, listenAddr)
	}

	for _, a := range cfg.AnnounceAddresses {
		announceAddr, err := multiaddr.NewMultiaddr(a)
		if err != nil {
			return nil, xerrors.Errorf("parsing announce address: %w", err)
		}
		ret.AnnounceAddr = append(ret.AnnounceAddr, announceAddr)
	}

	for _, na := range cfg.NoAnnounceAddresses {
		noAnnounceAddr, err := multiaddr.NewMultiaddr(na)
		if err != nil {
			return nil, xerrors.Errorf("parsing no announce address: %w", err)
		}
		ret.NoAnnounceAddr = append(ret.NoAnnounceAddr, noAnnounceAddr)
	}

	// Try to acquire the lock in DB
	_, err := db.Exec(ctx, `SELECT update_libp2p_node ($1)`, machine)
	if err != nil {
		return nil, xerrors.Errorf("acquiring libp2p locks from DB: %w", err)
	}

	var privKey []byte
	err = db.QueryRow(ctx, `SELECT priv_key FROM libp2p`).Scan(&privKey)
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

func MakeAddrsFactory(announceAddrs, noAnnounce []multiaddr.Multiaddr) (basichost.AddrsFactory, error) {
	filters := multiaddr.NewFilters()
	noAnnAddrs := map[string]bool{}
	for _, addr := range noAnnounce {
		f, err := mamask.NewMask(addr.String())
		if err == nil {
			filters.AddFilter(*f, multiaddr.ActionDeny)
			continue
		}
		noAnnAddrs[string(addr.Bytes())] = true
	}

	return func(allAddrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
		var addrs []multiaddr.Multiaddr
		if len(announceAddrs) > 0 {
			addrs = announceAddrs
		} else {
			addrs = allAddrs
		}

		var out []multiaddr.Multiaddr
		for _, maddr := range addrs {
			// check for exact matches
			ok := noAnnAddrs[string(maddr.Bytes())]
			// check for /ipcidr matches
			if !ok && !filters.AddrBlocked(maddr) {
				out = append(out, maddr)
			}
		}
		return out
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
	defer func() {
		if r := recover(); r != nil {
			netlog.Error("panic occurred", "stack", debug.Stack())
		}
	}()

	return h
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
}

func NewDealProvider(ctx context.Context, db *harmonydb.DB, cfg *config.CurioConfig, prov *mk12.MK12, api mk12libp2pAPI, machine string) error {
	h, err := NewLibp2pHost(ctx, db, cfg, machine)
	if err != nil {
		return xerrors.Errorf("failed to start libp2p nodes: %w", err)
	}

	var disabledMiners []address.Address

	for _, m := range cfg.Market.StorageMarketConfig.MK12.Libp2p.DisabledMiners {
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

	return nil
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

	var res *mk12.ProviderDealRejectionInfo

	if lo.Contains(p.disabledMiners, proposal.ClientDealProposal.Proposal.Provider) {
		reqLog.Infow("Deal rejected as libp2p is disabled for provider", "deal", proposal.DealUUID, "provider", proposal.ClientDealProposal.Proposal.Provider)
		res.Accepted = false
		res.Reason = "Libp2p is disabled for the provider"
	} else {
		// Start executing the deal.
		// Note: This method just waits for the deal to be accepted, it doesn't
		// wait for deal execution to complete.
		res, err := p.prov.ExecuteDeal(context.Background(), &proposal, s.Conn().RemotePeer())
		reqLog.Debugw("processed deal proposal accept")
		if err != nil {
			reqLog.Warnw("deal proposal failed", "err", err, "reason", res.Reason)
		}
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
	}
	err := p.db.Select(ctx, &dealInfos, `SELECT
    										offline,
											error,
											proposal,
											signed_proposal_cid
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
