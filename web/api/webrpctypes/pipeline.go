package webrpctypes

type PipelineStage struct {
	Name    string
	Pending int64
	Running int64
}

type PipelineStats struct {
	Total  int64
	Stages []PipelineStage
}
