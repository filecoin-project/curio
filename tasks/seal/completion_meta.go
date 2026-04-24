package seal

import (
	"context"

	"github.com/filecoin-project/curio/harmony/harmonytask"
)

// PoRepPipelineKey is the context key for sectors_sdr_pipeline (sp_id, sector_number).
type poRepPipelineKeyT struct{}

var PoRepPipelineKey poRepPipelineKeyT

// PipelineRef returns sp_id and sector_number stashed during Do via SetMeta(..., PoRepPipelineKey, ...).
func PipelineRef(ctx context.Context) (spID, sectorNum int64, ok bool) {
	v, found := harmonytask.MetaValue[[2]int64](ctx, PoRepPipelineKey)
	if !found {
		return 0, 0, false
	}
	return v[0], v[1], true
}
