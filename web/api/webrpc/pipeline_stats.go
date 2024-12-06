package webrpc

import "context"

type PipelineStage struct {
	Name    string
	Pending int64
	Running int64
}

type PipelineStats struct {
	// Total pipeline count
	Total int64

	Stages []PipelineStage
}

func (a *WebRPC) PipelineStatsMarket(ctx context.Context) (*PipelineStats, error) {
	var out PipelineStats

	out.Total = 60

	// eventually this will come from a DB
	out.Stages = append(out.Stages, PipelineStage{
		Name:    "Downloading",
		Pending: 2,
		Running: 10,
	})
	out.Stages = append(out.Stages, PipelineStage{
		Name:    "Publish",
		Pending: 2,
		Running: 10,
	})
	out.Stages = append(out.Stages, PipelineStage{
		Name:    "Seal",
		Pending: 2,
		Running: 10,
	})
	out.Stages = append(out.Stages, PipelineStage{
		Name:    "Index",
		Pending: 2,
		Running: 10,
	})
	out.Stages = append(out.Stages, PipelineStage{
		Name:    "Announce",
		Pending: 2,
		Running: 10,
	})

	return &out, nil
}
