package proof

import "golang.org/x/xerrors"

type RawMerkleProof struct {
	Leaf  [32]byte
	Proof [][32]byte
	Root  [32]byte
}

// MemtreeProof generates a Merkle proof for the given leaf index from the memtree.
// The memtree is a byte slice containing all the nodes of the Merkle tree, including leaves and internal nodes.
func MemtreeProof(memtree []byte, leafIndex int64) (*RawMerkleProof, error) {
	// Currently, the implementation supports only binary trees (arity == 2)
	const arity = 2

	// Calculate the total number of nodes in the memtree
	totalNodes := int64(len(memtree)) / NODE_SIZE

	// Reconstruct level sizes from the total number of nodes
	// Starting from the number of leaves, compute the number of nodes at each level
	nLeaves := (totalNodes + 1) / 2

	currLevelCount := nLeaves
	levelSizes := []int64{}
	totalNodesCheck := int64(0)

	for {
		levelSizes = append(levelSizes, currLevelCount)
		totalNodesCheck += currLevelCount

		if currLevelCount == 1 {
			break
		}
		// Compute the number of nodes in the next level
		currLevelCount = (currLevelCount + int64(arity) - 1) / int64(arity)
	}

	// Verify that the reconstructed total nodes match the actual total nodes
	if totalNodesCheck != totalNodes {
		return nil, xerrors.New("invalid memtree size; reconstructed total nodes do not match")
	}

	// Compute the starting byte offset for each level in memtree
	levelStarts := make([]int64, len(levelSizes))
	var offset int64 = 0
	for i, size := range levelSizes {
		levelStarts[i] = offset
		offset += size * NODE_SIZE
	}

	// Validate the leaf index
	if leafIndex < 0 || leafIndex >= levelSizes[0] {
		return nil, xerrors.Errorf("invalid leaf index %d for %d leaves", leafIndex, levelSizes[0])
	}

	// Initialize the proof structure
	proof := &RawMerkleProof{
		Proof: make([][NODE_SIZE]byte, 0, len(levelSizes)-1),
	}

	// Extract the leaf hash from the memtree
	leafOffset := levelStarts[0] + leafIndex*NODE_SIZE
	copy(proof.Leaf[:], memtree[leafOffset:leafOffset+NODE_SIZE])

	// Build the proof by collecting sibling hashes at each level
	index := leafIndex
	for level := 0; level < len(levelSizes)-1; level++ {
		siblingIndex := index ^ 1 // Toggle the last bit to get the sibling index

		siblingOffset := levelStarts[level] + siblingIndex*NODE_SIZE
		var siblingHash [NODE_SIZE]byte
		copy(siblingHash[:], memtree[siblingOffset:siblingOffset+NODE_SIZE])
		proof.Proof = append(proof.Proof, siblingHash)

		// Move up to the parent index
		index /= int64(arity)
	}

	// Extract the root hash from the memtree
	rootOffset := levelStarts[len(levelSizes)-1]
	copy(proof.Root[:], memtree[rootOffset:rootOffset+NODE_SIZE])

	return proof, nil
}
