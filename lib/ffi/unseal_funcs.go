package ffi

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/ffi/cunative"
	"github.com/filecoin-project/curio/lib/storiface"
)

func (sb *SealCalls) decodeCommon(ctx context.Context, taskID harmonytask.TaskID, sector storiface.SectorRef, fileType storiface.SectorFileType, decodeFunc func(sealReader, keyReader io.Reader, outFile io.Writer) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	paths, pathIDs, releaseSector, err := sb.Sectors.AcquireSector(ctx, &taskID, sector, storiface.FTNone, storiface.FTUnsealed, storiface.PathStorage)
	if err != nil {
		return xerrors.Errorf("acquiring sector paths: %w", err)
	}
	defer releaseSector()

	sealReader, err := sb.Sectors.storage.ReaderSeq(ctx, sector, fileType)
	if err != nil {
		return xerrors.Errorf("getting sealed sector reader: %w", err)
	}

	keyReader, err := sb.Sectors.storage.ReaderSeq(ctx, sector, storiface.FTKey)
	if err != nil {
		return xerrors.Errorf("getting key reader: %w", err)
	}

	tempDest := paths.Unsealed + storiface.TempSuffix

	outFile, err := os.Create(tempDest)
	if err != nil {
		return xerrors.Errorf("creating unsealed file: %w", err)
	}
	defer outFile.Close()

	start := time.Now()

	err = decodeFunc(sealReader, keyReader, outFile)
	if err != nil {
		return xerrors.Errorf("decoding sealed sector: %w", err)
	}

	end := time.Now()

	ssize, err := sector.ProofType.SectorSize()
	if err != nil {
		return xerrors.Errorf("getting sector size: %w", err)
	}

	log.Infow("decoded sector", "sectorID", sector, "duration", end.Sub(start), "MiB/s", float64(ssize)/(1<<20)/end.Sub(start).Seconds())

	if err := os.Rename(tempDest, paths.Unsealed); err != nil {
		return xerrors.Errorf("renaming to unsealed file: %w", err)
	}

	if err := sb.ensureOneCopy(ctx, sector.ID, pathIDs, storiface.FTUnsealed); err != nil {
		return xerrors.Errorf("ensure one copy: %w", err)
	}

	if err := sb.Sectors.storage.Remove(ctx, sector.ID, storiface.FTKey, true, nil); err != nil {
		return err
	}

	return nil
}

func (sb *SealCalls) DecodeSDR(ctx context.Context, taskID harmonytask.TaskID, sector storiface.SectorRef) error {
	return sb.decodeCommon(ctx, taskID, sector, storiface.FTSealed, func(sealReader, keyReader io.Reader, outFile io.Writer) error {
		return cunative.Decode(sealReader, keyReader, outFile)
	})
}

func (sb *SealCalls) DecodeSnap(ctx context.Context, taskID harmonytask.TaskID, commD, commK cid.Cid, sector storiface.SectorRef) error {
	return sb.decodeCommon(ctx, taskID, sector, storiface.FTUpdate, func(sealReader, keyReader io.Reader, outFile io.Writer) error {
		return cunative.DecodeSnap(sector.ProofType, commD, commK, keyReader, sealReader, outFile)
	})
}
