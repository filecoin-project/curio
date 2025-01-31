package proof

func computeTotalNodes(nLeaves, arity int64) (int64, []int64) {
	totalNodes := int64(0)
	levelCounts := []int64{}
	currLevelCount := nLeaves
	for currLevelCount > 0 {
		levelCounts = append(levelCounts, currLevelCount)
		totalNodes += currLevelCount
		if currLevelCount == 1 {
			break
		}
		currLevelCount = (currLevelCount + arity - 1) / arity
	}
	return totalNodes, levelCounts
}

func NodeLevel(leaves, arity int64) int {
	if leaves == 0 {
		return 0
	}
	level := 0
	for leaves > 1 {
		leaves = (leaves + arity - 1) / arity
		level++
	}
	return level + 1
}
