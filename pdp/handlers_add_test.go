package pdp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTransformAddPiecesRequestRejectsMultiSubPieceBeforeDB(t *testing.T) {
	service := &PDPService{}

	_, _, err := service.transformAddPiecesRequest(context.Background(), "public", []AddPieceRequest{
		{
			PieceCID: "piece",
			SubPieces: []SubPieceEntry{
				{SubPieceCID: "sub-piece-1"},
				{SubPieceCID: "sub-piece-2"},
			},
		},
	})
	require.ErrorContains(t, err, "one subPiece")
}

func TestTransformAddPiecesRequestRejectsMissingSubPieceBeforeDB(t *testing.T) {
	service := &PDPService{}

	_, _, err := service.transformAddPiecesRequest(context.Background(), "public", []AddPieceRequest{
		{
			PieceCID:  "piece",
			SubPieces: nil,
		},
	})
	require.ErrorContains(t, err, "one subPiece")
}

func TestCountPieceAddIndexesCountsDuplicatePiecesAsSeparateAdds(t *testing.T) {
	pieceAdds := []pieceAdditionStatusInfo{
		{Piece: "same-piece", AddMessageIndex: 0, SubPieceOffset: 0},
		{Piece: "same-piece", AddMessageIndex: 1, SubPieceOffset: 0},
	}

	require.Equal(t, 2, countPieceAddIndexes(pieceAdds))
}

func TestCountPieceAddIndexesCountsRowsForSameAddOnce(t *testing.T) {
	pieceAdds := []pieceAdditionStatusInfo{
		{Piece: "same-piece", AddMessageIndex: 0, SubPieceOffset: 0},
		{Piece: "same-piece", AddMessageIndex: 0, SubPieceOffset: 128},
	}

	require.Equal(t, 1, countPieceAddIndexes(pieceAdds))
}
