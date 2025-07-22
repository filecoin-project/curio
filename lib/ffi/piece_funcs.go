package ffi

import (
	"context"
	"io"
	"os"
	"time"

	"golang.org/x/xerrors"

	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/storiface"
)

func (sb *SealCalls) WritePiece(ctx context.Context, taskID *harmonytask.TaskID, pieceID storiface.PieceNumber, size int64, data io.Reader, storageType storiface.PathType) error {
	// Use storageType in AcquireSector
	paths, _, done, err := sb.sectors.AcquireSector(ctx, taskID, pieceID.Ref(), storiface.FTNone, storiface.FTPiece, storageType)
	if err != nil {
		return err
	}
	defer done()

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

	removeTemp = false
	return nil
}

func (sb *SealCalls) PieceReader(ctx context.Context, id storiface.PieceNumber) (io.ReadCloser, error) {
	return sb.sectors.storage.ReaderSeq(ctx, id.Ref(), storiface.FTPiece)
}

func (sb *SealCalls) RemovePiece(ctx context.Context, id storiface.PieceNumber) error {
	return sb.sectors.storage.Remove(ctx, id.Ref().ID, storiface.FTPiece, true, nil)
}

func (sb *SealCalls) WriteUploadPiece(ctx context.Context, pieceID storiface.PieceNumber, size int64, data io.Reader, storageType storiface.PathType, verifySize bool) (abi.PieceInfo, uint64, error) {
	// Use storageType in AcquireSector
	paths, _, done, err := sb.sectors.AcquireSector(ctx, nil, pieceID.Ref(), storiface.FTNone, storiface.FTPiece, storageType)
	if err != nil {
		return abi.PieceInfo{}, 0, err
	}
	defer done()

	dest := paths.Piece
	tempDest := dest + storiface.TempSuffix

	destFile, err := os.OpenFile(tempDest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return abi.PieceInfo{}, 0, xerrors.Errorf("creating temp piece file '%s': %w", tempDest, err)
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

	wr := new(commp.Calc)
	writers := io.MultiWriter(wr, destFile)

	n, err := io.CopyBuffer(writers, io.LimitReader(data, size), make([]byte, 8<<20))
	if err != nil {
		_ = destFile.Close()
		return abi.PieceInfo{}, 0, xerrors.Errorf("copying piece data: %w", err)
	}

	if err := destFile.Close(); err != nil {
		return abi.PieceInfo{}, 0, xerrors.Errorf("closing temp piece file: %w", err)
	}

	if verifySize && n != size {
		return abi.PieceInfo{}, 0, xerrors.Errorf("short write: %d", n)
	}

	digest, pieceSize, err := wr.Digest()
	if err != nil {
		return abi.PieceInfo{}, 0, xerrors.Errorf("computing piece digest: %w", err)
	}

	pcid, err := commcid.DataCommitmentV1ToCID(digest)
	if err != nil {
		return abi.PieceInfo{}, 0, xerrors.Errorf("computing piece CID: %w", err)
	}
	psize := abi.PaddedPieceSize(pieceSize)

	copyEnd := time.Now()

	log.Infow("wrote piece", "piece", pieceID, "size", n, "duration", copyEnd.Sub(copyStart), "dest", dest, "MiB/s", float64(size)/(1<<20)/copyEnd.Sub(copyStart).Seconds())

	if err := os.Rename(tempDest, dest); err != nil {
		return abi.PieceInfo{}, 0, xerrors.Errorf("rename temp piece to dest %s -> %s: %w", tempDest, dest, err)
	}

	removeTemp = false
	return abi.PieceInfo{PieceCID: pcid, Size: psize}, uint64(n), nil
}
