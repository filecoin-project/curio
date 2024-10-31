// Package market
/*
This File contains all the implementation details of how to handle
the mk1.2 deals.
*/
package mk12

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	logging "github.com/ipfs/go-log/v2"
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

	"github.com/filecoin-project/curio/deps/config"
	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/lib/ffi"
	"github.com/filecoin-project/curio/market/mk12/legacytypes"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	ctypes "github.com/filecoin-project/lotus/chain/types"
)

var log = logging.Logger("mk12")

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
	cfg    *config.CurioConfig
	sm     map[address.Address]abi.SectorSize
}

type validationError struct {
	error
	// The reason sent to the client for why validation failed
	reason string
}

func NewMK12Handler(miners []address.Address, db *harmonydb.DB, sc *ffi.SealCalls, mapi MK12API, cfg *config.CurioConfig) (*MK12, error) {
	ctx := context.Background()

	sm := make(map[address.Address]abi.SectorSize)

	for _, m := range miners {
		info, err := mapi.StateMinerInfo(ctx, m, types.EmptyTSK)
		if err != nil {
			return nil, xerrors.Errorf("getting miner info: %w", err)
		}
		if _, ok := sm[m]; !ok {
			sm[m] = info.SectorSize
		}
	}

	return &MK12{
		miners: miners,
		db:     db,
		api:    mapi,
		sc:     sc,
		sm:     sm,
		cfg:    cfg,
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

	// Apply backpressure
	wait, err := m.maybeApplyBackpressure(ctx, ds.ClientDealProposal.Proposal.Provider)
	if err != nil {
		log.Errorf("applying backpressure: %w", err)
		return &ProviderDealRejectionInfo{
			Reason: "internal server error: failed to apply backpressure",
		}, nil
	}
	if wait {
		log.Infof("Rejected deal %s due to backpressure", ds.DealUuid.String())
		return &ProviderDealRejectionInfo{
			Reason: "deal rejected due to backpressure. Please retry in some time.",
		}, nil
	}

	// TODO: Add deal filters

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
			error:  xerrors.Errorf("node error getting most recent state id: %w", err),
		}
	}

	tok := head.Key().Bytes()
	curEpoch := head.Height()

	// Check that the proposal piece cid is defined before attempting signature
	// validation - if it's not defined, it won't be possible to marshall the
	// deal proposal to check the signature
	proposal := deal.ClientDealProposal.Proposal
	if !proposal.PieceCID.Defined() {
		return &validationError{error: xerrors.Errorf("proposal PieceCID undefined")}
	}

	if ok, err := m.validateClientSignature(ctx, deal); err != nil || !ok {
		if err != nil {
			return &validationError{
				reason: "server error: validating signature",
				error:  xerrors.Errorf("validateSignature failed: %w", err),
			}
		}
		return &validationError{
			reason: "invalid signature",
			error:  xerrors.Errorf("invalid signature"),
		}
	}

	// validate deal proposal
	if !lo.Contains(m.miners, proposal.Provider) {
		err := xerrors.Errorf("incorrect provider for deal; proposal.Provider: %s; provider.Address: %s", proposal.Provider, m.miners)
		return &validationError{error: err}
	}

	if proposal.Label.Length() > DealMaxLabelSize {
		err := xerrors.Errorf("deal label can be at most %d bytes, is %d", DealMaxLabelSize, proposal.Label.Length())
		return &validationError{error: err}
	}

	if err := proposal.PieceSize.Validate(); err != nil {
		err := xerrors.Errorf("proposal piece size is invalid: %w", err)
		return &validationError{error: err}
	}

	if proposal.PieceCID.Prefix() != market.PieceCIDPrefix {
		err := xerrors.Errorf("proposal PieceCID had wrong prefix")
		return &validationError{error: err}
	}

	if proposal.EndEpoch <= proposal.StartEpoch {
		err := xerrors.Errorf("proposal end %d before proposal start %d", proposal.EndEpoch, proposal.StartEpoch)
		return &validationError{error: err}
	}

	if curEpoch > proposal.StartEpoch {
		err := xerrors.Errorf("deal start epoch %d has already elapsed (current epoch: %d)", proposal.StartEpoch, curEpoch)
		return &validationError{error: err}
	}

	// Check that the delta between the start and end epochs (the deal
	// duration) is within acceptable bounds
	minDuration, maxDuration := market.DealDurationBounds(proposal.PieceSize)
	if proposal.Duration() < minDuration || proposal.Duration() > maxDuration {
		err := xerrors.Errorf("deal duration out of bounds (min, max, provided): %d, %d, %d", minDuration, maxDuration, proposal.Duration())
		return &validationError{error: err}
	}

	// Check that the proposed end epoch isn't too far beyond the current epoch
	maxEndEpoch := curEpoch + miner.MaxSectorExpirationExtension
	if proposal.EndEpoch > maxEndEpoch {
		err := xerrors.Errorf("invalid deal end epoch %d: cannot be more than %d past current epoch %d", proposal.EndEpoch, miner.MaxSectorExpirationExtension, curEpoch)
		return &validationError{error: err}
	}

	bounds, err := m.api.StateDealProviderCollateralBounds(ctx, proposal.PieceSize, proposal.VerifiedDeal, ctypes.EmptyTSK)
	if err != nil {
		return &validationError{
			reason: "server error: getting collateral bounds",
			error:  xerrors.Errorf("node error getting collateral bounds: %w", err),
		}
	}

	// The maximum amount of collateral that the provider will put into escrow
	// for a deal is calculated as a multiple of the minimum bounded amount
	maxC := ctypes.BigMul(bounds.Min, ctypes.NewInt(maxDealCollateralMultiplier))

	pcMin := bounds.Min
	pcMax := maxC

	if proposal.ProviderCollateral.LessThan(pcMin) {
		err := xerrors.Errorf("proposed provider collateral %s below minimum %s", proposal.ProviderCollateral, pcMin)
		return &validationError{error: err}
	}

	if proposal.ProviderCollateral.GreaterThan(pcMax) {
		err := xerrors.Errorf("proposed provider collateral %s above maximum %s", proposal.ProviderCollateral, pcMax)
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
			error:  xerrors.Errorf("node error getting client market balance failed: %w", err),
		}
	}

	clientMarketBalance := ToSharedBalance(bal)

	// This doesn't guarantee that the client won't withdraw / lock those funds
	// but it's a decent first filter
	if clientMarketBalance.Available.LessThan(proposal.ClientBalanceRequirement()) {
		err := xerrors.Errorf("client available funds in escrow %d not enough to meet storage cost for deal %d", clientMarketBalance.Available, proposal.ClientBalanceRequirement())
		return &validationError{error: err}
	}

	// Verified deal checks
	if proposal.VerifiedDeal {
		// Get data cap
		dataCap, err := m.api.StateVerifiedClientStatus(ctx, proposal.Client, tsk)
		if err != nil {
			return &validationError{
				reason: "server error: getting verified datacap",
				error:  xerrors.Errorf("node error fetching verified data cap: %w", err),
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
			err := xerrors.Errorf("verified deal DataCap %d too small for proposed piece size %d", dataCap, pieceSize)
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
		return xerrors.Errorf("storage price per epoch less than asking price: %s < %s", proposal.StoragePricePerEpoch, minPrice)
	}

	if proposal.PieceSize < ask.MinPieceSize {
		return xerrors.Errorf("piece size less than minimum required size: %d < %d", proposal.PieceSize, ask.MinPieceSize)
	}

	if proposal.PieceSize > ask.MaxPieceSize {
		return xerrors.Errorf("piece size more than maximum allowed size: %d > %d", proposal.PieceSize, ask.MaxPieceSize)
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
	if deal.Transfer.Type == Libp2pScheme {
		return &ProviderDealRejectionInfo{
			Reason: "libp2p URLs are not supported by this provider",
		}, nil
	}

	prop := deal.ClientDealProposal.Proposal

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

	// Cbor marshal the Deal Label manually as non-string label will result in "" with JSON marshal
	label := prop.Label
	b := new(bytes.Buffer)
	err = label.MarshalCBOR(b)
	if err != nil {
		return &ProviderDealRejectionInfo{
			Reason: fmt.Sprintf("cbor marshal label: %s", err),
		}, nil
	}

	comm, err := m.db.BeginTransaction(ctx, func(tx *harmonydb.Tx) (commit bool, err error) {
		n, err := tx.Exec(`INSERT INTO market_mk12_deals (uuid, signed_proposal_cid, 
                                proposal_signature, proposal, piece_cid, 
                                piece_size, offline, verified, sp_id, start_epoch, end_epoch, 
                                client_peer_id, fast_retrieval, announce_to_ipni, url, url_headers, label) 
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
				ON CONFLICT (uuid) DO NOTHING`,
			deal.DealUuid.String(), deal.SignedProposalCID.String(), sigByte, propJson, prop.PieceCID.String(),
			prop.PieceSize, deal.IsOffline, prop.VerifiedDeal, mid, prop.StartEpoch, prop.EndEpoch, deal.ClientPeerID.String(),
			deal.FastRetrieval, deal.AnnounceToIPNI, tInfo.URL, headers, b.Bytes())

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

// maybeApplyBackpressure applies backpressure to the deal processing pipeline if certain conditions are met
// Check if ConcurrentDealSize > MaxConcurrentDealSize
// Check if WaitDealSectors > MaxQueueDealSector
// Check for buffered sector at each state of pipeline to their respective Max
func (m *MK12) maybeApplyBackpressure(ctx context.Context, maddr address.Address) (wait bool, err error) {
	var totalSize int64
	err = m.db.QueryRow(ctx, `SELECT COALESCE(SUM(piece_size), 0) AS total_piece_size
							FROM market_mk12_deal_pipeline
							WHERE sector IS NULL`).Scan(&totalSize)
	if err != nil {
		return false, xerrors.Errorf("failed to get cumulative deal size in process from DB: %w", err)
	}

	if totalSize > m.cfg.Market.StorageMarketConfig.MK12.MaxConcurrentDealSize {
		log.Infow("backpressure", "reason", "too many deals in process", "ConcurrentDealSize", totalSize, "max", m.cfg.Market.StorageMarketConfig.MK12.MaxConcurrentDealSize)
		return true, nil
	}

	cfg := m.cfg.Ingest

	if cfg.DoSnap {
		var bufferedEncode, bufferedProve, waitDealSectors int
		err = m.db.QueryRow(ctx, `
		WITH BufferedEncode AS (
			SELECT COUNT(p.task_id_encode) - COUNT(t.owner_id) AS buffered_encode
			FROM sectors_snap_pipeline p
					 LEFT JOIN harmony_task t ON p.task_id_encode = t.id
			WHERE p.after_encode = false
		),
		 BufferedProve AS (
			 SELECT COUNT(p.task_id_prove) - COUNT(t.owner_id) AS buffered_prove
			 FROM sectors_snap_pipeline p
					  LEFT JOIN harmony_task t ON p.task_id_prove = t.id
			 WHERE p.after_prove = true AND p.after_move_storage = false
		 ),
		 WaitDealSectors AS (
			SELECT COUNT(DISTINCT osp.sector_number) AS wait_deal_sectors_count
			FROM open_sector_pieces osp
				 LEFT JOIN sectors_snap_initial_pieces sip 
				 ON osp.sector_number = sip.sector_number
			WHERE sip.sector_number IS NULL
		)
		SELECT
			(SELECT buffered_encode FROM BufferedEncode) AS total_encode,
			(SELECT buffered_prove FROM BufferedProve) AS buffered_prove,
			(SELECT wait_deal_sectors_count FROM WaitDealSectors) AS wait_deal_sectors_count
		`).Scan(&bufferedEncode, &bufferedProve, &waitDealSectors)
		if err != nil {
			return false, xerrors.Errorf("counting buffered sectors: %w", err)
		}

		if cfg.MaxQueueDealSector != 0 && waitDealSectors > cfg.MaxQueueDealSector {
			log.Infow("backpressure", "reason", "too many wait deal sectors", "wait_deal_sectors", waitDealSectors, "max", cfg.MaxQueueDealSector)
			return true, nil
		}

		if cfg.MaxQueueSnapEncode != 0 && bufferedEncode > cfg.MaxQueueSnapEncode {
			log.Infow("backpressure", "reason", "too many encode tasks", "buffered", bufferedEncode, "max", cfg.MaxQueueSnapEncode)
			return true, nil
		}

		if cfg.MaxQueueSnapProve != 0 && bufferedProve > cfg.MaxQueueSnapProve {
			log.Infow("backpressure", "reason", "too many prove tasks", "buffered", bufferedProve, "max", cfg.MaxQueueSnapProve)
			return
		}
	} else {
		var bufferedSDR, bufferedTrees, bufferedPoRep, waitDealSectors int
		err = m.db.QueryRow(ctx, `
		WITH BufferedSDR AS (
			SELECT COUNT(p.task_id_sdr) - COUNT(t.owner_id) AS buffered_sdr_count
			FROM sectors_sdr_pipeline p
			LEFT JOIN harmony_task t ON p.task_id_sdr = t.id
			WHERE p.after_sdr = false
		),
		BufferedTrees AS (
			SELECT COUNT(p.task_id_tree_r) - COUNT(t.owner_id) AS buffered_trees_count
			FROM sectors_sdr_pipeline p
			LEFT JOIN harmony_task t ON p.task_id_tree_r = t.id
			WHERE p.after_sdr = true AND p.after_tree_r = false
		),
		BufferedPoRep AS (
			SELECT COUNT(p.task_id_porep) - COUNT(t.owner_id) AS buffered_porep_count
			FROM sectors_sdr_pipeline p
			LEFT JOIN harmony_task t ON p.task_id_porep = t.id
			WHERE p.after_tree_r = true AND p.after_porep = false
		),
		WaitDealSectors AS (
			SELECT COUNT(DISTINCT osp.sector_number) AS wait_deal_sectors_count
			FROM open_sector_pieces osp
				 LEFT JOIN sectors_sdr_initial_pieces sip 
				 ON osp.sector_number = sip.sector_number
			WHERE sip.sector_number IS NULL
		)
		SELECT
			(SELECT buffered_sdr_count FROM BufferedSDR) AS total_buffered_sdr,
			(SELECT buffered_trees_count FROM BufferedTrees) AS buffered_trees_count,
			(SELECT buffered_porep_count FROM BufferedPoRep) AS buffered_porep_count,
			(SELECT wait_deal_sectors_count FROM WaitDealSectors) AS wait_deal_sectors_count
		`).Scan(&bufferedSDR, &bufferedTrees, &bufferedPoRep, &waitDealSectors)
		if err != nil {
			return false, xerrors.Errorf("counting buffered sectors: %w", err)
		}

		if cfg.MaxQueueDealSector != 0 && waitDealSectors > cfg.MaxQueueDealSector {
			log.Infow("backpressure", "reason", "too many wait deal sectors", "wait_deal_sectors", waitDealSectors, "max", cfg.MaxQueueDealSector)
			return true, nil
		}

		if bufferedSDR > cfg.MaxQueueSDR {
			log.Infow("backpressure", "reason", "too many SDR tasks", "buffered", bufferedSDR, "max", cfg.MaxQueueSDR)
			return true, nil
		}
		if cfg.MaxQueueTrees != 0 && bufferedTrees > cfg.MaxQueueTrees {
			log.Infow("backpressure", "reason", "too many tree tasks", "buffered", bufferedTrees, "max", cfg.MaxQueueTrees)
			return true, nil
		}
		if cfg.MaxQueuePoRep != 0 && bufferedPoRep > cfg.MaxQueuePoRep {
			log.Infow("backpressure", "reason", "too many PoRep tasks", "buffered", bufferedPoRep, "max", cfg.MaxQueuePoRep)
			return true, nil
		}
	}

	return false, nil
}
