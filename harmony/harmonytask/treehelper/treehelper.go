package treehelper

// Given each task type's MayFollow list (predecessors whose completion may
// trigger this task), returns for every task type the preferred scheduling
// order among its downstream pipeline tasks: descendants closest to the
// pipeline end first, so work is ordered to minimize end-to-end latency when
// the oldest queued work is in that pipeline.
func FindPreferredTaskRunOrder(taskTypesMayFollows map[string][]string) map[string][]string {
	// predecessor -> task types that follow it in the pipeline
	forwardTree := make(map[string][]string)
	for taskType, mayFollows := range taskTypesMayFollows {
		for _, pred := range mayFollows {
			forwardTree[pred] = append(forwardTree[pred], taskType)
		}
	}

	out := make(map[string][]string)
	for taskType := range taskTypesMayFollows {
		var bfs []string
		layer := append([]string(nil), forwardTree[taskType]...)
		for len(layer) > 0 {
			bfs = append(bfs, layer...)
			var next []string
			for _, t := range layer {
				next = append(next, forwardTree[t]...)
			}
			layer = next
		}
		for i, j := 0, len(bfs)-1; i < j; i, j = i+1, j-1 {
			bfs[i], bfs[j] = bfs[j], bfs[i]
		}
		out[taskType] = bfs
	}
	return out
}
