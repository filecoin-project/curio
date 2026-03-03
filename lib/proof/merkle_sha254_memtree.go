package proof

import (
	"io"

	pool "github.com/libp2p/go-buffer-pool"
	"github.com/minio/sha256-simd"
	"golang.org/x/xerrors"

	"github.com/filecoin-project/go-state-types/abi"

	"github.com/filecoin-project/lotus/storage/sealer/fr32"
)

const MaxMemtreeSize = 1 << 30

// BuildSha254Memtree builds a sha256 memtree from the input data
// Returned slice should be released to the pool after use
func BuildSha254Memtree(rawIn io.Reader, size abi.UnpaddedPieceSize) ([]byte, error) {
	if size.Padded() > MaxMemtreeSize {
		return nil, xerrors.Errorf("piece too large for memtree: %d", size)
	}

	unpadBuf := pool.Get(int(size))
	// read into unpadBuf
	_, err := io.ReadFull(rawIn, unpadBuf)
	if err != nil {
		pool.Put(unpadBuf)
		return nil, xerrors.Errorf("failed to read into unpadBuf: %w", err)
	}

	nLeaves := int64(size.Padded()) / NODE_SIZE
	totalNodes, levelSizes := computeTotalNodes(nLeaves, 2)
	memtreeBuf := pool.Get(int(totalNodes * NODE_SIZE))

	fr32.Pad(unpadBuf, memtreeBuf[:size.Padded()])
	pool.Put(unpadBuf)

	d := sha256.New()

	levelStarts := make([]int64, len(levelSizes))
	levelStarts[0] = 0
	for i := 1; i < len(levelSizes); i++ {
		levelStarts[i] = levelStarts[i-1] + levelSizes[i-1]*NODE_SIZE
	}

	for level := 1; level < len(levelSizes); level++ {
		levelNodes := levelSizes[level]
		prevLevelStart := levelStarts[level-1]
		currLevelStart := levelStarts[level]

		for i := int64(0); i < levelNodes; i++ {
			leftOffset := prevLevelStart + (2*i)*NODE_SIZE

			d.Reset()
			d.Write(memtreeBuf[leftOffset : leftOffset+(NODE_SIZE*2)])

			outOffset := currLevelStart + i*NODE_SIZE
			// sum calls append, so we give it a zero len slice at the correct offset
			d.Sum(memtreeBuf[outOffset:outOffset])

			// set top bits to 00
			memtreeBuf[outOffset+NODE_SIZE-1] &= 0x3F
		}
	}

	return memtreeBuf, nil
}

func ComputeBinShaParent(left, right [NODE_SIZE]byte) [NODE_SIZE]byte {
	out := sha256.Sum256(append(left[:], right[:]...))
	out[NODE_SIZE-1] &= 0x3F
	return out
}

// BuildSha254MemtreeFromSnapshot builds a sha256 memtree from a pre-computed snapshot layer.
// The input data is a concatenation of 32-byte node hashes forming a complete layer.
// Returned slice should be released to the pool after use.
func BuildSha254MemtreeFromSnapshot(data []byte) ([]byte, error) {
	size := abi.PaddedPieceSize(len(data))
	if size > MaxMemtreeSize {
		return nil, xerrors.Errorf("piece too large for memtree: %d", size)
	}

	nLeaves := int64(size) / NODE_SIZE
	totalNodes, levelSizes := computeTotalNodes(nLeaves, 2)
	memtreeBuf := pool.Get(int(totalNodes * NODE_SIZE))

	copy(memtreeBuf[:len(data)], data)

	d := sha256.New()

	levelStarts := make([]int64, len(levelSizes))
	levelStarts[0] = 0
	for i := 1; i < len(levelSizes); i++ {
		levelStarts[i] = levelStarts[i-1] + levelSizes[i-1]*NODE_SIZE
	}

	for level := 1; level < len(levelSizes); level++ {
		levelNodes := levelSizes[level]
		prevLevelStart := levelStarts[level-1]
		currLevelStart := levelStarts[level]

		for i := int64(0); i < levelNodes; i++ {
			leftOffset := prevLevelStart + (2*i)*NODE_SIZE

			d.Reset()
			d.Write(memtreeBuf[leftOffset : leftOffset+(NODE_SIZE*2)])

			outOffset := currLevelStart + i*NODE_SIZE
			// sum calls append, so we give it a zero len slice at the correct offset
			d.Sum(memtreeBuf[outOffset:outOffset])

			// set top bits to 00
			memtreeBuf[outOffset+NODE_SIZE-1] &= 0x3F
		}
	}

	return memtreeBuf, nil
}
