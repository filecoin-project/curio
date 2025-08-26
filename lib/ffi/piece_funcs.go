package ffi

import (
	"context"
	"io"
	"os"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	storiface "github.com/filecoin-project/curio/lib/storiface"
)

func (sb *SealCalls) WritePiece(ctx context.Context, taskID *harmonytask.TaskID, pieceID storiface.PieceNumber, size int64, data io.Reader, storageType storiface.PathType) error {
	// Use storageType in AcquireSector
	paths, pathIDs, done, err := sb.Sectors.AcquireSector(ctx, taskID, pieceID.Ref(), storiface.FTNone, storiface.FTPiece, storageType)
	if err != nil {
		return err
	}

	skipDeclare := storiface.FTPiece
	defer func() {
		done(skipDeclare)
	}()

	dest := paths.Piece
	tempDest := dest + storiface.TempSuffix

	destFile, err := os.OpenFile(tempDest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return xerrors.Errorf("creating temp piece file '%s': %w", tempDest, err)
	}

	removeTemp := true
	defer func() {
		if removeTemp {
			rerr := os.Remove(tempDest)
			if rerr != nil {
				log.Errorf("removing temp file: %+v", rerr)
			}
		}
	}()

	copyStart := time.Now()

	n, err := io.CopyBuffer(destFile, io.LimitReader(data, size), make([]byte, 8<<20))
	if err != nil {
		_ = destFile.Close()
		return xerrors.Errorf("copying piece data: %w", err)
	}

	if err := destFile.Close(); err != nil {
		return xerrors.Errorf("closing temp piece file: %w", err)
	}

	if n != size {
		return xerrors.Errorf("short write: %d", n)
	}

	copyEnd := time.Now()

	log.Infow("wrote piece", "piece", pieceID, "size", size, "duration", copyEnd.Sub(copyStart), "dest", dest, "MiB/s", float64(size)/(1<<20)/copyEnd.Sub(copyStart).Seconds())

	if err := os.Rename(tempDest, dest); err != nil {
		return xerrors.Errorf("rename temp piece to dest %s -> %s: %w", tempDest, dest, err)
	}

	skipDeclare = storiface.FTNone
	removeTemp = false

	if err := sb.ensureOneCopy(ctx, pieceID.Ref().ID, pathIDs, storiface.FTPiece); err != nil {
		return xerrors.Errorf("ensure one copy: %w", err)
	}

	return nil
}

func (sb *SealCalls) PieceReader(ctx context.Context, id storiface.PieceNumber) (io.ReadCloser, error) {
	return sb.Sectors.storage.ReaderSeq(ctx, id.Ref(), storiface.FTPiece)
}

func (sb *SealCalls) RemovePiece(ctx context.Context, id storiface.PieceNumber) error {
	return sb.Sectors.storage.Remove(ctx, id.Ref().ID, storiface.FTPiece, true, nil)
}
