package mk12

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/ipfs/go-cid"
	"go.uber.org/zap"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	cborutil "github.com/filecoin-project/go-cbor-util"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin"
	"github.com/filecoin-project/go-state-types/builtin/v9/account"
	"github.com/filecoin-project/go-state-types/builtin/v9/market"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/go-state-types/exitcode"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/mk12/legacytypes"

	ctypes "github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/lib/sigs"
)

const DealMaxLabelSize = 256

const maxDealCollateralMultiplier = 2

// DefaultPrice is the default price for unverified deals (in attoFil / GiB / Epoch)
var DefaultPrice = abi.NewTokenAmount(50000000)

// DefaultVerifiedPrice is the default price for verified deals (in attoFil / GiB / Epoch)
var DefaultVerifiedPrice = abi.NewTokenAmount(5000000)

// DefaultDuration is the default number of epochs a storage ask is in effect for
const DefaultDuration abi.ChainEpoch = 1000000

// DefaultMinPieceSize is the minimum accepted piece size for data
const DefaultMinPieceSize abi.PaddedPieceSize = 16 << 30

// DefaultMaxPieceSize is the default maximum accepted size for pieces for deals
// TODO: It would be nice to default this to the miner's sector size
const DefaultMaxPieceSize abi.PaddedPieceSize = 32 << 30

func (m *MK12) GetAsk(ctx context.Context, miner address.Address) (*legacytypes.SignedStorageAsk, error) {

	minerid, err := address.IDFromAddress(miner)
	if err != nil {
		return nil, err
	}

	var asks []struct {
		Price         int64 `db:"price"`
		VerifiedPrice int64 `db:"verified_price"`
		MinPieceSize  int64 `db:"min_size"`
		MaxPieceSize  int64 `db:"max_size"`
		Miner         int64 `db:"sp_id"`
		Timestamp     int64 `db:"created_at"`
		Expiry        int64 `db:"expiry"`
		SeqNo         int64 `db:"sequence"`
	}

	err = m.db.Select(ctx, &asks, `SELECT sp_id, price, verified_price, min_size, max_size, created_at, expiry, sequence 
								FROM market_mk12_storage_ask WHERE sp_id = $1`, minerid)

	if err != nil {
		return nil, xerrors.Errorf("getting ask from database: %w", err)
	}

	if len(asks) == 0 {
		return nil, xerrors.Errorf("no ask found for the given miner")
	}

	ask := &legacytypes.StorageAsk{
		Price:         big.NewInt(asks[0].Price),
		VerifiedPrice: big.NewInt(asks[0].VerifiedPrice),
		MinPieceSize:  abi.PaddedPieceSize(asks[0].MinPieceSize),
		MaxPieceSize:  abi.PaddedPieceSize(asks[0].MaxPieceSize),
		Miner:         miner,
		Timestamp:     abi.ChainEpoch(asks[0].Timestamp),
		Expiry:        abi.ChainEpoch(asks[0].Expiry),
		SeqNo:         uint64(asks[0].SeqNo),
	}

	tok, err := m.api.ChainHead(ctx)
	if err != nil {
		return nil, err
	}

	msg, err := cborutil.Dump(ask)
	if err != nil {
		return nil, xerrors.Errorf("serializing: %w", err)
	}

	mi, err := m.api.StateMinerInfo(ctx, ask.Miner, tok.Key())
	if err != nil {
		return nil, err
	}

	signer, err := m.api.StateAccountKey(ctx, mi.Worker, tok.Key())
	if err != nil {
		return nil, err
	}

	sig, err := m.api.WalletSign(ctx, signer, msg)
	if err != nil {
		return nil, err
	}

	ret := &legacytypes.SignedStorageAsk{
		Ask:       ask,
		Signature: sig,
	}

	return ret, nil

}

func (m *MK12) SetAsk(ctx context.Context, price abi.TokenAmount, verifiedPrice abi.TokenAmount, miner address.Address, options ...legacytypes.StorageAskOption) error {

	spid, err := address.IDFromAddress(miner)
	if err != nil {
		return xerrors.Errorf("getting miner id from address: %w", err)
	}

	var seqnos []uint64
	err = m.db.Select(ctx, &seqnos, `SELECT sequence 
								FROM market_mk12_storage_ask WHERE sp_id = $1`, spid)

	if err != nil {
		return xerrors.Errorf("getting sequence from DB: %w", err)
	}

	if len(seqnos) == 0 {
		seqnos = []uint64{0}
	}

	minPieceSize := DefaultMinPieceSize
	maxPieceSize := DefaultMaxPieceSize

	duration := abi.ChainEpoch(builtin.EpochsInYear * 10)

	ts, err := m.api.ChainHead(ctx)
	if err != nil {
		return err
	}
	ask := &legacytypes.StorageAsk{
		Price:         price,
		VerifiedPrice: verifiedPrice,
		Timestamp:     ts.Height(),
		Expiry:        ts.Height() + duration,
		Miner:         miner,
		SeqNo:         seqnos[0] + 1,
		MinPieceSize:  minPieceSize,
		MaxPieceSize:  maxPieceSize,
	}

	for _, option := range options {
		option(ask)
	}

	n, err := m.db.Exec(ctx, `INSERT INTO market_mk12_storage_ask (
										sp_id, price, verified_price, min_size, max_size, created_at, expiry, sequence
									) VALUES (
										$1, $2, $3, $4, $5, $6, $7, $8
									)
									ON CONFLICT (sp_id) DO UPDATE SET
										price = EXCLUDED.price,
										verified_price = EXCLUDED.verified_price,
										min_size = EXCLUDED.min_size,
										max_size = EXCLUDED.max_size,
										created_at = EXCLUDED.created_at,
										expiry = EXCLUDED.expiry,
										sequence = EXCLUDED.sequence`,
		spid, ask.Price, ask.VerifiedPrice, ask.MinPieceSize, ask.MaxPieceSize, ask.Timestamp, ask.Expiry, int64(ask.SeqNo))

	if err != nil {
		return xerrors.Errorf("store ask success: updating pipeline: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("store ask success: updated %d rows", n)
	}

	return nil
}

func (m *MK12) verifySignature(ctx context.Context, sig crypto.Signature, addr address.Address, input []byte) (bool, error) {
	addr, err := m.api.StateAccountKey(ctx, addr, ctypes.EmptyTSK)
	if err != nil {
		return false, err
	}

	// Check if the client is an f4 address, ie an FVM contract
	clientAddr := addr.String()
	if len(clientAddr) >= 2 && (clientAddr[:2] == "t4" || clientAddr[:2] == "f4") {
		// Verify authorization by simulating an AuthenticateMessage
		return m.verifyContractSignature(ctx, sig, addr, input)
	}

	// Otherwise do local signature verification
	err = sigs.Verify(&sig, addr, input)
	return err == nil, err
}

// verifyContractSignature simulates sending an AuthenticateMessage to authenticate the signer
func (m *MK12) verifyContractSignature(ctx context.Context, sig crypto.Signature, addr address.Address, input []byte) (bool, error) {
	var params account.AuthenticateMessageParams
	params.Message = input
	params.Signature = sig.Data

	var msg ctypes.Message
	buf := new(bytes.Buffer)

	var err error
	err = params.MarshalCBOR(buf)
	if err != nil {
		return false, err
	}
	msg.Params = buf.Bytes()

	msg.From = builtin.StorageMarketActorAddr
	msg.To = addr
	msg.Nonce = 1

	msg.Method, err = builtin.GenerateFRCMethodNum("AuthenticateMessage") // abi.MethodNum(2643134072)
	if err != nil {
		return false, err
	}

	res, err := m.api.StateCall(ctx, &msg, ctypes.EmptyTSK)
	if err != nil {
		return false, fmt.Errorf("state call to %s returned an error: %w", addr, err)
	}

	return res.MsgRct.ExitCode == exitcode.Ok, nil
}

func GetDealStatus(ctx context.Context, db *harmonydb.DB, req DealStatusRequest, reqLog *zap.SugaredLogger) DealStatusResponse {
	errResp := func(err string) DealStatusResponse {
		return DealStatusResponse{DealUUID: req.DealUUID, Error: err}
	}

	var pdeals []struct {
		AfterPSD bool `db:"after_psd"`
		Sealed   bool `db:"sealed"`
		Indexed  bool `db:"indexed"`
	}

	err := db.Select(ctx, &pdeals, `SELECT 
									after_psd,
									sealed,
									indexed
								FROM 
									market_mk12_deal_pipeline
								WHERE 
									uuid = $1;`, req.DealUUID)

	if err != nil {
		reqLog.Errorw("failed to query deal status", "err", err)
		return errResp("failed to query the db for deal status")
	}

	if len(pdeals) > 1 {
		return errResp("found multiple entries for the same UUID, inform the storage provider")
	}

	// If deal is still in pipeline
	if len(pdeals) == 1 {
		pdeal := pdeals[0]
		// If PSD is done
		if pdeal.AfterPSD {
			st, err := getSealedDealStatus(ctx, db, req.DealUUID.String(), true)
			if err != nil {
				reqLog.Errorw("failed to get sealed deal status", "err", err)
				return errResp("failed to get sealed deal status")
			}
			ret := DealStatusResponse{
				DealUUID: req.DealUUID,
				DealStatus: &DealStatus{
					Error:             st.Error,
					Status:            "Sealing",
					SealingStatus:     "Sealed",
					Proposal:          st.Proposal,
					SignedProposalCid: st.SignedProposalCID,
					PublishCid:        cidOrNil(st.PublishCID),
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
		st, err := getSealedDealStatus(ctx, db, req.DealUUID.String(), false)
		if err != nil {
			reqLog.Errorw("failed to get sealed deal status", "err", err)
			return errResp("failed to get sealed deal status")
		}
		return DealStatusResponse{
			DealUUID: req.DealUUID,
			DealStatus: &DealStatus{
				Error:             st.Error,
				Status:            "Processing",
				SealingStatus:     "Not assigned to sector",
				Proposal:          st.Proposal,
				SignedProposalCid: st.SignedProposalCID,
				PublishCid:        cidOrNil(st.PublishCID),
				ChainDealID:       st.ChainDealID,
			},
			IsOffline:      st.Offline,
			TransferSize:   1,
			NBytesReceived: 1,
		}
	}

	// If deal is not in deal pipeline
	st, err := getSealedDealStatus(ctx, db, req.DealUUID.String(), true)
	if err != nil {
		reqLog.Errorw("failed to get sealed deal status", "err", err)
		return errResp("failed to get sealed deal status")
	}

	return DealStatusResponse{
		DealUUID: req.DealUUID,
		DealStatus: &DealStatus{
			Error:             st.Error,
			Status:            "Sealed",
			SealingStatus:     "Sealed and Indexed",
			Proposal:          st.Proposal,
			SignedProposalCid: st.SignedProposalCID,
			PublishCid:        cidOrNil(st.PublishCID),
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

func cidOrNil(c cid.Cid) *cid.Cid {
	if c == cid.Undef {
		return nil
	}

	return &c
}

func getSealedDealStatus(ctx context.Context, db *harmonydb.DB, id string, onChain bool) (dealInfo, error) {
	var dealInfos []struct {
		Offline           bool            `db:"offline"`
		Error             sql.NullString  `db:"error"`
		Proposal          json.RawMessage `db:"proposal"`
		SignedProposalCID string          `db:"signed_proposal_cid"`
		Label             []byte          `db:"label"`
	}
	err := db.Select(ctx, &dealInfos, `SELECT
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

	if !di.Error.Valid {
		di.Error = sql.NullString{}
	}

	ret := dealInfo{
		Offline:           di.Offline,
		Error:             di.Error.String,
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
	err = db.Select(ctx, &cInfos, `SELECT 
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
