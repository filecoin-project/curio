package libp2pimpl

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
	"github.com/ipfs/go-cid"
	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"go.uber.org/zap"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	cborutil "github.com/filecoin-project/go-cbor-util"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin/v9/market"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/mk12"
	"github.com/filecoin-project/curio/market/mk12/legacytypes"

	"github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("mk12-net")
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
			log.Error("panic occurred", "stack", debug.Stack())
		}
	}()

	return h
}

// DealProvider listens for incoming deal proposals over libp2p
type DealProvider struct {
	ctx  context.Context
	host host.Host
	prov *mk12.MK12
	api  mk12libp2pAPI
	db   *harmonydb.DB
}

type mk12libp2pAPI interface {
	StateAccountKey(context.Context, address.Address, types.TipSetKey) (address.Address, error)
}

func NewDealProvider(h host.Host, db *harmonydb.DB, prov *mk12.MK12, api mk12libp2pAPI) *DealProvider {
	p := &DealProvider{
		host: h,
		prov: prov,
		api:  api,
		db:   db,
	}
	return p
}

func (p *DealProvider) Start(ctx context.Context) {
	p.ctx = ctx

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
	p.host.SetStreamHandler(DealProtocolv121ID, SafeHandle(p.handleNewDealStream))
	p.host.SetStreamHandler(DealProtocolv120ID, SafeHandle(p.handleNewDealStream))
	p.host.SetStreamHandler(DealStatusV12ProtocolID, SafeHandle(p.handleNewDealStatusStream))

	// Handle Query Ask
	p.host.SetStreamHandler(legacytypes.AskProtocolID, SafeHandle(p.handleNewAskStream))

	// Wait for context cancellation

	<-p.ctx.Done()
	p.host.RemoveStreamHandler(DealProtocolv121ID)
	p.host.RemoveStreamHandler(DealProtocolv120ID)
	p.host.RemoveStreamHandler(DealStatusV12ProtocolID)
	p.host.RemoveStreamHandler(legacytypes.AskProtocolID)
}

// Called when the client opens a libp2p stream with a new deal proposal
func (p *DealProvider) handleNewDealStream(s network.Stream) {
	start := time.Now()
	reqLogUuid := uuid.New()
	reqLog := log.With("reqlog-uuid", reqLogUuid.String(), "client-peer", s.Conn().RemotePeer())
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

	// Start executing the deal.
	// Note: This method just waits for the deal to be accepted, it doesn't
	// wait for deal execution to complete.
	startExec := time.Now()
	res, err := p.prov.ExecuteDeal(context.Background(), &proposal, s.Conn().RemotePeer())
	reqLog.Debugw("processed deal proposal accept")
	if err != nil {
		reqLog.Warnw("deal proposal failed", "err", err, "reason", res.Reason)
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
	reqLog := log.With("reqlog-uuid", reqLogUuid.String(), "client-peer", s.Conn().RemotePeer())
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
		// ANything before PSD is processing
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
	reqLog := log.With("client-peer", s.Conn().RemotePeer())
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