package proof

import "testing"

func TestComputeTotalNodes(t *testing.T) {
	tests := []struct {
		leaves     int64
		arity      int64
		wantTotal  int64
		wantLevels int
	}{
		{1, 2, 1, 1},   // Single leaf: no parents needed, 1 level
		{2, 2, 3, 2},   // Two leaves: 1 parent, 2 levels (2+1=3)
		{3, 2, 6, 3},   // Three leaves: rounds up to 2 parents, then 1 root (3+2+1=6)
		{4, 2, 7, 3},   // Power of 2: perfect binary tree depth 3 (4+2+1=7)
		{7, 2, 14, 4},  // Seven leaves: 4, 2, 1 parents at each level (7+4+2+1=14)
		{8, 2, 15, 4},  // Power of 2: perfect binary tree depth 4 (8+4+2+1=15)
		{3, 3, 4, 2},   // Ternary: 3 leaves fit in 1 parent (3+1=4)
		{9, 3, 13, 3},  // Power of 3: perfect ternary tree depth 3 (9+3+1=13)
		{27, 3, 40, 4}, // Power of 3: perfect ternary tree depth 4 (27+9+3+1=40)
	}

	for _, tt := range tests {
		treeSize := computeTreeSize(tt.leaves, tt.arity)
		if treeSize.NodeCount != tt.wantTotal {
			t.Errorf("computeTotalNodes(%d, %d): total=%d, want %d", tt.leaves, tt.arity, treeSize.NodeCount, tt.wantTotal)
		}
		if len(treeSize.LevelSizes) != tt.wantLevels {
			t.Errorf("computeTotalNodes(%d, %d): levels=%d, want %d", tt.leaves, tt.arity, len(treeSize.LevelSizes), tt.wantLevels)
		}
		if treeSize.LevelSizes[len(treeSize.LevelSizes)-1] != 1 {
			t.Errorf("computeTotalNodes(%d, %d): root != 1", tt.leaves, tt.arity)
		}
	}
}
