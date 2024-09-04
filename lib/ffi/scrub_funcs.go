package ffi

import (
	"context"
	"github.com/filecoin-project/curio/lib/proof"
	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/ipfs/go-cid"
	"golang.org/x/xerrors"
	"io"
	"time"
)

func (sb *SealCalls) CheckUnsealedCID(ctx context.Context, sid abi.SectorID) (cid.Cid, error) {
	reader, err := sb.sectors.storage.ReaderSeq(ctx, storiface.SectorRef{ID: sid}, storiface.FTUnsealed)
	if err != nil {
		return cid.Undef, xerrors.Errorf("getting unsealed sector reader: %w", err)
	}
	defer reader.Close()

	startTime := time.Now()

	cc := new(proof.DataCidWriter)
	n, err := io.CopyBuffer(cc, reader, make([]byte, 1<<20))
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
