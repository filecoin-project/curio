package cunative

import (
	"bytes"
	"crypto/rand"
	"github.com/detailyang/go-fallocate"
	"github.com/filecoin-project/curio/lib/proof"
	ffi "github.com/filecoin-project/filecoin-ffi"
	commp2 "github.com/filecoin-project/go-commp-utils/v2"
	"github.com/filecoin-project/go-commp-utils/zerocomm"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/lib/nullreader"
	"github.com/filecoin-project/lotus/storage/sealer/fr32"
	"github.com/stretchr/testify/require"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapDecode(t *testing.T) {
	spt := abi.RegisteredSealProof_StackedDrg2KiBV1_1

	td := t.TempDir()
	cache := filepath.Join(td, "cache")
	unseal := filepath.Join(td, "unsealed")
	sealKey := filepath.Join(td, "sealed")

	require.NoError(t, os.MkdirAll(cache, 0755))

	ssize, err := spt.SectorSize()
	require.NoError(t, err)

	// write null "unsealed"
	{
		uf, err := os.Create(unseal)
		require.NoError(t, err)
		_, err = io.CopyN(uf, &nullreader.Reader{}, int64(ssize))
		require.NoError(t, err)
		require.NoError(t, uf.Close())
	}

	{
		// proofs are really dumb
		f, err := os.Create(sealKey)
		require.NoError(t, err)
		err = f.Close()
		require.NoError(t, err)
	}

	snum := abi.SectorNumber(123)
	miner := abi.ActorID(545)
	ticket := abi.SealRandomness{1, 2, 3, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	pieces := []abi.PieceInfo{{
		Size:     abi.PaddedPieceSize(ssize),
		PieceCID: zerocomm.ZeroPieceCommitment(abi.PaddedPieceSize(ssize).Unpadded()),
	}}

	p1o, err := ffi.SealPreCommitPhase1(spt, cache, unseal, sealKey, snum, miner, ticket, pieces)
	require.NoError(t, err)
	commK, _, err := ffi.SealPreCommitPhase2(p1o, cache, sealKey)
	require.NoError(t, err)

	// snap encode
	update := filepath.Join(td, "update")
	updateCache := filepath.Join(td, "update-cache")

	// data to encode
	unsBuf := make([]byte, abi.PaddedPieceSize(ssize).Unpadded())
	_, _ = rand.Read(unsBuf)

	padded := make([]byte, abi.PaddedPieceSize(ssize))
	fr32.Pad(unsBuf, padded)

	// write to the update file as fr32 padded
	{
		f, err := os.Create(unseal)
		require.NoError(t, err)

		_, err = io.Copy(f, bytes.NewReader(padded))
		require.NoError(t, err)

		err = f.Close()
		require.NoError(t, err)
	}

	unsealedCid, err := commp2.GeneratePieceCIDFromFile(abi.RegisteredSealProof_StackedDrg2KiBV1_1, bytes.NewReader(unsBuf), abi.PaddedPieceSize(ssize).Unpadded())
	require.NoError(t, err)

	pieces = []abi.PieceInfo{{
		Size:     abi.PaddedPieceSize(ssize),
		PieceCID: unsealedCid,
	}}

	upt, err := spt.RegisteredUpdateProof()
	require.NoError(t, err)

	{
		require.NoError(t, os.MkdirAll(updateCache, 0755))
		f, err := os.Create(update)
		require.NoError(t, err)
		require.NoError(t, fallocate.Fallocate(f, 0, int64(ssize)))
		err = f.Close()
		require.NoError(t, err)
	}

	_, commD, err := ffi.SectorUpdate.EncodeInto(upt, update, updateCache, sealKey, cache, unseal, pieces)
	require.NoError(t, err)

	// snap decode
	keyReader, err := os.Open(sealKey)
	require.NoError(t, err)
	updateReader, err := os.Open(update)
	require.NoError(t, err)

	var out bytes.Buffer
	err = DecodeSnap(spt, commD, commK, keyReader, updateReader, &out)
	require.NoError(t, err)

	// extract with rust
	decOut := filepath.Join(td, "goldenOut")

	f, err := os.Create(decOut)
	require.NoError(t, err)
	require.NoError(t, fallocate.Fallocate(f, 0, int64(ssize)))
	require.NoError(t, f.Close())

	err = ffi.SectorUpdate.DecodeFrom(upt, decOut, update, sealKey, cache, commD)
	require.NoError(t, err)

	// read rust data
	rustOut, err := os.Open(decOut)
	require.NoError(t, err)

	var outRust bytes.Buffer
	_, err = io.Copy(&outRust, rustOut)
	require.NoError(t, err)
	require.NoError(t, rustOut.Close())

	// compare rust out with padded
	require.Equal(t, outRust.Bytes(), padded)

	// check data
	require.Equal(t, int(ssize), out.Len())

	for i := 0; i < out.Len(); i += proof.NODE_SIZE {
		//require.Equal(t, unsBuf[i], out.Bytes()[i])
		t.Logf("i: %d", i)
		require.Equal(t, unsBuf[i:i+proof.NODE_SIZE], out.Bytes()[i:i+proof.NODE_SIZE])
	}
}
