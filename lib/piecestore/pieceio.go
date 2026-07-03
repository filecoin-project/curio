package piecestore

import (
	"context"
	"io"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/storiface"
)

// PieceIO provides FFI-free piece storage operations.
type PieceIO interface {
	WritePiece(ctx context.Context, taskID *harmonytask.TaskID, pieceID storiface.PieceNumber, size int64, data io.Reader, storageType storiface.PathType) error
	WriteUploadPiece(ctx context.Context, pieceID storiface.PieceNumber, size int64, data io.Reader, storageType storiface.PathType, verifySize bool) (abi.PieceInfo, uint64, error)
	PieceReader(ctx context.Context, id storiface.PieceNumber) (io.ReadCloser, error)
	RemovePiece(ctx context.Context, id storiface.PieceNumber) error
}
