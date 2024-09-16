package ffi

import (
	"bufio"
	"context"
	"io"
	"time"

	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/storiface"

	"github.com/filecoin-project/lotus/storage/sealer/fr32"
)

func (sb *SealCalls) CheckUnsealedCID(ctx context.Context, s storiface.SectorRef) (cid.Cid, error) {
	reader, err := sb.sectors.storage.ReaderSeq(ctx, s, storiface.FTUnsealed)
	if err != nil {
		return cid.Undef, xerrors.Errorf("getting unsealed sector reader: %w", err)
	}
	defer reader.Close()

	ssize, err := s.ProofType.SectorSize()
	if err != nil {
		return cid.Undef, xerrors.Errorf("getting sector size: %w", err)
	}

	startTime := time.Now()
	cc := new(proof.DataCidWriter)

	upReader, err := fr32.NewUnpadReader(bufio.NewReaderSize(io.LimitReader(reader, int64(ssize)), 1<<20), abi.PaddedPieceSize(ssize))
	if err != nil {
		return cid.Undef, xerrors.Errorf("creating unpad reader")
	}

	n, err := io.CopyBuffer(cc, upReader, make([]byte, abi.PaddedPieceSize(1<<20).Unpadded()))
	if err != nil {
		return cid.Undef, xerrors.Errorf("computing unsealed CID: %w", err)
	}

	res, err := cc.Sum()
	if err != nil {
		return cid.Undef, xerrors.Errorf("computing unsealed CID: %w", err)
	}

	log.Infow("computed unsealed CID", "cid", res, "size", n, "duration", time.Since(startTime), "MiB/s", float64(n)/time.Since(startTime).Seconds()/1024/1024)

	return res.PieceCID, nil
}
