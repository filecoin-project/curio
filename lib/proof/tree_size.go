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

func computeTotalLevels(nLeaves, arity int64) int64 {
	levels := int64(0)
	currLevelCount := nLeaves
	for currLevelCount > 0 {
		levels++
		if currLevelCount == 1 {
			break
		}
		currLevelCount = (currLevelCount + arity - 1) / arity
	}
	return levels
}
