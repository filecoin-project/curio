package proof

// TreeSize holds the structure information for an n-ary tree.
type TreeSize struct {
	// Total nodes in tree
	NodeCount int64
	// Nodes at each level (leaf to root), LevelSizes[0] = leaves, LevelSizes[n-1] = root (always 1)
	LevelSizes []int64
}

// computeTreeSize returns tree structure info: total node count and per-level counts.
// Example: leaves=4, arity=2 → TreeSize{NodeCount: 7, LevelSizes: [4, 2, 1]}.
//
// LevelSizes[0] = leaves, LevelSizes[n-1] = root (always 1).
func computeTreeSize(leaves, arity int64) TreeSize {
	var total int64
	var sizes []int64
	count := leaves
	for count > 0 {
		sizes = append(sizes, count)
		total += count
		if count == 1 {
			break
		}
		count = (count + arity - 1) / arity
	}
	return TreeSize{NodeCount: total, LevelSizes: sizes}
}
