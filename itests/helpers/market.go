package helpers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"

	"github.com/ipfs/go-cid"
	"github.com/oklog/ulid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-address"
	cborutil "github.com/filecoin-project/go-cbor-util"
	"github.com/filecoin-project/go-state-types/abi"
	verifreg13 "github.com/filecoin-project/go-state-types/builtin/v13/verifreg"
	verifreg16 "github.com/filecoin-project/go-state-types/builtin/v16/verifreg"
	market9 "github.com/filecoin-project/go-state-types/builtin/v9/market"

	"github.com/filecoin-project/curio/harmony/harmonydb"
	"github.com/filecoin-project/curio/market/mk20"

	"github.com/filecoin-project/lotus/api/v1api"
	"github.com/filecoin-project/lotus/chain/types"
)

type MK12SignedProposal struct {
	Proposal          market9.DealProposal
	SignedProposalCID cid.Cid
	ProposalCID       cid.Cid
	ProposalJSON      []byte
	SignatureBytes    []byte
	LabelCBOR         []byte
}

func BuildSignedMK12Proposal(ctx context.Context, full v1api.FullNode, clientAddr address.Address, providerAddr address.Address, root cid.Cid, pieceCID cid.Cid, pieceSize abi.PaddedPieceSize, startEpoch abi.ChainEpoch, endEpoch abi.ChainEpoch, verified bool, providerCollateral abi.TokenAmount) (*MK12SignedProposal, error) {
	label, err := market9.NewLabelFromString(root.String())
	if err != nil {
		return nil, xerrors.Errorf("new deal label: %w", err)
	}

	proposal := market9.DealProposal{
		PieceCID:             pieceCID,
		PieceSize:            pieceSize,
		VerifiedDeal:         verified,
		Client:               clientAddr,
		Provider:             providerAddr,
		Label:                label,
		StartEpoch:           startEpoch,
		EndEpoch:             endEpoch,
		StoragePricePerEpoch: abi.NewTokenAmount(0),
		ProviderCollateral:   providerCollateral,
		ClientCollateral:     abi.NewTokenAmount(0),
	}

	buf, err := cborutil.Dump(&proposal)
	if err != nil {
		return nil, xerrors.Errorf("dump proposal cbor: %w", err)
	}

	sig, err := full.WalletSign(ctx, clientAddr, buf)
	if err != nil {
		return nil, xerrors.Errorf("wallet sign proposal: %w", err)
	}

	sigBytes, err := sig.MarshalBinary()
	if err != nil {
		return nil, xerrors.Errorf("marshal signature: %w", err)
	}

	proposalCID, err := proposal.Cid()
	if err != nil {
		return nil, xerrors.Errorf("proposal cid: %w", err)
	}

	signedProposalNode, err := cborutil.AsIpld(&market9.ClientDealProposal{
		Proposal:        proposal,
		ClientSignature: *sig,
	})
	if err != nil {
		return nil, xerrors.Errorf("signed proposal ipld: %w", err)
	}

	proposalJSON, err := json.Marshal(proposal)
	if err != nil {
		return nil, xerrors.Errorf("marshal proposal json: %w", err)
	}

	labelCBOR := new(bytes.Buffer)
	if err := label.MarshalCBOR(labelCBOR); err != nil {
		return nil, xerrors.Errorf("marshal label cbor: %w", err)
	}

	return &MK12SignedProposal{
		Proposal:          proposal,
		SignedProposalCID: signedProposalNode.Cid(),
		ProposalCID:       proposalCID,
		ProposalJSON:      proposalJSON,
		SignatureBytes:    sigBytes,
		LabelCBOR:         labelCBOR.Bytes(),
	}, nil
}

func ProviderCollateralBounds(ctx context.Context, full v1api.FullNode, pieceSize abi.PaddedPieceSize, verified bool) (abi.TokenAmount, error) {
	bounds, err := full.StateDealProviderCollateralBounds(ctx, pieceSize, verified, types.EmptyTSK)
	if err != nil {
		return abi.TokenAmount{}, xerrors.Errorf("state deal provider collateral bounds: %w", err)
	}
	return bounds.Min, nil
}

func InsertCompletedParkedPiece(tx *harmonydb.Tx, pieceCID string, pieceSize abi.PaddedPieceSize, rawSize int64, longTerm bool) (int64, error) {
	var pieceID int64
	err := tx.QueryRow(
		`INSERT INTO parked_pieces (piece_cid, piece_padded_size, piece_raw_size, complete, long_term)
		 VALUES ($1, $2, $3, TRUE, $4)
		 RETURNING id`,
		pieceCID, pieceSize, rawSize, longTerm,
	).Scan(&pieceID)
	if err != nil {
		return 0, xerrors.Errorf("insert completed parked piece: %w", err)
	}
	return pieceID, nil
}

func InsertParkedPieceRef(tx *harmonydb.Tx, pieceID int64, dataURL string, headers []byte, longTerm bool) (int64, error) {
	if len(headers) == 0 {
		headers = []byte("{}")
	}

	var refID int64
	err := tx.QueryRow(
		`INSERT INTO parked_piece_refs (piece_id, data_url, data_headers, long_term)
		 VALUES ($1, $2, $3, $4)
		 RETURNING ref_id`,
		pieceID, dataURL, headers, longTerm,
	).Scan(&refID)
	if err != nil {
		return 0, xerrors.Errorf("insert parked piece ref: %w", err)
	}
	return refID, nil
}

type MK12F05PendingSeed struct {
	UUID          string
	SPID          int64
	PieceCID      string
	PieceSize     abi.PaddedPieceSize
	RawSize       int64
	Offline       bool
	URL           string
	ShouldIndex   bool
	Announce      bool
	ClientPeerID  string
	FastRetrieval bool
	Signed        *MK12SignedProposal
}

func SeedMK12F05PendingDeal(tx *harmonydb.Tx, s MK12F05PendingSeed) error {
	if s.Signed == nil {
		return xerrors.Errorf("signed proposal is required")
	}

	n, err := tx.Exec(`INSERT INTO market_mk12_deals (
			uuid, sp_id, signed_proposal_cid,
			proposal_signature, proposal, proposal_cid,
			offline, verified, start_epoch, end_epoch,
			client_peer_id, piece_cid, piece_size, raw_size,
			fast_retrieval, announce_to_ipni,
			url, url_headers, label
		) VALUES ($1, $2, $3,
			$4, $5::jsonb, $6,
			$7, $8, $9, $10,
			$11, $12, $13, $14,
			$15, $16,
			$17, '{}'::jsonb, $18)`,
		s.UUID, s.SPID, s.Signed.SignedProposalCID.String(),
		s.Signed.SignatureBytes, string(s.Signed.ProposalJSON), s.Signed.ProposalCID.String(),
		s.Offline, s.Signed.Proposal.VerifiedDeal, s.Signed.Proposal.StartEpoch, s.Signed.Proposal.EndEpoch,
		s.ClientPeerID, s.PieceCID, s.PieceSize, s.RawSize,
		s.FastRetrieval, s.Announce,
		s.URL, s.Signed.LabelCBOR,
	)
	if err != nil {
		return xerrors.Errorf("insert market_mk12_deals row: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("insert market_mk12_deals row: expected 1 row, got %d", n)
	}

	n, err = tx.Exec(`INSERT INTO market_mk12_deal_pipeline (
			uuid, sp_id, piece_cid, piece_size, raw_size,
			offline, url, should_index, announce, is_ddo
		) VALUES ($1, $2, $3, $4, $5,
			$6, $7, $8, $9, FALSE)`,
		s.UUID, s.SPID, s.PieceCID, s.PieceSize, s.RawSize,
		s.Offline, s.URL, s.ShouldIndex, s.Announce,
	)
	if err != nil {
		return xerrors.Errorf("insert market_mk12_deal_pipeline row: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("insert market_mk12_deal_pipeline row: expected 1 row, got %d", n)
	}

	return nil
}

type MK12DDOPendingSeed struct {
	UUID          string
	SPID          int64
	Client        string
	PieceCID      string
	PieceSize     abi.PaddedPieceSize
	RawSize       int64
	Offline       bool
	URL           string
	ShouldIndex   bool
	Announce      bool
	FastRetrieval bool
	Verified      bool
	StartEpoch    abi.ChainEpoch
	EndEpoch      abi.ChainEpoch
	AllocationID  int64
}

func SeedMK12DDOPendingDeal(tx *harmonydb.Tx, s MK12DDOPendingSeed) error {
	n, err := tx.Exec(`INSERT INTO market_direct_deals (
			uuid, sp_id, client, offline, verified,
			start_epoch, end_epoch, allocation_id,
			piece_cid, piece_size, raw_size,
			fast_retrieval, announce_to_ipni
		) VALUES ($1, $2, $3, $4, $5,
			$6, $7, $8,
			$9, $10, $11,
			$12, $13)`,
		s.UUID, s.SPID, s.Client, s.Offline, s.Verified,
		s.StartEpoch, s.EndEpoch, s.AllocationID,
		s.PieceCID, s.PieceSize, s.RawSize,
		s.FastRetrieval, s.Announce,
	)
	if err != nil {
		return xerrors.Errorf("insert market_direct_deals row: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("insert market_direct_deals row: expected 1 row, got %d", n)
	}

	n, err = tx.Exec(`INSERT INTO market_mk12_deal_pipeline (
			uuid, sp_id, piece_cid, piece_size, raw_size,
			offline, url, should_index, announce, is_ddo
		) VALUES ($1, $2, $3, $4, $5,
			$6, $7, $8, $9, TRUE)`,
		s.UUID, s.SPID, s.PieceCID, s.PieceSize, s.RawSize,
		s.Offline, s.URL, s.ShouldIndex, s.Announce,
	)
	if err != nil {
		return xerrors.Errorf("insert market_mk12_deal_pipeline row for ddo: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("insert market_mk12_deal_pipeline row for ddo: expected 1 row, got %d", n)
	}

	return nil
}

type MK20PendingSeed struct {
	DealID       string
	Client       string
	Provider     address.Address
	Contract     string
	PieceCIDV2   cid.Cid
	Offline      bool
	SourceURL    string
	Indexing     bool
	Announce     bool
	AllocationID *verifreg13.AllocationId
	Duration     abi.ChainEpoch
}

func SeedMK20PendingDeal(tx *harmonydb.Tx, s MK20PendingSeed) error {
	id, err := ulid.Parse(s.DealID)
	if err != nil {
		return xerrors.Errorf("parse mk20 deal id: %w", err)
	}

	ds := &mk20.DataSource{
		PieceCID: s.PieceCIDV2,
		Format: mk20.PieceDataFormat{
			Car: &mk20.FormatCar{},
		},
	}

	if s.Offline {
		ds.SourceOffline = &mk20.DataSourceOffline{}
	} else {
		ds.SourceHTTP = &mk20.DataSourceHTTP{
			URLs: []mk20.HttpUrl{{
				URL:      s.SourceURL,
				Headers:  http.Header{},
				Priority: 0,
				Fallback: false,
			}},
		}
	}

	var allocationID *verifreg16.AllocationId
	if s.AllocationID != nil {
		alloc := verifreg16.AllocationId(*s.AllocationID)
		allocationID = &alloc
	}

	deal := &mk20.Deal{
		Identifier: id,
		Client:     s.Client,
		Data:       ds,
		Products: mk20.Products{
			DDOV1: &mk20.DDOV1{
				Provider:        s.Provider,
				PieceManager:    s.Provider,
				Duration:        s.Duration,
				AllocationId:    allocationID,
				ContractAddress: s.Contract,
			},
			RetrievalV1: &mk20.RetrievalV1{
				Indexing:        s.Indexing,
				AnnouncePayload: s.Announce,
			},
		},
	}

	if err := deal.SaveToDB(tx); err != nil {
		return xerrors.Errorf("save mk20 deal row: %w", err)
	}

	n, err := tx.Exec(`INSERT INTO market_mk20_pipeline_waiting (id) VALUES ($1)`, s.DealID)
	if err != nil {
		return xerrors.Errorf("insert mk20 pipeline waiting row: %w", err)
	}
	if n != 1 {
		return xerrors.Errorf("insert mk20 pipeline waiting row: expected 1 row, got %d", n)
	}

	if s.Offline {
		n, err = tx.Exec(`INSERT INTO market_mk20_offline_urls (id, piece_cid_v2, url, headers)
			VALUES ($1, $2, $3, '{}'::jsonb)`,
			s.DealID, s.PieceCIDV2.String(), s.SourceURL,
		)
		if err != nil {
			return xerrors.Errorf("insert mk20 offline url row: %w", err)
		}
		if n != 1 {
			return xerrors.Errorf("insert mk20 offline url row: expected 1 row, got %d", n)
		}
	}

	return nil
}

type ProcessPieceDealParams struct {
	DealID        string
	PieceCID      string
	BoostDeal     bool
	SPID          int64
	SectorNum     int64
	PieceOffset   any
	PieceLength   int64
	RawSize       int64
	FastRetrieval bool
	PieceRefID    any
	LegacyDeal    bool
	LegacyDealID  int64
}

func ProcessPieceDealTx(tx *harmonydb.Tx, p ProcessPieceDealParams) error {
	_, err := tx.Exec(`SELECT process_piece_deal($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		p.DealID,
		p.PieceCID,
		p.BoostDeal,
		p.SPID,
		p.SectorNum,
		p.PieceOffset,
		p.PieceLength,
		p.RawSize,
		p.FastRetrieval,
		p.PieceRefID,
		p.LegacyDeal,
		p.LegacyDealID,
	)
	if err != nil {
		return xerrors.Errorf("process_piece_deal for %s: %w", p.DealID, err)
	}
	return nil
}
