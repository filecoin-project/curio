package sector

import (
	"context"
	"encoding/json"
	"io"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/builtin/v9/verifreg"
	"github.com/filecoin-project/go-state-types/network"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/storage/pipeline/piece"
)

type UniversalPieceInfo interface {
	Impl() piece.PieceDealInfo
	String() string
	Key() piece.PieceKey

	Valid(nv network.Version) error
	StartEpoch() (abi.ChainEpoch, error)
	EndEpoch() (abi.ChainEpoch, error)
	PieceCID() cid.Cid
	KeepUnsealedRequested() bool

	GetAllocation(ctx context.Context, aapi piece.AllocationAPI, tsk types.TipSetKey) (*verifreg.Allocation, error)
}

// SafeSectorPiece is a wrapper around SectorPiece which makes it hard to misuse
// especially by making it hard to access raw Deal / DDO info
type SafeSectorPiece struct {
	real api.SectorPiece
}

func SafePiece(piece api.SectorPiece) SafeSectorPiece {
	return SafeSectorPiece{piece}
}

var _ UniversalPieceInfo = &SafeSectorPiece{}

func (sp *SafeSectorPiece) Piece() abi.PieceInfo {
	return sp.real.Piece
}

func (sp *SafeSectorPiece) HasDealInfo() bool {
	return sp.real.DealInfo != nil
}

func (sp *SafeSectorPiece) DealInfo() UniversalPieceInfo {
	return sp.real.DealInfo
}

// cbor passthrough
func (sp *SafeSectorPiece) UnmarshalCBOR(r io.Reader) (err error) {
	return sp.real.UnmarshalCBOR(r)
}

func (sp *SafeSectorPiece) MarshalCBOR(w io.Writer) error {
	return sp.real.MarshalCBOR(w)
}

// json passthrough
func (sp *SafeSectorPiece) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &sp.real)
}

func (sp *SafeSectorPiece) MarshalJSON() ([]byte, error) {
	return json.Marshal(sp.real)
}

type handleDealInfoParams struct {
	FillerHandler        func(UniversalPieceInfo) error
	BuiltinMarketHandler func(UniversalPieceInfo) error
	DDOHandler           func(UniversalPieceInfo) error
}

func (sp *SafeSectorPiece) handleDealInfo(params handleDealInfoParams) error {
	if !sp.HasDealInfo() {
		if params.FillerHandler == nil {
			return xerrors.Errorf("FillerHandler is not provided")
		}
		return params.FillerHandler(sp)
	}

	if sp.real.DealInfo.PublishCid != nil {
		if params.BuiltinMarketHandler == nil {
			return xerrors.Errorf("BuiltinMarketHandler is not provided")
		}
		return params.BuiltinMarketHandler(sp)
	}

	if params.DDOHandler == nil {
		return xerrors.Errorf("DDOHandler is not provided")
	}
	return params.DDOHandler(sp)
}

// SectorPiece Proxy

func (sp *SafeSectorPiece) Impl() piece.PieceDealInfo {
	if !sp.HasDealInfo() {
		return piece.PieceDealInfo{}
	}

	return sp.real.DealInfo.Impl()
}

func (sp *SafeSectorPiece) String() string {
	if !sp.HasDealInfo() {
		return "<no deal info>"
	}

	return sp.real.DealInfo.String()
}

func (sp *SafeSectorPiece) Key() piece.PieceKey {
	return sp.real.DealInfo.Key()
}

func (sp *SafeSectorPiece) Valid(nv network.Version) error {
	return sp.real.DealInfo.Valid(nv)
}

func (sp *SafeSectorPiece) StartEpoch() (abi.ChainEpoch, error) {
	if !sp.HasDealInfo() {
		return 0, xerrors.Errorf("no deal info")
	}

	return sp.real.DealInfo.StartEpoch()
}

func (sp *SafeSectorPiece) EndEpoch() (abi.ChainEpoch, error) {
	if !sp.HasDealInfo() {
		return 0, xerrors.Errorf("no deal info")
	}

	return sp.real.DealInfo.EndEpoch()
}

func (sp *SafeSectorPiece) PieceCID() cid.Cid {
	if !sp.HasDealInfo() {
		return sp.real.Piece.PieceCID
	}

	return sp.real.DealInfo.PieceCID()
}

func (sp *SafeSectorPiece) KeepUnsealedRequested() bool {
	if !sp.HasDealInfo() {
		return false
	}

	return sp.real.DealInfo.KeepUnsealedRequested()
}

func (sp *SafeSectorPiece) GetAllocation(ctx context.Context, aapi piece.AllocationAPI, tsk types.TipSetKey) (*verifreg.Allocation, error) {
	if !sp.HasDealInfo() {
		return nil, xerrors.Errorf("no deal info")
	}

	return sp.real.DealInfo.GetAllocation(ctx, aapi, tsk)
}
