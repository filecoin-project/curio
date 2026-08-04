package treehelper

// buildForwardDAG builds the pred→task directed graph: an edge pred→task
// exists iff task’s MayFollow includes pred. allNodes is every registered task
// name plus every predecessor string referenced in any MayFollow slice.
func buildForwardDAG(taskTypesMayFollows map[string][]string) (forward map[string][]string, allNodes map[string]struct{}) {
	forward = make(map[string][]string)
	allNodes = make(map[string]struct{}, len(taskTypesMayFollows)*2)
	for task, preds := range taskTypesMayFollows {
		allNodes[task] = struct{}{}
		for _, pred := range preds {
			allNodes[pred] = struct{}{}
			forward[pred] = append(forward[pred], task)
		}
	}
	return forward, allNodes
}

// assertAcyclic panics if forward contains a directed cycle.
func assertAcyclic(forward map[string][]string, allNodes map[string]struct{}) {
	const (
		unseen int8 = iota
		visiting
		done
	)
	state := make(map[string]int8, len(allNodes))
	var visit func(string)
	visit = func(u string) {
		switch state[u] {
		case visiting:
			panic("treehelper: MayFollow contains a cycle (pred→task graph)")
		case done:
			// This node was fully explored from an earlier DFS root (outer loop) or
			// an already-finished branch. Do not traverse again: one global visiting /
			// done table for all roots is correct for directed cycle detection (edge
			// to done cannot close a cycle with the current stack).
			return
		default:
			// unseen (including missing map entry): first visit to u.
		}
		state[u] = visiting
		for _, v := range forward[u] {
			visit(v)
		}
		state[u] = done
	}
	for n := range allNodes {
		if state[n] == unseen {
			visit(n)
		}
	}
}

// AssertMayFollowAcyclic panics if MayFollow forms a directed cycle when
// interpreted as pred→task edges (same graph as FindPreferredTaskRunOrder).
func AssertMayFollowAcyclic(taskTypesMayFollows map[string][]string) {
	forward, allNodes := buildForwardDAG(taskTypesMayFollows)
	assertAcyclic(forward, allNodes)
}

// Given each task type's MayFollow list (predecessors whose completion may
// trigger this task), returns for every task type the preferred scheduling
// order among its downstream pipeline tasks: descendants closest to the
// pipeline end first, so work is ordered to minimize end-to-end latency when
// the oldest queued work is in that pipeline.
func FindPreferredTaskRunOrder(taskTypesMayFollows map[string][]string) map[string][]string {
	forward, allNodes := buildForwardDAG(taskTypesMayFollows)
	assertAcyclic(forward, allNodes)

	out := make(map[string][]string)
	for taskType := range taskTypesMayFollows {
		var bfs []string
		layer := append([]string(nil), forward[taskType]...)
		for len(layer) > 0 {
			bfs = append(bfs, layer...)
			var next []string
			for _, t := range layer {
				next = append(next, forward[t]...)
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
