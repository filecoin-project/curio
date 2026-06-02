package metrics

import (
	"context"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	curiobuild "github.com/filecoin-project/curio/build"
)

var (
	nodeTag, _        = tag.NewKey("node")
	nodeVersionTag, _ = tag.NewKey("version")

	NodeInfo = stats.Int64("node_info", "Curio node identity and version. Value is always 1.", stats.UnitDimensionless)
)

func init() {
	err := view.Register(&view.View{
		Measure:     NodeInfo,
		Aggregation: view.LastValue(),
		TagKeys:     []tag.Key{nodeTag, nodeVersionTag},
	})
	if err != nil {
		panic(err)
	}
}

func RecordNodeInfo(ctx context.Context, nodeName, listenAddr string) error {
	node := nodeName
	if node == "" {
		node = listenAddr
	}

	return stats.RecordWithTags(ctx, []tag.Mutator{
		tag.Upsert(nodeTag, node),
		tag.Upsert(nodeVersionTag, curiobuild.UserVersion()),
	}, NodeInfo.M(1))
}
