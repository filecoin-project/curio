package helpers

import (
	"bytes"
	"fmt"
	"io"
	"math/bits"
	"os"
	"path/filepath"
	"testing"

	"github.com/ipfs/go-cid"
	carv2 "github.com/ipld/go-car/v2"
	"github.com/ipld/go-car/v2/blockstore"
	"github.com/stretchr/testify/require"

	"github.com/filecoin-project/go-data-segment/datasegment"
	commcid "github.com/filecoin-project/go-fil-commcid"
	commp "github.com/filecoin-project/go-fil-commp-hashhash"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/curio/lib/storiface"
	"github.com/filecoin-project/curio/lib/testutils"
	"github.com/filecoin-project/curio/market/mk20"

	"github.com/filecoin-project/lotus/storage/sealer/fr32"
)

type PieceFixture struct {
	RootCID    cid.Cid
	CarBytes   []byte
	PieceCIDV1 cid.Cid
	PieceCIDV2 cid.Cid
	PieceSize  abi.PaddedPieceSize
	RawSize    int64
}

func WriteParkedPieceFixture(dir string, pieceID int64, data []byte) error {
	path := filepath.Join(
		dir,
		storiface.FTPiece.String(),
		storiface.SectorName(storiface.PieceNumber(pieceID).Ref().ID),
	)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func CreatePieceFixture(t *testing.T, dir string, sourceSize int64) PieceFixture {
	t.Helper()

	srcPath, err := testutils.CreateRandomTmpFile(dir, sourceSize)
	require.NoError(t, err)

	root, carPath, err := testutils.CreateDenseCARWith(dir, srcPath, 64, 8, []carv2.Option{
		blockstore.WriteAsCarV1(true),
	})
	require.NoError(t, err)

	carBytes, err := os.ReadFile(carPath)
	require.NoError(t, err)

	return createRawPieceFixture(t, carBytes, root)
}

func createRawPieceFixture(t *testing.T, raw []byte, root cid.Cid) PieceFixture {
	t.Helper()

	wr := new(commp.Calc)
	defer wr.Reset()

	n, err := wr.Write(raw)
	require.NoError(t, err)
	digest, paddedPieceSize, err := wr.Digest()
	require.NoError(t, err)

	pieceCIDV1, err := commcid.DataCommitmentV1ToCID(digest)
	require.NoError(t, err)
	pieceCIDV2, err := commcid.DataCommitmentToPieceCidv2(digest, uint64(n))
	require.NoError(t, err)

	return PieceFixture{
		RootCID:    root,
		CarBytes:   raw,
		PieceCIDV1: pieceCIDV1,
		PieceCIDV2: pieceCIDV2,
		PieceSize:  abi.PaddedPieceSize(paddedPieceSize),
		RawSize:    int64(n),
	}
}

func CreateAggregateFixtureFromSubpieces(t *testing.T, subpieces []PieceFixture) (PieceFixture, []mk20.DataSource) {
	t.Helper()

	require.GreaterOrEqual(t, len(subpieces), 2)

	deals := make([]abi.PieceInfo, 0, len(subpieces))
	readers := make([]io.Reader, 0, len(subpieces))
	subSources := make([]mk20.DataSource, 0, len(subpieces))

	for _, sp := range subpieces {
		deals = append(deals, abi.PieceInfo{
			PieceCID: sp.PieceCIDV1,
			Size:     sp.PieceSize,
		})
		readers = append(readers, io.LimitReader(bytes.NewReader(sp.CarBytes), sp.RawSize))
		subSources = append(subSources, mk20.DataSource{
			PieceCID: sp.PieceCIDV2,
			Format: mk20.PieceDataFormat{
				Car: &mk20.FormatCar{},
			},
		})
	}

	_, aggregatedRawSize, err := datasegment.ComputeDealPlacement(deals)
	require.NoError(t, err)

	overallSize := abi.PaddedPieceSize(aggregatedRawSize)
	next := 1 << (64 - bits.LeadingZeros64(uint64(overallSize+256)))

	aggr, err := datasegment.NewAggregate(abi.PaddedPieceSize(next), deals)
	require.NoError(t, err)

	outR, err := aggr.AggregateObjectReader(readers)
	require.NoError(t, err)

	aggregateRaw, err := io.ReadAll(outR)
	require.NoError(t, err)

	fixture := createRawPieceFixture(t, aggregateRaw, cid.Undef)
	require.Equal(t, abi.PaddedPieceSize(next), fixture.PieceSize)

	return fixture, subSources
}

func WriteUnsealedSectorFixture(dir string, miner abi.ActorID, sector abi.SectorNumber, sectorSize abi.SectorSize, fixture PieceFixture) error {
	if fixture.PieceSize > abi.PaddedPieceSize(sectorSize) {
		return fmt.Errorf("fixture piece too large for sector: piece=%d sector=%d", fixture.PieceSize, sectorSize)
	}

	// Unsealed files are FR32 padded; write the piece at offset 0 and zero-fill the rest of the sector.
	paddedPiece := FR32PadFixture(fixture.CarBytes, fixture.PieceSize)
	sectorData := make([]byte, int(sectorSize))
	copy(sectorData, paddedPiece)

	sectorPath := filepath.Join(
		dir,
		storiface.FTUnsealed.String(),
		storiface.SectorName(abi.SectorID{Miner: miner, Number: sector}),
	)
	return os.WriteFile(sectorPath, sectorData, 0o644)
}

func FR32PadFixture(raw []byte, pieceSize abi.PaddedPieceSize) []byte {
	unpaddedLen := int(pieceSize.Unpadded())
	in := make([]byte, unpaddedLen)
	copy(in, raw)

	out := make([]byte, int(pieceSize))
	inOff := 0
	outOff := 0
	for inOff < len(in) {
		fr32.Pad(in[inOff:inOff+int(fr32.UnpaddedFr32Chunk)], out[outOff:outOff+int(fr32.PaddedFr32Chunk)])
		inOff += int(fr32.UnpaddedFr32Chunk)
		outOff += int(fr32.PaddedFr32Chunk)
	}
	return out
}
