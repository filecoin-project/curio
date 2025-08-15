//go:build cunative

package cunative

import (
	"bytes"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"time"
	"testing"

	"github.com/detailyang/go-fallocate"
	"github.com/stretchr/testify/require"

	ffi "github.com/filecoin-project/filecoin-ffi"
	commp2 "github.com/filecoin-project/go-commp-utils/v2"
	"github.com/filecoin-project/go-commp-utils/zerocomm"
	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/lib/nullreader"
	"github.com/filecoin-project/lotus/storage/sealer/fr32"
)

func TestSnapEncodeMatchesFFI(t *testing.T) {
	t.Run("2K", testSnapEncode(abi.RegisteredSealProof_StackedDrg2KiBV1_1))
	t.Run("8M", testSnapEncode(abi.RegisteredSealProof_StackedDrg8MiBV1_1))
}

func testSnapEncode(spt abi.RegisteredSealProof) func(t *testing.T) {
	return func(t *testing.T) {
		td := t.TempDir()
		cache := filepath.Join(td, "cache")
		unseal := filepath.Join(td, "unsealed")
		sealKey := filepath.Join(td, "sealed")

		require.NoError(t, os.MkdirAll(cache, 0o755))

		ssize, err := spt.SectorSize()
		require.NoError(t, err)

		// write null "unsealed" for CC sealing
		{
			uf, err := os.Create(unseal)
			require.NoError(t, err)
			_, err = io.CopyN(uf, &nullreader.Reader{}, int64(ssize))
			require.NoError(t, err)
			require.NoError(t, uf.Close())
		}

		// create empty sealed file (key)
		{
			f, err := os.Create(sealKey)
			require.NoError(t, err)
			require.NoError(t, f.Close())
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

		// snap encode inputs
		update := filepath.Join(td, "update")
		updateCache := filepath.Join(td, "update-cache")

		// data to encode
		unsBuf := make([]byte, abi.PaddedPieceSize(ssize).Unpadded())
		_, _ = rand.Read(unsBuf)

		padded := make([]byte, abi.PaddedPieceSize(ssize))
		fr32.Pad(unsBuf, padded)

		// write padded to the unseal file
		{
			f, err := os.Create(unseal)
			require.NoError(t, err)
			_, err = io.Copy(f, bytes.NewReader(padded))
			require.NoError(t, err)
			require.NoError(t, f.Close())
		}

		// compute CommD over unpadded data
		unsealedCid, err := commp2.GeneratePieceCIDFromFile(abi.RegisteredSealProof_StackedDrg2KiBV1_1, bytes.NewReader(unsBuf), abi.PaddedPieceSize(ssize).Unpadded())
		require.NoError(t, err)

		pieces = []abi.PieceInfo{{
			Size:     abi.PaddedPieceSize(ssize),
			PieceCID: unsealedCid,
		}}

		upt, err := spt.RegisteredUpdateProof()
		require.NoError(t, err)

		// prepare update file for legacy FFI encode
		{
			require.NoError(t, os.MkdirAll(updateCache, 0o755))
			f, err := os.Create(update)
			require.NoError(t, err)
			require.NoError(t, fallocate.Fallocate(f, 0, int64(ssize)))
			require.NoError(t, f.Close())
		}

		// legacy FFI encode into update
		_, commD, err := ffi.SectorUpdate.EncodeInto(upt, update, updateCache, sealKey, cache, unseal, pieces)
		require.NoError(t, err)
		_ = commD // return value used to match commD below

		// read legacy output bytes
		legacyF, err := os.Open(update)
		require.NoError(t, err)
		var legacyBuf bytes.Buffer
		_, err = io.Copy(&legacyBuf, legacyF)
		require.NoError(t, err)
		require.NoError(t, legacyF.Close())

		// our EncodeSnap
		keyReader, err := os.Open(sealKey)
		require.NoError(t, err)
		dataReader, err := os.Open(unseal)
		require.NoError(t, err)

		var ourBuf bytes.Buffer
		start := time.Now()
		err = EncodeSnap(spt, commD, commK, keyReader, dataReader, &ourBuf)
		done := time.Now()
		t.Logf("EncodeSnap time: %s", done.Sub(start))
		t.Logf("EncodeSnap throughput: %f MB/s", float64(ssize)/done.Sub(start).Seconds()/1024/1024)
		require.NoError(t, err)

		// compare byte-for-byte
		require.Equal(t, legacyBuf.Len(), ourBuf.Len(), "output size mismatch")
		require.Equal(t, legacyBuf.Bytes(), ourBuf.Bytes(), "encoded replica differs from legacy FFI output")

		t.Logf("EncodeSnap good")
	}
}
