package proof

import (
	pool "github.com/libp2p/go-buffer-pool"
	"github.com/minio/sha256-simd"
)

// Sha254PartialMemtree computes a memtree starting from pre-supplied subtree roots.
// You provide 2^computedLevels subtree roots (via SetSubtreeRoot), then Build()
// hashes them up through computedLevels to produce the final root.
//
// Allows faster execution during proving, loads pre-computed subtree roots from storage to avoid
// re-hashing the full tree, then Build() only the remaining levels for proof generation.
type Sha254PartialMemtree struct {
	// memtreeBuf holds all nodes: level 0 (subtree roots) through top level (single root)
	memtreeBuf []byte
	// levelSizes[i] is the number of nodes at level i
	levelSizes []int64
	// levelStarts[i] is the byte offset in memtreeBuf where level i begins
	levelStarts []int64
}

// NewSha254PartialMemtree allocates a partial memtree whose bottom layer has
// 2^computedLevels subtree-root slots. The remaining computedLevels levels
// above are computed by Build.
func NewSha254PartialMemtree(computedLevels int) *Sha254PartialMemtree {
	nRoots := int64(1) << computedLevels
	totalNodes, levelSizes := computeTotalNodes(nRoots, 2)

	buf := pool.Get(int(totalNodes * NODE_SIZE))
	starts := make([]int64, len(levelSizes))
	for i := 1; i < len(starts); i++ {
		starts[i] = starts[i-1] + levelSizes[i-1]*NODE_SIZE
	}

	return &Sha254PartialMemtree{
		memtreeBuf:  buf,
		levelSizes:  levelSizes,
		levelStarts: starts,
	}
}

// SubtreeRootCount returns the number of subtree roots expected at level 0.
func (p *Sha254PartialMemtree) SubtreeRootCount() int64 { return p.levelSizes[0] }

// SetSubtreeRoot writes a NODE_SIZE hash into level 0 at the given index.
func (p *Sha254PartialMemtree) SetSubtreeRoot(index int64, hash []byte) {
	off := p.levelStarts[0] + index*NODE_SIZE
	copy(p.memtreeBuf[off:off+NODE_SIZE], hash)
}

// Build hashes every level above the subtree roots, identical to the inner
// loop of BuildSha254Memtree.
func (p *Sha254PartialMemtree) Build() {
	d := sha256.New()
	for level := 1; level < len(p.levelSizes); level++ {
		prevStart := p.levelStarts[level-1]
		currStart := p.levelStarts[level]
		for i := int64(0); i < p.levelSizes[level]; i++ {
			left := prevStart + 2*i*NODE_SIZE
			d.Reset()
			d.Write(p.memtreeBuf[left : left+2*NODE_SIZE])
			out := currStart + i*NODE_SIZE
			d.Sum(p.memtreeBuf[out:out])
			p.memtreeBuf[out+NODE_SIZE-1] &= 0x3F
		}
	}
}

// Root returns the single root hash (top of the tree).
func (p *Sha254PartialMemtree) Root() [NODE_SIZE]byte {
	var root [NODE_SIZE]byte
	off := p.levelStarts[len(p.levelSizes)-1]
	copy(root[:], p.memtreeBuf[off:off+NODE_SIZE])
	return root
}

// Memtree returns the underlying buffer (subtree roots + computed levels).
func (p *Sha254PartialMemtree) Memtree() []byte { return p.memtreeBuf }

// Release returns the buffer to the pool. The struct must not be used after.
func (p *Sha254PartialMemtree) Release() {
	if p.memtreeBuf != nil {
		pool.Put(p.memtreeBuf)
		p.memtreeBuf = nil
	}
}

// --- helpers for testing against BuildSha254Memtree ---

// SubtreeRootLevelInFullTree returns the level index in a full memtree
// (with fullTreeLeaves leaves) that corresponds to this partial tree's
// subtree-root layer.
func (p *Sha254PartialMemtree) SubtreeRootLevelInFullTree(fullTreeLeaves int64) int {
	_, fullLevels := computeTotalNodes(fullTreeLeaves, 2)
	return len(fullLevels) - len(p.levelSizes)
}

// LoadSubtreeRootsFromFullMemtree copies the matching level from a full
// memtree (as returned by BuildSha254Memtree) into this partial tree's
// subtree-root layer.
func (p *Sha254PartialMemtree) LoadSubtreeRootsFromFullMemtree(fullMemtree []byte, fullTreeLeaves int64) {
	_, fullLevels := computeTotalNodes(fullTreeLeaves, 2)
	srcLevel := len(fullLevels) - len(p.levelSizes)

	var srcOff int64
	for i := 0; i < srcLevel; i++ {
		srcOff += fullLevels[i] * NODE_SIZE
	}

	n := p.levelSizes[0] * NODE_SIZE
	copy(p.memtreeBuf[p.levelStarts[0]:p.levelStarts[0]+n],
		fullMemtree[srcOff:srcOff+n])
}
