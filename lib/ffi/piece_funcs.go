package ffi

import (
	"context"
	"io"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/storiface"
)

func (sb *SealCalls) WritePiece(ctx context.Context, taskID *harmonytask.TaskID, pieceID storiface.PieceNumber, size int64, data io.Reader, storageType storiface.PathType) error {
	return sb.Pieces.WritePiece(ctx, taskID, pieceID, size, data, storageType)
}

func (sb *SealCalls) PieceReader(ctx context.Context, id storiface.PieceNumber) (io.ReadCloser, error) {
	return sb.Pieces.PieceReader(ctx, id)
}

func (sb *SealCalls) RemovePiece(ctx context.Context, id storiface.PieceNumber) error {
	return sb.Pieces.RemovePiece(ctx, id)
}

func (sb *SealCalls) WriteUploadPiece(ctx context.Context, pieceID storiface.PieceNumber, size int64, data io.Reader, storageType storiface.PathType, verifySize bool) (abi.PieceInfo, uint64, error) {
	return sb.Pieces.WriteUploadPiece(ctx, pieceID, size, data, storageType, verifySize)
}
