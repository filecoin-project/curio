// Package market
/*
This File contains all the implementation details of how to handle
the mk1.2 deals.
*/
package mk12

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/samber/lo"
	"github.com/yugabyte/pgx/v5"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	cborutil "github.com/filecoin-project/go-cbor-util"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/go-state-types/builtin/v13/miner"
	"github.com/filecoin-project/go-state-types/builtin/v9/market"
	"github.com/filecoin-project/go-state-types/crypto"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/market/mk12/legacytypes"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	ctypes "github.com/filecoin-project/lotus/chain/types"
)

type MK12API interface {
	ChainHead(context.Context) (*types.TipSet, error)
	StateAccountKey(context.Context, address.Address, types.TipSetKey) (address.Address, error)
	StateCall(context.Context, *types.Message, types.TipSetKey) (*api.InvocResult, error)
	StateDealProviderCollateralBounds(context.Context, abi.PaddedPieceSize, bool, types.TipSetKey) (api.DealCollateralBounds, error)
	StateMarketBalance(context.Context, address.Address, types.TipSetKey) (api.MarketBalance, error)
	StateMinerInfo(context.Context, address.Address, types.TipSetKey) (api.MinerInfo, error)
	StateVerifiedClientStatus(ctx context.Context, addr address.Address, tsk types.TipSetKey) (*abi.StoragePower, error)
	WalletSign(context.Context, address.Address, []byte) (*crypto.Signature, error)
}

type MK12 struct {
	miners []address.Address
	db     *harmonydb.DB
	api    MK12API
	sc     *ffi.SealCalls
}

type validationError struct {
	error
	// The reason sent to the client for why validation failed
	reason string
}

func NewMK12Handler(miners []address.Address, db *harmonydb.DB, sc *ffi.SealCalls, mapi MK12API) (*MK12, error) {
	return &MK12{
		miners: miners,
		db:     db,
		api:    mapi,
		sc:     sc,
	}, nil
}

// ExecuteDeal is called when the Storage Provider receives a deal proposal
// from the network
func (m *MK12) ExecuteDeal(ctx context.Context, dp *DealParams, clientPeer peer.ID) (*ProviderDealRejectionInfo, error) {

	ds := &ProviderDealState{
		DealUuid:           dp.DealUUID,
		ClientDealProposal: dp.ClientDealProposal,
		ClientPeerID:       clientPeer,
		DealDataRoot:       dp.DealDataRoot,
		Transfer:           dp.Transfer,
		IsOffline:          dp.IsOffline,
		CleanupData:        !dp.IsOffline,
		FastRetrieval:      !dp.RemoveUnsealedCopy,
		AnnounceToIPNI:     !dp.SkipIPNIAnnounce,
	}

	spc, err := ds.GetSignedProposalCid()
	if err != nil {
		return &ProviderDealRejectionInfo{
			Reason: fmt.Sprintf("getting signed proposal cid: %s", err.Error()),
		}, nil
	}

	ds.SignedProposalCID = spc

	// Validate the deal proposal
	if err := m.validateDealProposal(ctx, ds); err != nil {
		// Send the client a reason for the rejection that doesn't reveal the
		// internal error message
		reason := err.reason
		if reason == "" {
			reason = err.Error()
		}

		return &ProviderDealRejectionInfo{
			Reason: fmt.Sprintf("failed validation: %s", reason),
		}, nil
	}

	return m.processDeal(ctx, ds)
}

// ValidateDealProposal validates a proposed deal against the provider criteria.
// It returns a validationError. If a nicer error message should be sent to the
// client, the reason string will be set to that nicer error message.
func (m *MK12) validateDealProposal(ctx context.Context, deal *ProviderDealState) *validationError {
	head, err := m.api.ChainHead(ctx)
	if err != nil {
		return &validationError{
			reason: "server error: getting chain head",
			error:  fmt.Errorf("node error getting most recent state id: %w", err),
		}
	}

	tok := head.Key().Bytes()
	curEpoch := head.Height()

	// Check that the proposal piece cid is defined before attempting signature
	// validation - if it's not defined, it won't be possible to marshall the
	// deal proposal to check the signature
	proposal := deal.ClientDealProposal.Proposal
	if !proposal.PieceCID.Defined() {
		return &validationError{error: fmt.Errorf("proposal PieceCID undefined")}
	}

	if ok, err := m.validateClientSignature(ctx, deal); err != nil || !ok {
		if err != nil {
			return &validationError{
				reason: "server error: validating signature",
				error:  fmt.Errorf("validateSignature failed: %w", err),
			}
		}
		return &validationError{
			reason: "invalid signature",
			error:  fmt.Errorf("invalid signature"),
		}
	}

	// validate deal proposal
	if !lo.Contains(m.miners, proposal.Provider) {
		err := fmt.Errorf("incorrect provider for deal; proposal.Provider: %s; provider.Address: %s", proposal.Provider, m.miners)
		return &validationError{error: err}
	}

	if proposal.Label.Length() > DealMaxLabelSize {
		err := fmt.Errorf("deal label can be at most %d bytes, is %d", DealMaxLabelSize, proposal.Label.Length())
		return &validationError{error: err}
	}

	if err := proposal.PieceSize.Validate(); err != nil {
		err := fmt.Errorf("proposal piece size is invalid: %w", err)
		return &validationError{error: err}
	}

	if proposal.PieceCID.Prefix() != market.PieceCIDPrefix {
		err := fmt.Errorf("proposal PieceCID had wrong prefix")
		return &validationError{error: err}
	}

	if proposal.EndEpoch <= proposal.StartEpoch {
		err := fmt.Errorf("proposal end %d before proposal start %d", proposal.EndEpoch, proposal.StartEpoch)
		return &validationError{error: err}
	}

	if curEpoch > proposal.StartEpoch {
		err := fmt.Errorf("deal start epoch %d has already elapsed (current epoch: %d)", proposal.StartEpoch, curEpoch)
		return &validationError{error: err}
	}

	// Check that the delta between the start and end epochs (the deal
	// duration) is within acceptable bounds
	minDuration, maxDuration := market.DealDurationBounds(proposal.PieceSize)
	if proposal.Duration() < minDuration || proposal.Duration() > maxDuration {
		err := fmt.Errorf("deal duration out of bounds (min, max, provided): %d, %d, %d", minDuration, maxDuration, proposal.Duration())
		return &validationError{error: err}
	}

	// Check that the proposed end epoch isn't too far beyond the current epoch
	maxEndEpoch := curEpoch + miner.MaxSectorExpirationExtension
	if proposal.EndEpoch > maxEndEpoch {
		err := fmt.Errorf("invalid deal end epoch %d: cannot be more than %d past current epoch %d", proposal.EndEpoch, miner.MaxSectorExpirationExtension, curEpoch)
		return &validationError{error: err}
	}

	bounds, err := m.api.StateDealProviderCollateralBounds(ctx, proposal.PieceSize, proposal.VerifiedDeal, ctypes.EmptyTSK)
	if err != nil {
		return &validationError{
			reason: "server error: getting collateral bounds",
			error:  fmt.Errorf("node error getting collateral bounds: %w", err),
		}
	}

	// The maximum amount of collateral that the provider will put into escrow
	// for a deal is calculated as a multiple of the minimum bounded amount
	maxC := ctypes.BigMul(bounds.Min, ctypes.NewInt(maxDealCollateralMultiplier))

	pcMin := bounds.Min
	pcMax := maxC

	if proposal.ProviderCollateral.LessThan(pcMin) {
		err := fmt.Errorf("proposed provider collateral %s below minimum %s", proposal.ProviderCollateral, pcMin)
		return &validationError{error: err}
	}

	if proposal.ProviderCollateral.GreaterThan(pcMax) {
		err := fmt.Errorf("proposed provider collateral %s above maximum %s", proposal.ProviderCollateral, pcMax)
		return &validationError{error: err}
	}

	if err := m.validateAsk(ctx, deal); err != nil {
		return &validationError{error: err}
	}

	tsk, err := ctypes.TipSetKeyFromBytes(tok)
	if err != nil {
		return &validationError{
			reason: "server error: tip set key from bytes",
			error:  err,
		}
	}

	bal, err := m.api.StateMarketBalance(ctx, proposal.Client, tsk)
	if err != nil {
		return &validationError{
			reason: "server error: getting market balance",
			error:  fmt.Errorf("node error getting client market balance failed: %w", err),
		}
	}

	clientMarketBalance := ToSharedBalance(bal)

	// This doesn't guarantee that the client won't withdraw / lock those funds
	// but it's a decent first filter
	if clientMarketBalance.Available.LessThan(proposal.ClientBalanceRequirement()) {
		err := fmt.Errorf("client available funds in escrow %d not enough to meet storage cost for deal %d", clientMarketBalance.Available, proposal.ClientBalanceRequirement())
		return &validationError{error: err}
	}

	// Verified deal checks
	if proposal.VerifiedDeal {
		// Get data cap
		dataCap, err := m.api.StateVerifiedClientStatus(ctx, proposal.Client, tsk)
		if err != nil {
			return &validationError{
				reason: "server error: getting verified datacap",
				error:  fmt.Errorf("node error fetching verified data cap: %w", err),
			}
		}

		if dataCap == nil {
			return &validationError{
				reason: "client is not a verified client",
				error:  errors.New("node error fetching verified data cap: data cap missing -- client not verified"),
			}
		}

		pieceSize := big.NewIntUnsigned(uint64(proposal.PieceSize))
		if dataCap.LessThan(pieceSize) {
			err := fmt.Errorf("verified deal DataCap %d too small for proposed piece size %d", dataCap, pieceSize)
			return &validationError{error: err}
		}
	}

	return nil
}

func (m *MK12) validateAsk(ctx context.Context, deal *ProviderDealState) error {
	sask, err := m.GetAsk(ctx, deal.ClientDealProposal.Proposal.Provider)
	if err != nil {
		return xerrors.Errorf("getting ask for miner %s: %w", deal.ClientDealProposal.Proposal.Provider.String(), err)
	}

	ask := sask.Ask

	askPrice := ask.Price
	if deal.ClientDealProposal.Proposal.VerifiedDeal {
		askPrice = ask.VerifiedPrice
	}

	proposal := deal.ClientDealProposal.Proposal
	minPrice := big.Div(big.Mul(askPrice, abi.NewTokenAmount(int64(proposal.PieceSize))), abi.NewTokenAmount(1<<30))
	if proposal.StoragePricePerEpoch.LessThan(minPrice) {
		return fmt.Errorf("storage price per epoch less than asking price: %s < %s", proposal.StoragePricePerEpoch, minPrice)
	}

	if proposal.PieceSize < ask.MinPieceSize {
		return fmt.Errorf("piece size less than minimum required size: %d < %d", proposal.PieceSize, ask.MinPieceSize)
	}

	if proposal.PieceSize > ask.MaxPieceSize {
		return fmt.Errorf("piece size more than maximum allowed size: %d > %d", proposal.PieceSize, ask.MaxPieceSize)
	}

	return nil
}

func ToSharedBalance(bal api.MarketBalance) legacytypes.Balance {
	return legacytypes.Balance{
		Locked:    bal.Locked,
		Available: big.Sub(bal.Escrow, bal.Locked),
	}
}

func (m *MK12) validateClientSignature(ctx context.Context, deal *ProviderDealState) (bool, error) {
	b, err := cborutil.Dump(&deal.ClientDealProposal.Proposal)
	if err != nil {
		return false, xerrors.Errorf("failed to serialize client deal proposal: %w", err)
	}

	verified, err := m.verifySignature(ctx, deal.ClientDealProposal.ClientSignature, deal.ClientDealProposal.Proposal.Client, b)
	if err != nil {
		return false, xerrors.Errorf("error verifying signature: %w", err)
	}
	return verified, nil
}

func (m *MK12) processDeal(ctx context.Context, deal *ProviderDealState) (*ProviderDealRejectionInfo, error) {
	// TODO: Add deal filters and Backpressure

	if deal.Transfer.Type == Libp2pScheme {
		return &ProviderDealRejectionInfo{
			Reason: "libp2p URLs are not supported by this provider",
		}, nil
	}

	propJson, err := json.Marshal(deal.ClientDealProposal.Proposal)
	if err != nil {
		return &ProviderDealRejectionInfo{
			Reason: fmt.Sprintf("json.Marshal(piece.DealProposal): %s", err),
		}, nil
	}

	sigByte, err := deal.ClientDealProposal.ClientSignature.MarshalBinary()
	if err != nil {
		return &ProviderDealRejectionInfo{
			Reason: fmt.Sprintf("marshal client signature: %s", err),
		}, nil
	}

	prop := deal.ClientDealProposal.Proposal

	mid, err := address.IDFromAddress(prop.Provider)
	if err != nil {
		return &ProviderDealRejectionInfo{
			Reason: fmt.Sprintf("address.IDFromAddress: %s", err),
		}, nil
	}

	// de-serialize transport opaque token
	tInfo := &HttpRequest{}
	if err := json.Unmarshal(deal.Transfer.Params, tInfo); err != nil {
		return &ProviderDealRejectionInfo{
			Reason: fmt.Sprintf("failed to de-serialize transport params bytes '%s': %s", string(deal.Transfer.Params), err),
		}, nil
	}

	headers, err := json.Marshal(tInfo.Headers)
	if err != nil {
		return &ProviderDealRejectionInfo{
			Reason: fmt.Sprintf("failed to marshal headers: %s", err),
		}, nil
	}

	comm, err := m.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		n, err := tx.Exec(`INSERT INTO market_mk12_deals (uuid, signed_proposal_cid, 
                                proposal_signature, proposal, piece_cid, 
                                piece_size, offline, verified, sp_id, start_epoch, end_epoch, 
                                client_peer_id, fast_retrieval, announce_to_ipni, url, url_headers) 
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
				ON CONFLICT (uuid) DO NOTHING`,
			deal.DealUuid.String(), deal.SignedProposalCID.String(), sigByte, propJson, prop.PieceCID.String(),
			prop.PieceSize, deal.IsOffline, prop.VerifiedDeal, mid, prop.StartEpoch, prop.EndEpoch, deal.ClientPeerID.String(),
			deal.FastRetrieval, deal.AnnounceToIPNI, tInfo.URL, headers)

		if err != nil {
			return false, xerrors.Errorf("store deal success: %w", err)
		}

		if n != 1 {
			return false, xerrors.Errorf("store deal success: updated %d rows instead of 1", n)
		}

		// Create piece park entry for online deals
		if !deal.IsOffline {
			var pieceID int64
			// Attempt to select the piece ID first
			err = tx.QueryRow(`SELECT id FROM parked_pieces WHERE piece_cid = $1`, prop.PieceCID.String()).Scan(&pieceID)

			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					// Piece does not exist, attempt to insert
					err = tx.QueryRow(`
							INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size)
							VALUES ($1, $2, $3)
							ON CONFLICT (piece_cid) DO NOTHING
							RETURNING id`, prop.PieceCID.String(), int64(prop.PieceSize), int64(deal.Transfer.Size)).Scan(&pieceID)
					if err != nil {
						return false, xerrors.Errorf("inserting new parked piece and getting id: %w", err)
					}
				} else {
					// Some other error occurred during select
					return false, xerrors.Errorf("checking existing parked piece: %w", err)
				}
			}

			// Add parked_piece_ref
			var refID int64
			err = tx.QueryRow(`INSERT INTO parked_piece_refs (piece_id, data_url, data_headers)
        			VALUES ($1, $2, $3) RETURNING ref_id`, pieceID, tInfo.URL, headers).Scan(&refID)
			if err != nil {
				return false, xerrors.Errorf("inserting parked piece ref: %w", err)
			}

			pieceIDUrl := url.URL{
				Scheme: "pieceref",
				Opaque: fmt.Sprintf("%d", refID),
			}

			_, err = tx.Exec(`INSERT INTO market_mk12_deal_pipeline (uuid, sp_id, piece_cid, piece_size, offline, url, raw_size, should_index, announce)
								VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) ON CONFLICT (uuid) DO NOTHING`,
				deal.DealUuid.String(), mid, prop.PieceCID.String(), prop.PieceSize, deal.IsOffline, pieceIDUrl.String(), deal.Transfer.Size,
				deal.FastRetrieval, deal.AnnounceToIPNI)
			if err != nil {
				return false, xerrors.Errorf("inserting deal into deal pipeline: %w", err)
			}

		} else {
			// Insert the offline deal into the deal pipeline
			_, err = tx.Exec(`INSERT INTO market_mk12_deal_pipeline (uuid, sp_id, piece_cid, piece_size, offline, should_index, announce)
								VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT (uuid) DO NOTHING`,
				deal.DealUuid.String(), mid, prop.PieceCID.String(), prop.PieceSize, deal.IsOffline, deal.FastRetrieval, deal.AnnounceToIPNI)
			if err != nil {
				return false, xerrors.Errorf("inserting deal into deal pipeline: %w", err)
			}
		}

		return true, nil
	}, harmonydb.OptionRetry())

	if err != nil {
		return &ProviderDealRejectionInfo{
			Reason: fmt.Sprintf("store deal: %s", err.Error()),
		}, nil
	}
	if !comm {
		return &ProviderDealRejectionInfo{
			Reason: "store deal: could not commit the transaction",
		}, nil
	}

	return &ProviderDealRejectionInfo{
		Accepted: true,
	}, nil
}
