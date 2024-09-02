package ffi

import (
	"context"
	"os"
	"time"

	"golang.org/x/xerrors"

	"github.com/filecoin-project/curio/harmony/harmonytask"
	"github.com/filecoin-project/curio/lib/ffi/cunative"
	"github.com/filecoin-project/curio/lib/storiface"
)

func (sb *SealCalls) DecodeSDR(ctx context.Context, taskID harmonytask.TaskID, sector storiface.SectorRef) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	paths, pathIDs, releaseSector, err := sb.sectors.AcquireSector(ctx, &taskID, sector, storiface.FTNone, storiface.FTUnsealed, storiface.PathSealing)
	if err != nil {
		return xerrors.Errorf("acquiring sector paths: %w", err)
	}
	defer releaseSector()

	sealReader, err := sb.sectors.storage.ReaderSeq(ctx, sector, storiface.FTSealed)
	if err != nil {
		return xerrors.Errorf("getting unsealed sector reader: %w", err)
	}

	keyReader, err := sb.sectors.storage.ReaderSeq(ctx, sector, storiface.FTKey)
	if err != nil {
		return xerrors.Errorf("getting key reader: %w", err)
	}

	tempDest := paths.Unsealed + storiface.TempSuffix

	outFile, err := os.Create(tempDest)
	if err != nil {
		return xerrors.Errorf("creating unsealed file: %w", err)
	}

	start := time.Now()

	err = cunative.Decode(sealReader, keyReader, outFile)
	if err != nil {
		return xerrors.Errorf("decoding sealed sector: %w", err)
	}

	if err := outFile.Close(); err != nil {
		return xerrors.Errorf("closing unsealed file: %w", err)
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

	return nil
}
