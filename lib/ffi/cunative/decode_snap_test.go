package cunative

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
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
	"time"
)

func TestSnapDecode(t *testing.T) {
	t.Run("2K", testSnapDecode(abi.RegisteredSealProof_StackedDrg2KiBV1_1))
	t.Run("8M", testSnapDecode(abi.RegisteredSealProof_StackedDrg8MiBV1_1))
}

func testSnapDecode(spt abi.RegisteredSealProof) func(t *testing.T) {
	return func(t *testing.T) {
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

		decStart := time.Now()
		err = ffi.SectorUpdate.DecodeFrom(upt, decOut, update, sealKey, cache, commD)
		require.NoError(t, err)

		decDone := time.Now()
		t.Logf("Decode time: %s", decDone.Sub(decStart))
		t.Logf("Decode throughput: %f MB/s", float64(ssize)/decDone.Sub(decStart).Seconds()/1024/1024)

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
			require.Equal(t, padded[i:i+proof.NODE_SIZE], out.Bytes()[i:i+proof.NODE_SIZE])
		}
	}
}

func TestPhi(t *testing.T) {
	d := "b0c133e15929f16f9491f9c82a128786d006a3b7286642cc78644c974f55c42f"
	r := "58b65c3e1a1d52c078cb69b4ac995e515c54be9cbecd8ed28ee8009722d3c969"

	goodPhi := "436c953f3ae69bf47385748daf306871e6839a91d5229a55dda0e02653ce2f27"

	dBytes, err := hex.DecodeString(d)
	require.NoError(t, err)
	rBytes, err := hex.DecodeString(r)
	require.NoError(t, err)

	phi, err := Phi(dBytes, rBytes)
	require.NoError(t, err)

	require.Equal(t, goodPhi, hex.EncodeToString(phi[:]))
}
